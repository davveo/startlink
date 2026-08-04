//go:build rocketmq

package mq

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/apache/rocketmq-client-go/v2"
	"github.com/apache/rocketmq-client-go/v2/consumer"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/apache/rocketmq-client-go/v2/producer"
	"github.com/starlink/push/internal/config"
)

// ApacheRocketTransport 基于官方 rocketmq-client-go/v2 的生产 Transport。
// 编译：go build -tags rocketmq ./...
// 依赖：go get github.com/apache/rocketmq-client-go/v2
type ApacheRocketTransport struct {
	cfg      config.RocketMQConfig
	producer rocketmq.Producer
	mu       sync.Mutex
	started  bool
}

func NewApacheRocketTransport(cfg config.RocketMQConfig) *ApacheRocketTransport {
	return &ApacheRocketTransport{cfg: cfg}
}

func init() {
	rocketInitFn = func(cfg config.RocketMQConfig) {
		if len(cfg.NameServers) == 0 {
			slog.Warn("rocketmq transport skip: name_servers empty")
			return
		}
		SetRocketTransport(NewApacheRocketTransport(cfg))
		slog.Info("rocketmq apache transport registered", "name_servers", cfg.NameServers)
	}
	if os.Getenv("STARLINK_ROCKETMQ_AUTO") == "1" {
		ns := strings.Split(os.Getenv("STARLINK_ROCKETMQ_NS"), ",")
		TryInitRocketTransport(config.RocketMQConfig{NameServers: ns, Retry: 2})
	}
}

func (t *ApacheRocketTransport) Start(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.started {
		return nil
	}
	opts := []producer.Option{
		producer.WithNameServer(t.cfg.NameServers),
		producer.WithRetry(t.cfg.Retry),
		producer.WithGroupName("starlink-producer"),
	}
	if t.cfg.Namespace != "" {
		opts = append(opts, producer.WithNamespace(t.cfg.Namespace))
	}
	if t.cfg.AccessKey != "" {
		opts = append(opts, producer.WithCredentials(primitive.Credentials{
			AccessKey: t.cfg.AccessKey,
			SecretKey: t.cfg.SecretKey,
		}))
	}
	p, err := rocketmq.NewProducer(opts...)
	if err != nil {
		return fmt.Errorf("new producer: %w", err)
	}
	if err := p.Start(); err != nil {
		return fmt.Errorf("start producer: %w", err)
	}
	t.producer = p
	t.started = true
	return nil
}

func (t *ApacheRocketTransport) EnsureTopic(ctx context.Context, topic string) error {
	if topic == "" {
		return fmt.Errorf("empty topic")
	}
	return nil
}

func (t *ApacheRocketTransport) Send(ctx context.Context, topic string, body []byte) error {
	if t.producer == nil {
		return fmt.Errorf("producer not started")
	}
	msg := &primitive.Message{Topic: topic, Body: body}
	_, err := t.producer.SendSync(ctx, msg)
	return err
}

func (t *ApacheRocketTransport) Subscribe(ctx context.Context, topic, group, consumerID string, onMessage func(ctx context.Context, body []byte) error) error {
	opts := []consumer.Option{
		consumer.WithNameServer(t.cfg.NameServers),
		consumer.WithGroupName(group),
		consumer.WithConsumerModel(consumer.Clustering),
		consumer.WithConsumeFromWhere(consumer.ConsumeFromLastOffset),
		consumer.WithInstance(consumerID),
	}
	if t.cfg.Namespace != "" {
		opts = append(opts, consumer.WithNamespace(t.cfg.Namespace))
	}
	if t.cfg.AccessKey != "" {
		opts = append(opts, consumer.WithCredentials(primitive.Credentials{
			AccessKey: t.cfg.AccessKey,
			SecretKey: t.cfg.SecretKey,
		}))
	}
	c, err := rocketmq.NewPushConsumer(opts...)
	if err != nil {
		return err
	}
	err = c.Subscribe(topic, consumer.MessageSelector{}, func(ctx context.Context, msgs ...*primitive.MessageExt) (consumer.ConsumeResult, error) {
		for _, m := range msgs {
			if err := onMessage(ctx, m.Body); err != nil {
				return consumer.ConsumeRetryLater, nil
			}
		}
		return consumer.ConsumeSuccess, nil
	})
	if err != nil {
		return err
	}
	if err := c.Start(); err != nil {
		return err
	}
	<-ctx.Done()
	_ = c.Shutdown()
	return ctx.Err()
}

func (t *ApacheRocketTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.producer != nil {
		err := t.producer.Shutdown()
		t.producer = nil
		t.started = false
		return err
	}
	return nil
}
