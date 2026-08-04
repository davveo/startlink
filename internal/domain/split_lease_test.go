package domain

import (
	"testing"
	"time"
)

func TestSplitLeaseExpired(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-30 * time.Second)
	stale := now.Add(-120 * time.Second)

	if SplitLeaseExpired(&fresh, 90, now) {
		t.Fatal("fresh lease should not expire")
	}
	if !SplitLeaseExpired(&stale, 90, now) {
		t.Fatal("stale lease should expire")
	}
	if !SplitLeaseExpired(nil, 90, now) {
		t.Fatal("nil lease should expire")
	}
}
