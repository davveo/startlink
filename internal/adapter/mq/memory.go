package mq

import (
	"context"
	"fmt"
	"sync"

	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
)

// MemoryQueue 进程内队列，便于单测 / 本地无中间件联调
type MemoryQueue struct {
	name   string
	ch     chan domain.PushMessage
	mu     sync.Mutex
	closed bool
}

func NewMemoryQueue(name string, buffer int) *MemoryQueue {
	if buffer <= 0 {
		buffer = 1024
	}
	return &MemoryQueue{name: name, ch: make(chan domain.PushMessage, buffer)}
}

func (q *MemoryQueue) EnsureReady(context.Context) error { return nil }

func (q *MemoryQueue) Publish(ctx context.Context, msgs []domain.PushMessage) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return fmt.Errorf("memory queue %s closed", q.name)
	}
	for i := range msgs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case q.ch <- msgs[i]:
		}
	}
	return nil
}

func (q *MemoryQueue) Consume(ctx context.Context, _ string, batch int, handler func(ctx context.Context, msg domain.PushMessage) error) error {
	concurrency := batch
	if concurrency <= 0 {
		concurrency = 16
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	defer wg.Wait()

	dispatch := func(msg domain.PushMessage) bool {
		select {
		case <-ctx.Done():
			return false
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(m domain.PushMessage) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := handler(ctx, m); err != nil {
				// 失败重新入队（简单重试）
				select {
				case q.ch <- m:
				default:
				}
			}
		}(msg)
		return true
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-q.ch:
			if !ok {
				return nil
			}
			if !dispatch(msg) {
				return ctx.Err()
			}
		}
	}
}

func (q *MemoryQueue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.closed {
		close(q.ch)
		q.closed = true
	}
	return nil
}

func init() {
	Register(DriverMemory, func(deps Deps) (*Queues, error) {
		buf := deps.Cfg.Memory.BufferSize
		if buf <= 0 {
			buf = 4096
		}
		return &Queues{
			High:   NewMemoryQueue("high", buf),
			Normal: NewMemoryQueue("normal", buf),
		}, nil
	})
}

var _ port.MessageQueue = (*MemoryQueue)(nil)
