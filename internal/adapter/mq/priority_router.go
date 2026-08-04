package mq

import (
	"context"
	"fmt"

	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
)

// PriorityRouter 按消息优先级分流到独立队列（驱动无关）
type PriorityRouter struct {
	driver string
	high   port.MessageQueue
	normal port.MessageQueue
}

func NewPriorityRouter(driver string, high, normal port.MessageQueue) *PriorityRouter {
	if driver == "" {
		driver = "unknown"
	}
	return &PriorityRouter{driver: driver, high: high, normal: normal}
}

func (r *PriorityRouter) Driver() string          { return r.driver }
func (r *PriorityRouter) High() port.MessageQueue { return r.high }
func (r *PriorityRouter) Normal() port.MessageQueue {
	return r.normal
}

func (r *PriorityRouter) Queue(p domain.Priority) port.MessageQueue {
	if p.IsHigh() {
		return r.high
	}
	return r.normal
}

func (r *PriorityRouter) EnsureReady(ctx context.Context) error {
	if err := r.high.EnsureReady(ctx); err != nil {
		return fmt.Errorf("high queue: %w", err)
	}
	if err := r.normal.EnsureReady(ctx); err != nil {
		return fmt.Errorf("normal queue: %w", err)
	}
	return nil
}

// EnsureGroup 兼容旧名
func (r *PriorityRouter) EnsureGroup(ctx context.Context) error {
	return r.EnsureReady(ctx)
}

func (r *PriorityRouter) Publish(ctx context.Context, msgs []domain.PushMessage) error {
	if len(msgs) == 0 {
		return nil
	}
	var high, normal []domain.PushMessage
	for i := range msgs {
		if msgs[i].Priority.IsHigh() {
			high = append(high, msgs[i])
		} else {
			normal = append(normal, msgs[i])
		}
	}
	if len(high) > 0 {
		if err := r.high.Publish(ctx, high); err != nil {
			return fmt.Errorf("publish high: %w", err)
		}
	}
	if len(normal) > 0 {
		if err := r.normal.Publish(ctx, normal); err != nil {
			return fmt.Errorf("publish normal: %w", err)
		}
	}
	return nil
}

func (r *PriorityRouter) Consume(ctx context.Context, consumerID string, batch int, handler func(ctx context.Context, msg domain.PushMessage) error) error {
	return r.normal.Consume(ctx, consumerID, batch, handler)
}

func (r *PriorityRouter) Close() error {
	var first error
	for _, q := range []port.MessageQueue{r.high, r.normal} {
		if c, ok := q.(interface{ Close() error }); ok {
			if err := c.Close(); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}

// 编译期断言
var (
	_ port.PriorityBroker = (*PriorityRouter)(nil)
	_ port.MessageQueue   = (*PriorityRouter)(nil)
)
