package config

import (
	"testing"
	"time"

	"github.com/starlink/push/internal/domain"
)

func TestChannelRetryPolicyBackoff(t *testing.T) {
	p := ChannelRetryPolicy{
		Backoff: RetryBackoffExponential,
		Base:    50 * time.Millisecond,
		Max:     5 * time.Second,
	}
	if got := p.BackoffDelay(0); got != 50*time.Millisecond {
		t.Fatalf("exp attempt0: got %v", got)
	}
	if got := p.BackoffDelay(1); got != 100*time.Millisecond {
		t.Fatalf("exp attempt1: got %v", got)
	}
	if got := p.BackoffDelay(2); got != 200*time.Millisecond {
		t.Fatalf("exp attempt2: got %v", got)
	}
	// 封顶
	if got := p.BackoffDelay(20); got != 5*time.Second {
		t.Fatalf("exp capped: got %v", got)
	}

	p.Backoff = RetryBackoffLinear
	if got := p.BackoffDelay(0); got != 50*time.Millisecond {
		t.Fatalf("linear0: got %v", got)
	}
	if got := p.BackoffDelay(3); got != 200*time.Millisecond {
		t.Fatalf("linear3: got %v", got)
	}

	p.Backoff = RetryBackoffFixed
	if got := p.BackoffDelay(5); got != 50*time.Millisecond {
		t.Fatalf("fixed: got %v", got)
	}
}

func TestPusherBuildRetryTablePerChannel(t *testing.T) {
	smsRetry := 5
	cfg := PusherConfig{
		MaxRetry:     3,
		RetryBackoff: RetryBackoffExponential,
		RetryBaseMs:  50,
		RetryMaxMs:   5000,
		TimeoutSec:   10,
		Channels: map[string]ChannelSenderConfig{
			"sms": {
				Mode:         "http",
				URL:          "http://example/sms",
				TimeoutSec:   3,
				MaxRetry:     &smsRetry,
				RetryBackoff: RetryBackoffFixed,
				RetryBaseMs:  200,
				RetryMaxMs:   200,
			},
			"inbox": {
				Mode: "stub",
				// 未覆盖 → 用默认
			},
		},
	}
	table := cfg.BuildRetryTable()
	if table.Default.MaxRetry != 3 || table.Default.Backoff != RetryBackoffExponential {
		t.Fatalf("default: %+v", table.Default)
	}
	sms := table.For(domain.ChannelSMS)
	if sms.MaxRetry != 5 || sms.Backoff != RetryBackoffFixed || sms.Base != 200*time.Millisecond {
		t.Fatalf("sms: %+v", sms)
	}
	if sms.Timeout != 3*time.Second {
		t.Fatalf("sms timeout: %v", sms.Timeout)
	}
	inbox := table.For(domain.ChannelInbox)
	if inbox.MaxRetry != 3 || inbox.Timeout != 10*time.Second {
		t.Fatalf("inbox fallback: %+v", inbox)
	}
	// 未配置渠道也回落默认
	email := table.For(domain.ChannelEmail)
	if email.MaxRetry != 3 {
		t.Fatalf("email fallback: %+v", email)
	}
}
