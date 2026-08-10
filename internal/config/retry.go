package config

import (
	"strings"
	"time"

	"github.com/starlink/push/internal/domain"
)

// 退避曲线名（yaml: retry_backoff）
const (
	RetryBackoffExponential = "exponential"
	RetryBackoffLinear      = "linear"
	RetryBackoffFixed       = "fixed"
)

// ChannelRetryPolicy 单渠道发送重试策略（Gateway doSend 使用）。
// MaxRetry 为额外重试次数（总尝试 = MaxRetry+1）；Timeout 为单次 Send 超时。
type ChannelRetryPolicy struct {
	MaxRetry int
	Backoff  string // exponential | linear | fixed
	Base     time.Duration
	Max      time.Duration
	Timeout  time.Duration
}

// ChannelRetryTable 默认策略 + 按渠道覆盖。
type ChannelRetryTable struct {
	Default   ChannelRetryPolicy
	ByChannel map[domain.ChannelType]ChannelRetryPolicy
}

// For 取渠道策略；未单独配置时回落 Default。
func (t ChannelRetryTable) For(ch domain.ChannelType) ChannelRetryPolicy {
	if t.ByChannel != nil {
		if p, ok := t.ByChannel[ch]; ok {
			return p
		}
	}
	return t.Default
}

// BackoffDelay 第 attempt 次失败后的等待（attempt 从 0 起）。
func (p ChannelRetryPolicy) BackoffDelay(attempt int) time.Duration {
	base := p.Base
	if base <= 0 {
		base = 50 * time.Millisecond
	}
	max := p.Max
	if max <= 0 {
		max = 5 * time.Second
	}
	if attempt < 0 {
		attempt = 0
	}

	var d time.Duration
	switch normalizeBackoff(p.Backoff) {
	case RetryBackoffFixed:
		d = base
	case RetryBackoffLinear:
		d = base * time.Duration(attempt+1)
	default: // exponential
		shift := attempt
		if shift > 20 {
			shift = 20
		}
		d = base * time.Duration(1<<uint(shift))
	}
	if d > max {
		return max
	}
	if d < 0 {
		return max
	}
	return d
}

// BuildRetryTable 从 pusher 配置解析默认 + 分渠道策略。
func (p PusherConfig) BuildRetryTable() ChannelRetryTable {
	def := ChannelRetryPolicy{
		MaxRetry: p.MaxRetry,
		Backoff:  normalizeBackoff(p.RetryBackoff),
		Base:     msOr(p.RetryBaseMs, 50),
		Max:      msOr(p.RetryMaxMs, 5000),
		Timeout:  secOr(p.TimeoutSec, 10),
	}
	if def.MaxRetry < 0 {
		def.MaxRetry = 0
	}

	out := ChannelRetryTable{
		Default:   def,
		ByChannel: make(map[domain.ChannelType]ChannelRetryPolicy),
	}
	for name, cfg := range p.Channels {
		ch := domain.ChannelType(name)
		if !ch.Valid() {
			continue
		}
		pol := def
		if cfg.MaxRetry != nil {
			pol.MaxRetry = *cfg.MaxRetry
			if pol.MaxRetry < 0 {
				pol.MaxRetry = 0
			}
		}
		if b := strings.TrimSpace(cfg.RetryBackoff); b != "" {
			pol.Backoff = normalizeBackoff(b)
		}
		if cfg.RetryBaseMs > 0 {
			pol.Base = time.Duration(cfg.RetryBaseMs) * time.Millisecond
		}
		if cfg.RetryMaxMs > 0 {
			pol.Max = time.Duration(cfg.RetryMaxMs) * time.Millisecond
		}
		if cfg.TimeoutSec > 0 {
			pol.Timeout = time.Duration(cfg.TimeoutSec) * time.Second
		}
		out.ByChannel[ch] = pol
	}
	return out
}

func normalizeBackoff(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case RetryBackoffLinear:
		return RetryBackoffLinear
	case RetryBackoffFixed:
		return RetryBackoffFixed
	default:
		return RetryBackoffExponential
	}
}

func msOr(v, def int) time.Duration {
	if v <= 0 {
		v = def
	}
	return time.Duration(v) * time.Millisecond
}

func secOr(v, def int) time.Duration {
	if v <= 0 {
		v = def
	}
	return time.Duration(v) * time.Second
}
