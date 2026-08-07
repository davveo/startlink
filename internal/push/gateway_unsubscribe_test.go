package push

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/starlink/push/internal/domain"
)

type stubUnsub struct {
	unsubscribed bool
	err          error
}

func (s stubUnsub) IsUnsubscribed(context.Context, string, domain.ChannelType) (bool, error) {
	return s.unsubscribed, s.err
}

// 退订检查读 Redis 且对 normal 优先级 fail-closed：
// 不可判定必须包装成 deferred 哨兵，否则重试耗尽就进 DLQ，fail-closed 变成丢消息。
func TestCheckUnsubscribedUnavailableIsDeferred(t *testing.T) {
	g := &Gateway{unsub: stubUnsub{err: errors.New("redis down")}}
	msg := domain.PushMessage{UserID: "u1", Priority: domain.PriorityNormal}

	err := g.checkUnsubscribed(context.Background(), msg, domain.ChannelSMS)
	if !errors.Is(err, domain.ErrUnsubscribeUnavailable) {
		t.Fatalf("expected unsubscribe-unavailable sentinel, got %v", err)
	}
	if !isRetryableSendErr(err) {
		t.Fatal("unsubscribe-unavailable must be retryable, not a hard failure")
	}
}

func TestCheckUnsubscribedHighPriorityFailsOpen(t *testing.T) {
	g := &Gateway{unsub: stubUnsub{err: errors.New("redis down")}}
	msg := domain.PushMessage{UserID: "u1", Priority: domain.PriorityHigh}

	if err := g.checkUnsubscribed(context.Background(), msg, domain.ChannelSMS); err != nil {
		t.Fatalf("transactional message should fail-open, got %v", err)
	}
}

func TestCheckUnsubscribedDeniesUnsubscribed(t *testing.T) {
	g := &Gateway{unsub: stubUnsub{unsubscribed: true}}
	err := g.checkUnsubscribed(context.Background(), domain.PushMessage{UserID: "u1"}, domain.ChannelSMS)
	if !errors.Is(err, domain.ErrUnsubscribed) {
		t.Fatalf("expected unsubscribed, got %v", err)
	}
	if isRetryableSendErr(err) {
		t.Fatal("unsubscribed is terminal, must not be retryable")
	}
}

func TestLoadLocationCached(t *testing.T) {
	first := loadLocationCached("Asia/Shanghai")
	if first == nil {
		t.Fatal("expected a location")
	}
	if second := loadLocationCached("Asia/Shanghai"); second != first {
		t.Fatal("expected the cached *time.Location instance")
	}
	if loadLocationCached("  ") != nil || loadLocationCached("Not/AZone") != nil {
		t.Fatal("blank and invalid zones should degrade to local time")
	}
	// 非法时区名负缓存后仍返回 nil
	if loadLocationCached("Not/AZone") != nil {
		t.Fatal("invalid zone must stay nil after negative caching")
	}
}

func TestSweepMainCacheDropsExpiredEntries(t *testing.T) {
	g := &Gateway{mainCache: make(map[uint64]mainCacheEntry)}
	now := time.Now()
	for i := 0; i < mainCacheSoftLimit+10; i++ {
		g.mainCache[uint64(i)] = mainCacheEntry{expiresAt: now.Add(-time.Second)}
	}
	g.mainCache[999999] = mainCacheEntry{expiresAt: now.Add(time.Minute)}

	g.sweepMainCacheLocked(now)

	if len(g.mainCache) != 1 {
		t.Fatalf("expired entries should be evicted, size=%d", len(g.mainCache))
	}
	if _, ok := g.mainCache[999999]; !ok {
		t.Fatal("live entry must be kept")
	}
}
