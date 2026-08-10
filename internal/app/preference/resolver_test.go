package preference

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/starlink/push/internal/domain"
)

func TestResolverCacheHit(t *testing.T) {
	repo := newFakeRepo()
	repo.prefs["u1"] = domain.UserPreference{UserID: "u1", MarketingOptOut: true}
	r := NewResolver(repo, time.Minute, 100)

	for i := 0; i < 5; i++ {
		p, err := r.Resolve(context.Background(), "u1")
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if p == nil || !p.MarketingOptOut {
			t.Fatalf("unexpected pref: %#v", p)
		}
	}
	if got := repo.calls(); got != 1 {
		t.Fatalf("expected 1 db read, got %d", got)
	}
}

func TestResolverNegativeCache(t *testing.T) {
	repo := newFakeRepo()
	r := NewResolver(repo, time.Minute, 100)

	for i := 0; i < 3; i++ {
		p, err := r.Resolve(context.Background(), "ghost")
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if p != nil {
			t.Fatalf("expected nil pref, got %#v", p)
		}
	}
	if got := repo.calls(); got != 1 {
		t.Fatalf("missing negative cache: %d db reads", got)
	}
	if r.Len() != 1 {
		t.Fatalf("expected negative entry cached, len=%d", r.Len())
	}
}

func TestResolverTTLExpiry(t *testing.T) {
	repo := newFakeRepo()
	repo.prefs["u1"] = domain.UserPreference{UserID: "u1"}
	r := NewResolver(repo, 20*time.Millisecond, 100)

	if _, err := r.Resolve(context.Background(), "u1"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	if _, err := r.Resolve(context.Background(), "u1"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got := repo.calls(); got != 2 {
		t.Fatalf("expected refetch after ttl, got %d reads", got)
	}
}

func TestResolverInvalidate(t *testing.T) {
	repo := newFakeRepo()
	repo.prefs["u1"] = domain.UserPreference{UserID: "u1"}
	r := NewResolver(repo, time.Minute, 100)

	if _, err := r.Resolve(context.Background(), "u1"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	r.Invalidate("u1")
	if r.Len() != 0 {
		t.Fatalf("entry not removed, len=%d", r.Len())
	}
	if _, err := r.Resolve(context.Background(), "u1"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got := repo.calls(); got != 2 {
		t.Fatalf("expected refetch after invalidate, got %d reads", got)
	}
}

func TestResolverEvictsWhenOverCapacity(t *testing.T) {
	repo := newFakeRepo()
	const max = 50
	r := NewResolver(repo, time.Minute, max)

	for i := 0; i < max*4; i++ {
		if _, err := r.Resolve(context.Background(), fmt.Sprintf("u%d", i)); err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if r.Len() > max {
			t.Fatalf("cache exceeded capacity: %d > %d", r.Len(), max)
		}
	}
}

func TestResolverEvictsExpiredFirst(t *testing.T) {
	repo := newFakeRepo()
	const max = 4
	r := NewResolver(repo, 10*time.Millisecond, max)

	for i := 0; i < max; i++ {
		if _, err := r.Resolve(context.Background(), fmt.Sprintf("old%d", i)); err != nil {
			t.Fatalf("resolve: %v", err)
		}
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := r.Resolve(context.Background(), "fresh"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if r.Len() != 1 {
		t.Fatalf("expected expired entries swept, len=%d", r.Len())
	}
}

func TestResolverReturnsRepoError(t *testing.T) {
	repo := newFakeRepo()
	repo.getErr = errors.New("db down")
	r := NewResolver(repo, time.Minute, 10)

	if _, err := r.Resolve(context.Background(), "u1"); err == nil {
		t.Fatal("expected error surfaced to caller for fail-open decision")
	}
	if r.Len() != 0 {
		t.Fatalf("errors must not be cached, len=%d", r.Len())
	}
}

func TestResolverDefaults(t *testing.T) {
	r := NewResolver(newFakeRepo(), 0, 0)
	if r.ttl != DefaultResolverTTL || r.maxEntries != DefaultResolverMaxEntries {
		t.Fatalf("bad defaults: ttl=%v max=%d", r.ttl, r.maxEntries)
	}
}
