package redisx

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/starlink/push/internal/config"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
)

// 令牌桶 Lua：返回 {allowed(0|1), tokens}
var tokenBucketLua = redis.NewScript(`
local rate = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local cost = tonumber(ARGV[4])
local data = redis.call('HMGET', KEYS[1], 'tokens', 'ts')
local tokens = tonumber(data[1])
local ts = tonumber(data[2])
if tokens == nil then tokens = burst end
if ts == nil then ts = now end
local elapsed = (now - ts) / 1000.0
if elapsed < 0 then elapsed = 0 end
tokens = tokens + elapsed * rate
if tokens > burst then tokens = burst end
local allowed = 0
if tokens >= cost then
  tokens = tokens - cost
  allowed = 1
end
redis.call('HMSET', KEYS[1], 'tokens', tokens, 'ts', now)
redis.call('EXPIRE', KEYS[1], 120)
return {allowed, tokens}
`)

const (
	// adaptLookupTimeout 读取自适应收缩系数的最长阻塞时间
	adaptLookupTimeout = 200 * time.Millisecond
	// degradedRateFactor Redis 不可用时本地桶相对配额的保守系数
	degradedRateFactor = 0.5
	degradedKeyPrefix  = "degraded:"
)

// degraded 降级速率下限保 1，避免配额过小时完全停发
func degraded(v float64) float64 {
	return maxFloat(1, v*degradedRateFactor)
}

// ChannelQuotaLimiter 渠道 × 优先级配额；distributed=false 时用进程内桶。
type ChannelQuotaLimiter struct {
	rdb    *redis.Client
	cfg    config.ChannelQuotaConfig
	legacy int // enabled=false 时的全局 QPS

	localMu sync.Mutex
	local   map[string]*localBucket

	highSinceMu sync.Mutex
	highSince   map[string]time.Time // channel|priority -> 持续高压起点
}

type localBucket struct {
	tokens float64
	last   time.Time
	rate   float64
	burst  float64
}

func NewChannelQuotaLimiter(rdb *redis.Client, cfg config.ChannelQuotaConfig, legacyGlobalQPS int) port.ChannelLimiter {
	if legacyGlobalQPS <= 0 {
		legacyGlobalQPS = 500
	}
	return &ChannelQuotaLimiter{
		rdb:       rdb,
		cfg:       cfg,
		legacy:    legacyGlobalQPS,
		local:     make(map[string]*localBucket),
		highSince: make(map[string]time.Time),
	}
}

func (l *ChannelQuotaLimiter) Enabled() bool { return l.cfg.Enabled }

func (l *ChannelQuotaLimiter) cfgEntry(ch domain.ChannelType) (config.ChannelQuotaEntry, bool) {
	if l.cfg.Channels == nil {
		return config.ChannelQuotaEntry{}, false
	}
	e, ok := l.cfg.Channels[string(ch)]
	return e, ok
}

func (l *ChannelQuotaLimiter) AdmissionMode(channel domain.ChannelType) string {
	e, ok := l.cfgEntry(channel)
	if !ok || e.Admission == "" {
		return "soft"
	}
	return strings.ToLower(e.Admission)
}

func (l *ChannelQuotaLimiter) TargetFinishMinutes(channel domain.ChannelType) int {
	e, ok := l.cfgEntry(channel)
	if !ok || e.TargetFinishMinutes <= 0 {
		return 60
	}
	return e.TargetFinishMinutes
}

func (l *ChannelQuotaLimiter) ratesFor(ctx context.Context, ch domain.ChannelType, prio domain.Priority) (rate, burst float64, split bool) {
	e, ok := l.cfgEntry(ch)
	if !ok || e.QPS <= 0 {
		return 0, 0, false
	}
	ratio := e.HighReserveRatio
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	total := float64(e.QPS) * l.adaptMult(ctx, ch)
	b := float64(e.Burst)
	if b <= 0 {
		b = float64(e.QPS)
	}
	if ratio <= 0 {
		return total, b, false // high/normal 共享同一桶
	}
	if prio.Normalize().IsHigh() {
		return total * ratio, maxFloat(1, b*ratio), true
	}
	return total * (1 - ratio), maxFloat(1, b*(1-ratio)), true
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// AvailableQPS 属 port.ChannelLimiter 契约（无 ctx）。内部读 Redis 自适应系数，
// 用短超时兜底，避免半开连接把调用方阻塞到读超时。
func (l *ChannelQuotaLimiter) AvailableQPS(channel domain.ChannelType, priority domain.Priority) float64 {
	if !l.cfg.Enabled {
		return float64(l.legacy)
	}
	ctx, cancel := context.WithTimeout(context.Background(), adaptLookupTimeout)
	defer cancel()
	rate, _, _ := l.ratesFor(ctx, channel, priority)
	return rate
}

func (l *ChannelQuotaLimiter) adaptKey(ch domain.ChannelType) string {
	return l.cfg.RedisKeyPrefix + "adapt:" + string(ch)
}

func (l *ChannelQuotaLimiter) adaptMult(ctx context.Context, ch domain.ChannelType) float64 {
	if !l.cfg.Adaptive.Enabled {
		return 1
	}
	if l.rdb == nil || !l.cfg.Distributed {
		l.localMu.Lock()
		defer l.localMu.Unlock()
		if b, ok := l.local["adapt:"+string(ch)]; ok && time.Since(b.last) < time.Duration(l.cfg.Adaptive.RecoverIntervalSec)*time.Second {
			if b.rate > 0 && b.rate < 1 {
				return b.rate
			}
		}
		return 1
	}
	v, err := l.rdb.Get(ctx, l.adaptKey(ch)).Float64()
	if err != nil || v <= 0 || v > 1 {
		return 1
	}
	return v
}

func (l *ChannelQuotaLimiter) ObserveVendorThrottle(ctx context.Context, channel domain.ChannelType) {
	if !l.cfg.Enabled || !l.cfg.Adaptive.Enabled {
		return
	}
	factor := l.cfg.Adaptive.ShrinkFactor
	if factor <= 0 || factor > 1 {
		factor = 0.5
	}
	ttl := time.Duration(l.cfg.Adaptive.RecoverIntervalSec) * time.Second
	slog.Warn("channel quota adaptive shrink", "channel", channel, "factor", factor, "ttl_sec", l.cfg.Adaptive.RecoverIntervalSec)
	if l.rdb != nil && l.cfg.Distributed {
		_ = l.rdb.Set(ctx, l.adaptKey(channel), factor, ttl).Err()
		return
	}
	l.localMu.Lock()
	l.local["adapt:"+string(channel)] = &localBucket{rate: factor, last: time.Now()}
	l.localMu.Unlock()
}

func (l *ChannelQuotaLimiter) bucketKey(scope, prio string) string {
	if prio == "" {
		return l.cfg.RedisKeyPrefix + scope
	}
	return l.cfg.RedisKeyPrefix + scope + ":" + prio
}

func (l *ChannelQuotaLimiter) Wait(ctx context.Context, channel domain.ChannelType, priority domain.Priority) error {
	timeout := l.cfg.WaitTimeout()
	wctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if !l.cfg.Enabled {
		return l.waitLocal(wctx, "legacy", float64(l.legacy), float64(l.legacy))
	}

	global := float64(l.cfg.GlobalQPS)
	if global > 0 {
		if err := l.waitOne(wctx, "global", "", global, global); err != nil {
			return err
		}
	}

	rate, burst, split := l.ratesFor(wctx, channel, priority)
	if rate <= 0 {
		slog.Warn("channel quota missing, fail-open after global", "channel", channel)
		return nil
	}
	prioKey := ""
	if split {
		prioKey = string(priority.Normalize())
	}
	if err := l.waitOne(wctx, string(channel), prioKey, rate, burst); err != nil {
		slog.Info("channel quota timeout",
			"channel", channel, "priority", priority.Normalize(), "stage", "quota_timeout")
		return domain.ErrChannelThrottled
	}
	return nil
}

func (l *ChannelQuotaLimiter) waitOne(ctx context.Context, scope, prio string, rate, burst float64) error {
	if l.cfg.Distributed && l.rdb != nil {
		return l.waitRedis(ctx, scope, prio, rate, burst)
	}
	key := scope
	if prio != "" {
		key = scope + ":" + prio
	}
	return l.waitLocal(ctx, key, rate, burst)
}

func (l *ChannelQuotaLimiter) waitRedis(ctx context.Context, scope, prio string, rate, burst float64) error {
	key := l.bucketKey(scope, prio)
	for {
		now := time.Now().UnixMilli()
		res, err := tokenBucketLua.Run(ctx, l.rdb, []string{key}, rate, burst, now, 1).Slice()
		if err != nil {
			if ctx.Err() != nil {
				return domain.ErrChannelThrottled
			}
			// 无限放行会让多实例在 Redis 故障期间全速打供应商；
			// 降级到进程内桶并按保守系数收缩（假定多实例共享同一配额）。
			slog.Warn("quota redis error, degrade to local bucket", "key", key, "err", err)
			return l.waitLocal(ctx, degradedKeyPrefix+key, degraded(rate), degraded(burst))
		}
		allowed, _ := toInt64(res[0])
		if allowed == 1 {
			return nil
		}
		select {
		case <-ctx.Done():
			return domain.ErrChannelThrottled
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (l *ChannelQuotaLimiter) waitLocal(ctx context.Context, key string, rate, burst float64) error {
	for {
		if l.takeLocal(key, rate, burst) {
			return nil
		}
		select {
		case <-ctx.Done():
			return domain.ErrChannelThrottled
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (l *ChannelQuotaLimiter) takeLocal(key string, rate, burst float64) bool {
	l.localMu.Lock()
	defer l.localMu.Unlock()
	b, ok := l.local[key]
	now := time.Now()
	if !ok {
		b = &localBucket{tokens: burst, last: now, rate: rate, burst: burst}
		l.local[key] = b
	}
	b.rate, b.burst = rate, burst
	elapsed := now.Sub(b.last).Seconds()
	b.tokens += elapsed * rate
	if b.tokens > burst {
		b.tokens = burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (l *ChannelQuotaLimiter) Utilization(ctx context.Context, channel domain.ChannelType, priority domain.Priority) (float64, error) {
	if !l.cfg.Enabled {
		return 0, nil
	}
	rate, burst, split := l.ratesFor(ctx, channel, priority)
	if rate <= 0 || burst <= 0 {
		return 0, nil
	}
	prioKey := ""
	if split {
		prioKey = string(priority.Normalize())
	}
	var tokens float64
	if l.cfg.Distributed && l.rdb != nil {
		key := l.bucketKey(string(channel), prioKey)
		m, err := l.rdb.HGetAll(ctx, key).Result()
		if err != nil {
			return 0, err
		}
		if m["tokens"] == "" {
			return 0, nil
		}
		fmt.Sscanf(m["tokens"], "%f", &tokens)
	} else {
		key := string(channel)
		if prioKey != "" {
			key = string(channel) + ":" + prioKey
		}
		l.localMu.Lock()
		if b, ok := l.local[key]; ok {
			tokens = b.tokens
		} else {
			tokens = burst
		}
		l.localMu.Unlock()
	}
	util := 1 - tokens/burst
	if util < 0 {
		util = 0
	}
	if util > 1 {
		util = 1
	}

	mapKey := string(channel) + "|" + string(priority.Normalize())
	l.highSinceMu.Lock()
	defer l.highSinceMu.Unlock()
	if util >= l.cfg.Backpressure.HighWatermark {
		if l.highSince[mapKey].IsZero() {
			l.highSince[mapKey] = time.Now()
		}
	} else if util <= l.cfg.Backpressure.LowWatermark {
		delete(l.highSince, mapKey)
	}
	return util, nil
}

// SustainedHigh 利用率持续高于高水位超过 SustainSec
func (l *ChannelQuotaLimiter) SustainedHigh(channel domain.ChannelType, priority domain.Priority, util float64) bool {
	if !l.cfg.Backpressure.Enabled {
		return false
	}
	if util < l.cfg.Backpressure.HighWatermark {
		return false
	}
	mapKey := string(channel) + "|" + string(priority.Normalize())
	l.highSinceMu.Lock()
	defer l.highSinceMu.Unlock()
	since, ok := l.highSince[mapKey]
	if !ok || since.IsZero() {
		return false
	}
	return time.Since(since) >= time.Duration(l.cfg.Backpressure.SustainSec)*time.Second
}

func (l *ChannelQuotaLimiter) Backpressure() config.QuotaBackpressureConfig {
	return l.cfg.Backpressure
}

func (l *ChannelQuotaLimiter) OverCapacityAction() string {
	return strings.ToLower(l.cfg.OverCapacityAction)
}

func toInt64(v any) (int64, error) {
	switch x := v.(type) {
	case int64:
		return x, nil
	case int:
		return int64(x), nil
	case string:
		var n int64
		_, err := fmt.Sscan(x, &n)
		return n, err
	default:
		return 0, fmt.Errorf("unexpected type %T", v)
	}
}

// QuotaHelpers 扩展接口：Scheduler/拆分需要反压与超容量动作（仍实现 ChannelLimiter）
type QuotaHelpers interface {
	port.ChannelLimiter
	SustainedHigh(channel domain.ChannelType, priority domain.Priority, util float64) bool
	Backpressure() config.QuotaBackpressureConfig
	OverCapacityAction() string
}

var _ port.ChannelLimiter = (*ChannelQuotaLimiter)(nil)
var _ QuotaHelpers = (*ChannelQuotaLimiter)(nil)
