package mq

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/starlink/push/internal/domain"
)

func TestMemoryQueueConcurrentConsume(t *testing.T) {
	q := NewMemoryQueue("test", 64)
	const total = 20
	const concurrency = 4

	msgs := make([]domain.PushMessage, total)
	for i := 0; i < total; i++ {
		msgs[i] = domain.PushMessage{MsgID: strconv.Itoa(i), UserID: "u"}
	}
	if err := q.Publish(context.Background(), msgs); err != nil {
		t.Fatal(err)
	}

	var (
		inflight    atomic.Int64
		maxInflight atomic.Int64
		done        atomic.Int64
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = q.Consume(ctx, "c1", concurrency, func(ctx context.Context, msg domain.PushMessage) error {
			cur := inflight.Add(1)
			for {
				old := maxInflight.Load()
				if cur <= old || maxInflight.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(30 * time.Millisecond)
			inflight.Add(-1)
			if done.Add(1) >= total {
				cancel()
			}
			return nil
		})
	}()

	wg.Wait()
	if got := done.Load(); got != total {
		t.Fatalf("processed %d want %d", got, total)
	}
	if got := maxInflight.Load(); got < 2 || got > concurrency {
		t.Fatalf("max inflight=%d, want in [2,%d]", got, concurrency)
	}
}
