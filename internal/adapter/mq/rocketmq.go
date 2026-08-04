package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
)

// RocketTransport 可替换的 RocketMQ 传输层（真实 SDK 或自研客户端）。
// 默认未注入时，driver=rocketmq 会报错提示实现本接口。
type RocketTransport interface {
	// Start 建立到 NameServer / Proxy 的连接
	Start(ctx context.Context) error
	// EnsureTopic 确保 Topic 可用（部分云厂商需控制台预创建，可空实现）
	EnsureTopic(ctx context.Context, topic string) error
	// Send 同步发送
	Send(ctx context.Context, topic string, body []byte) error
	// Subscribe 启动订阅；onMessage 返回 nil 表示消费成功
	Subscribe(ctx context.Context, topic, group, consumerID string, onMessage func(ctx context.Context, body []byte) error) error
	Close() error
}

var (
	rocketMu        sync.RWMutex
	rocketTransport RocketTransport
)

// SetRocketTransport 注入真实 RocketMQ 客户端（业务侧或 adapter 实现后调用）。
// 须在 mq.Open 之前设置；可在 init / main 中注册。
func SetRocketTransport(t RocketTransport) {
	rocketMu.Lock()
	defer rocketMu.Unlock()
	rocketTransport = t
}

func getRocketTransport() RocketTransport {
	rocketMu.RLock()
	defer rocketMu.RUnlock()
	return rocketTransport
}

// RocketMQQueue 基于可插拔 Transport 的单 Topic 队列
type RocketMQQueue struct {
	topic     string
	group     string
	transport RocketTransport
}

func NewRocketMQQueue(topic, group string, t RocketTransport) *RocketMQQueue {
	return &RocketMQQueue{topic: topic, group: group, transport: t}
}

func (q *RocketMQQueue) EnsureReady(ctx context.Context) error {
	if q.transport == nil {
		return fmt.Errorf("rocketmq transport not set; call mq.SetRocketTransport(...)")
	}
	return q.transport.EnsureTopic(ctx, q.topic)
}

func (q *RocketMQQueue) Publish(ctx context.Context, msgs []domain.PushMessage) error {
	if q.transport == nil {
		return fmt.Errorf("rocketmq transport not set")
	}
	for i := range msgs {
		raw, err := json.Marshal(msgs[i])
		if err != nil {
			return err
		}
		if err := q.transport.Send(ctx, q.topic, raw); err != nil {
			return fmt.Errorf("rocketmq send topic=%s: %w", q.topic, err)
		}
	}
	return nil
}

func (q *RocketMQQueue) Consume(ctx context.Context, consumerID string, batch int, handler func(ctx context.Context, msg domain.PushMessage) error) error {
	if q.transport == nil {
		return fmt.Errorf("rocketmq transport not set")
	}
	concurrency := batch
	if concurrency <= 0 {
		concurrency = 16
	}
	// 限制在途 handler；若 transport 本身串行回调则不会提升吞吐，但仍提供上限保护
	sem := make(chan struct{}, concurrency)
	return q.transport.Subscribe(ctx, q.topic, q.group, consumerID, func(ctx context.Context, body []byte) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case sem <- struct{}{}:
		}
		defer func() { <-sem }()

		var msg domain.PushMessage
		if err := json.Unmarshal(body, &msg); err != nil {
			slog.Error("rocketmq unmarshal failed", "err", err)
			return nil // 坏消息丢弃，避免毒丸死循环
		}
		return handler(ctx, msg)
	})
}

func init() {
	Register(DriverRocketMQ, func(deps Deps) (*Queues, error) {
		t := getRocketTransport()
		if t == nil {
			return nil, fmt.Errorf(
				"driver %s requires Apache RocketMQ transport: go build -tags rocketmq, set mq.rocketmq.name_servers; see internal/adapter/mq/rocketmq_transport_example.go",
				DriverRocketMQ,
			)
		}
		highTopic := deps.Cfg.High.TopicOrStream()
		normalTopic := deps.Cfg.Normal.TopicOrStream()
		if highTopic == "" || normalTopic == "" {
			return nil, fmt.Errorf("high/normal topic required")
		}
		if deps.Cfg.High.Group == "" || deps.Cfg.Normal.Group == "" {
			return nil, fmt.Errorf("high/normal group required")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := t.Start(ctx); err != nil {
			return nil, fmt.Errorf("rocketmq start: %w", err)
		}
		return &Queues{
			High:   NewRocketMQQueue(highTopic, deps.Cfg.High.Group, t),
			Normal: NewRocketMQQueue(normalTopic, deps.Cfg.Normal.Group, t),
		}, nil
	})
}

var _ port.MessageQueue = (*RocketMQQueue)(nil)
