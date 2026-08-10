package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	appnotify "github.com/starlink/push/internal/app/notify"
	apptrace "github.com/starlink/push/internal/app/trace"
	"github.com/starlink/push/internal/bootstrap"
	"github.com/starlink/push/internal/scheduler"
)

func main() {
	cfgPath := flag.String("config", "configs/config.yaml", "config file path")
	flag.Parse()

	infra, err := bootstrap.NewInfra(*cfgPath)
	if err != nil {
		slog.Error("bootstrap failed", "err", err)
		os.Exit(1)
	}
	if err := infra.MQ.EnsureReady(context.Background()); err != nil {
		slog.Error("ensure mq ready", "err", err)
		os.Exit(1)
	}
	if err := infra.Redis.Ping(context.Background()); err != nil {
		slog.Error("redis ping", "err", err)
		os.Exit(1)
	}

	inbox := appnotify.NewBus(infra.Notifications, nil, infra.Redis.RDB())
	tracer := apptrace.NewRecorder(infra.Traces, "scheduler")
	defer tracer.Close()
	agg := scheduler.NewAggregator(infra.Tasks, infra.AggCache, infra.Webhook, infra.Push, inbox)
	agg.SetTracer(tracer)
	splitter := scheduler.NewSplitter(infra.Tasks, infra.Audience, infra.Limiter, infra.Cfg.Scheduler.BatchSize, infra.Push)
	splitter.SetExcludeResolver(infra.Segments)
	splitter.SetTracer(tracer)
	worker := scheduler.NewWorker(
		infra.Tasks,
		infra.MQ,
		agg,
		splitter,
		infra.Limiter,
		infra.Cfg.Scheduler.WorkerConcurrency,
		infra.Cfg.Scheduler.PollIntervalMs,
		infra.Cfg.Scheduler.ClaimTimeoutSec,
		infra.Cfg.Scheduler.SplitLeaseSec,
		infra.Cfg.Scheduler.SplitConcurrency,
	)
	worker.SetTracer(tracer)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	slog.Info("scheduler running", "worker_id", worker.ID())
	if err := worker.Run(ctx); err != nil && err != context.Canceled {
		slog.Error("scheduler exited", "err", err)
		os.Exit(1)
	}
}
