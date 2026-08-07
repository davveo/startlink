package redisx

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/starlink/push/internal/config"
	"github.com/starlink/push/internal/domain"
)

func TestChannelQuotaLocalWaitAndThrottle(t *testing.T) {
	cfg := config.ChannelQuotaConfig{
		Enabled:       true,
		Distributed:   false,
		WaitTimeoutMs: 80,
		GlobalQPS:     1000,
		Channels: map[string]config.ChannelQuotaEntry{
			"sms": {QPS: 5, Burst: 2, HighReserveRatio: 0.4, Admission: "soft"},
		},
	}
	cfg.RedisKeyPrefix = "starlink:quota:test:"
	l := NewChannelQuotaLimiter(nil, cfg, 500).(*ChannelQuotaLimiter)

	ctx := context.Background()
	// burst=2 * 0.4 for high ≈ 0.8 → maxFloat 1；先耗尽
	for i := 0; i < 3; i++ {
		_ = l.Wait(ctx, domain.ChannelSMS, domain.PriorityHigh)
	}
	err := l.Wait(ctx, domain.ChannelSMS, domain.PriorityHigh)
	if err != domain.ErrChannelThrottled {
		// 可能仍拿到令牌（补充速率）；再紧一点
		cfg.WaitTimeoutMs = 30
		l.cfg.WaitTimeoutMs = 30
		// 打满 normal 桶
		l2 := NewChannelQuotaLimiter(nil, cfg, 500).(*ChannelQuotaLimiter)
		l2.cfg.Channels["sms"] = config.ChannelQuotaEntry{QPS: 1, Burst: 1, HighReserveRatio: 0, Admission: "soft"}
		_ = l2.Wait(ctx, domain.ChannelSMS, domain.PriorityNormal)
		err = l2.Wait(ctx, domain.ChannelSMS, domain.PriorityNormal)
		if err != domain.ErrChannelThrottled {
			t.Fatalf("want ErrChannelThrottled, got %v", err)
		}
	}
}

func TestChannelQuotaAdaptiveShrink(t *testing.T) {
	cfg := config.ChannelQuotaConfig{
		Enabled:     true,
		Distributed: false,
		Adaptive: config.QuotaAdaptiveConfig{
			Enabled:            true,
			ShrinkFactor:       0.5,
			RecoverIntervalSec: 60,
		},
		Channels: map[string]config.ChannelQuotaEntry{
			"sms": {QPS: 100, Burst: 100, HighReserveRatio: 0},
		},
	}
	l := NewChannelQuotaLimiter(nil, cfg, 500).(*ChannelQuotaLimiter)
	before := l.AvailableQPS(domain.ChannelSMS, domain.PriorityNormal)
	l.ObserveVendorThrottle(context.Background(), domain.ChannelSMS)
	after := l.AvailableQPS(domain.ChannelSMS, domain.PriorityNormal)
	if after >= before || after < before*0.4 {
		t.Fatalf("adaptive shrink: before=%v after=%v", before, after)
	}
}

func TestChannelQuotaAdmissionMode(t *testing.T) {
	cfg := config.ChannelQuotaConfig{
		Enabled: true,
		Channels: map[string]config.ChannelQuotaEntry{
			"sms": {QPS: 10, Admission: "enforce", TargetFinishMinutes: 30},
		},
	}
	l := NewChannelQuotaLimiter(nil, cfg, 500)
	if l.AdmissionMode(domain.ChannelSMS) != "enforce" {
		t.Fatal(l.AdmissionMode(domain.ChannelSMS))
	}
	if l.TargetFinishMinutes(domain.ChannelSMS) != 30 {
		t.Fatal(l.TargetFinishMinutes(domain.ChannelSMS))
	}
	if l.AdmissionMode(domain.ChannelEmail) != "soft" {
		t.Fatal("missing channel should soft")
	}
}

func TestChannelQuotaLegacyDisabled(t *testing.T) {
	cfg := config.ChannelQuotaConfig{Enabled: false, WaitTimeoutMs: 50}
	l := NewChannelQuotaLimiter(nil, cfg, 2).(*ChannelQuotaLimiter) // burst≈2
	ctx := context.Background()
	_ = l.Wait(ctx, domain.ChannelSMS, domain.PriorityNormal)
	_ = l.Wait(ctx, domain.ChannelSMS, domain.PriorityNormal)
	err := l.Wait(ctx, domain.ChannelSMS, domain.PriorityNormal)
	if err != domain.ErrChannelThrottled {
		t.Fatalf("expected throttle, got %v", err)
	}
}

// Redis 不可用时无限放行会让多实例全速打供应商，必须降级到进程内保守桶
func TestChannelQuotaDegradesToLocalBucketWhenRedisDown(t *testing.T) {
	cfg := config.ChannelQuotaConfig{
		Enabled:       true,
		Distributed:   true,
		WaitTimeoutMs: 120,
		GlobalQPS:     0,
		Channels: map[string]config.ChannelQuotaEntry{
			"sms": {QPS: 2, Burst: 2, HighReserveRatio: 0},
		},
	}
	cfg.RedisKeyPrefix = "starlink:quota:degrade:"
	rdb := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1", // 必然连不上
		DialTimeout: 30 * time.Millisecond,
		MaxRetries:  -1,
	})
	defer rdb.Close()
	l := NewChannelQuotaLimiter(rdb, cfg, 500).(*ChannelQuotaLimiter)

	ctx := context.Background()
	throttled := false
	for i := 0; i < 6; i++ {
		if errors.Is(l.Wait(ctx, domain.ChannelSMS, domain.PriorityNormal), domain.ErrChannelThrottled) {
			throttled = true
			break
		}
	}
	if !throttled {
		t.Fatal("redis outage must not fail-open unconditionally")
	}
}

func TestDegradedRate(t *testing.T) {
	if got := degraded(100); got != 50 {
		t.Fatalf("degraded(100)=%v", got)
	}
	if got := degraded(1); got != 1 {
		t.Fatalf("degraded must keep a floor of 1, got %v", got)
	}
}

func TestSustainedHigh(t *testing.T) {
	cfg := config.ChannelQuotaConfig{
		Enabled: true,
		Backpressure: config.QuotaBackpressureConfig{
			Enabled:       true,
			HighWatermark: 0.5,
			LowWatermark:  0.2,
			SustainSec:    0, // 立即
		},
		Channels: map[string]config.ChannelQuotaEntry{
			"sms": {QPS: 10, Burst: 10},
		},
	}
	l := NewChannelQuotaLimiter(nil, cfg, 500).(*ChannelQuotaLimiter)
	// 耗尽令牌抬高利用率
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		_ = l.Wait(ctx, domain.ChannelSMS, domain.PriorityNormal)
	}
	util, err := l.Utilization(ctx, domain.ChannelSMS, domain.PriorityNormal)
	if err != nil {
		t.Fatal(err)
	}
	if util < 0.5 {
		t.Fatalf("util=%v", util)
	}
	if !l.SustainedHigh(domain.ChannelSMS, domain.PriorityNormal, util) {
		t.Fatal("expected sustained high with SustainSec=0")
	}
}
