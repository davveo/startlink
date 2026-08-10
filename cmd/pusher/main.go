package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/google/uuid"
	"github.com/starlink/push/internal/adapter/audience"
	apptrace "github.com/starlink/push/internal/app/trace"
	"github.com/starlink/push/internal/bootstrap"
	"github.com/starlink/push/internal/port"
	"github.com/starlink/push/internal/push"
)

// preferenceResolver 偏好中心关闭时返回 nil，Gateway 会完全跳过偏好查询。
func preferenceResolver(infra *bootstrap.Infra) port.PreferenceResolver {
	if !infra.Cfg.Preference.IsEnabled() || infra.PrefResolver == nil {
		return nil
	}
	return infra.PrefResolver
}

func main() {
	cfgPath := flag.String("config", "configs/config.yaml", "config file path")
	queueMode := flag.String("queue", "all", "consume queue: all | high | normal")
	flag.Parse()

	infra, err := bootstrap.NewInfra(*cfgPath)
	if err != nil {
		slog.Error("bootstrap failed", "err", err)
		os.Exit(1)
	}
	defer func() { _ = infra.MQ.Close() }()

	if err := infra.MQ.EnsureReady(context.Background()); err != nil {
		slog.Error("ensure mq ready", "err", err)
		os.Exit(1)
	}
	if err := infra.Redis.Ping(context.Background()); err != nil {
		slog.Error("redis ping", "err", err)
		os.Exit(1)
	}

	if !infra.Cfg.Preference.IsEnabled() {
		slog.Warn("user preference center disabled; marketing opt-out and per-user quiet hours will not be enforced")
	}

	tracer := apptrace.NewRecorder(infra.Traces, "pusher")
	defer tracer.Close()

	gateway := push.NewGateway(
		infra.Channels,
		infra.AggCache,
		infra.Push,
		infra.Tasks,
		infra.Limiter,
		infra.Cfg.Pusher.RateLimitQPS,
		infra.Cfg.Pusher.MaxRetry,
		infra.Cfg.Pusher.DedupTTLSec,
		infra.Cfg.Freq,
		audience.NewUnsubscribeFilter(infra.Redis.RDB(), infra.Cfg.Compliance.UnsubscribeKeyPrefix),
		preferenceResolver(infra),
		infra.Cfg.Preference.FailOpen,
	)
	gateway.SetTracer(tracer)

	baseID := "pusher-" + uuid.NewString()[:8]
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	type queueRun struct {
		name string
		mq   port.MessageQueue
		conc int
	}
	var runs []queueRun
	switch *queueMode {
	case "high":
		runs = append(runs, queueRun{"high", infra.MQ.High(), infra.Cfg.Pusher.HighWorkerConcurrency})
	case "normal":
		runs = append(runs, queueRun{"normal", infra.MQ.Normal(), infra.Cfg.Pusher.WorkerConcurrency})
	case "all", "":
		runs = append(runs,
			queueRun{"high", infra.MQ.High(), infra.Cfg.Pusher.HighWorkerConcurrency},
			queueRun{"normal", infra.MQ.Normal(), infra.Cfg.Pusher.WorkerConcurrency},
		)
	default:
		slog.Error("invalid -queue", "value", *queueMode, "want", "all|high|normal")
		os.Exit(1)
	}

	slog.Info("pusher running",
		"base_id", baseID,
		"queue_mode", *queueMode,
		"mq_driver", infra.MQ.Driver(),
		"high_topic", infra.Cfg.MQ.High.TopicOrStream(),
		"normal_topic", infra.Cfg.MQ.Normal.TopicOrStream(),
	)

	errCh := make(chan error, len(runs))
	var wg sync.WaitGroup
	for _, r := range runs {
		r := r
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := baseID + "-" + r.name
			c := push.NewConsumer(r.mq, gateway, id, r.conc)
			slog.Info("queue consumer started", "queue", r.name, "consumer_id", id, "concurrency", r.conc)
			if err := c.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				errCh <- err
				cancel()
			}
		}()
	}
	wg.Wait()

	select {
	case err := <-errCh:
		slog.Error("pusher exited", "err", err)
		os.Exit(1)
	default:
	}
}
