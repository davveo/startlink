package mq

import (
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/starlink/push/internal/config"
	"github.com/starlink/push/internal/domain"
)

func TestDLQStreamName(t *testing.T) {
	if got := DLQStreamName("starlink:push:high", ":dlq"); got != "starlink:push:high:dlq" {
		t.Fatalf("got %q", got)
	}
	if got := DLQStreamName("s", ""); got != "s:dlq" {
		t.Fatalf("empty suffix default got %q", got)
	}
}

func TestShouldDeadLetter(t *testing.T) {
	cases := []struct {
		name  string
		count int64
		max   int64
		err   error
		want  bool
	}{
		{"under max", 3, 5, errors.New("fail"), false},
		{"at max", 5, 5, errors.New("fail"), true},
		{"over max", 6, 5, errors.New("fail"), true},
		{"paused never", 99, 5, domain.ErrMainTaskPaused, false},
		{"quiet hours never", 99, 5, domain.ErrQuietHours, false},
		{"channel throttled never", 99, 5, domain.ErrChannelThrottled, false},
		{"send window never", 99, 5, domain.ErrOutsideSendWindow, false},
		{"status unavailable never", 99, 5, domain.ErrMainStatusUnavailable, false},
		{"max disabled", 10, 0, errors.New("fail"), false},
		{"nil err at max", 5, 5, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldDeadLetter(tc.count, tc.max, tc.err); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestNormalizeOptions(t *testing.T) {
	o := normalizeOptions(RedisStreamOptions{})
	if o.ClaimMinIdle <= 0 || o.ClaimBatch <= 0 || o.MaxDelivery <= 0 || o.DLQSuffix == "" {
		t.Fatalf("defaults not applied: %+v", o)
	}
	if o.DeferredMaxAge != defaultDeferredMaxAge {
		t.Fatalf("deferred max age default: %v", o.DeferredMaxAge)
	}
	if got := normalizeOptions(RedisStreamOptions{DeferredMaxAge: -1}); got.DeferredMaxAge != 0 {
		t.Fatalf("negative should disable deferred dlq: %v", got.DeferredMaxAge)
	}
}

// 退订不可判定与频控不可判定同为 fail-closed，必须一起豁免死信，
// 否则 Redis 抖动期间的营销消息会被直接丢弃。
func TestIsDeferredRequeueCoversUnavailableSentinels(t *testing.T) {
	for _, err := range []error{
		domain.ErrFrequencyUnavailable,
		domain.ErrUnsubscribeUnavailable,
		domain.ErrMainStatusUnavailable,
		domain.ErrMainTaskPaused,
		domain.ErrOutsideSendWindow,
		domain.ErrQuietHours,
		domain.ErrChannelThrottled,
	} {
		if !isDeferredRequeue(fmt.Errorf("wrapped: %w", err)) {
			t.Fatalf("%v should be deferred", err)
		}
		if ShouldDeadLetter(999, 5, err) {
			t.Fatalf("%v must never dead-letter by count", err)
		}
	}
	if isDeferredRequeue(errors.New("boom")) {
		t.Fatal("plain error must not be deferred")
	}
}

func TestStreamEntryAge(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	age, ok := streamEntryAge(strconv.FormatInt(now.Add(-90*time.Minute).UnixMilli(), 10)+"-0", now)
	if !ok || age != 90*time.Minute {
		t.Fatalf("age=%v ok=%v", age, ok)
	}
	if _, ok := streamEntryAge("not-an-id", now); ok {
		t.Fatal("unparsable id should report ok=false")
	}
	if _, ok := streamEntryAge("", now); ok {
		t.Fatal("empty id should report ok=false")
	}
}

// deferred 消息只按总滞留时长兜底，不按投递次数：
// 30s 重认领一次、max_delivery=5 时不能在 2.5 分钟后就被判死。
func TestDeferredDeadLetterUsesAgeNotDeliveryCount(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	id := func(d time.Duration) string {
		return strconv.FormatInt(now.Add(-d).UnixMilli(), 10) + "-3"
	}
	if deferredDeadLetter(id(3*time.Minute), now, defaultDeferredMaxAge) {
		t.Fatal("3min old deferred message must stay pending")
	}
	if !deferredDeadLetter(id(25*time.Hour), now, defaultDeferredMaxAge) {
		t.Fatal("25h old deferred message should dead-letter")
	}
	if deferredDeadLetter(id(25*time.Hour), now, 0) {
		t.Fatal("max age 0 disables deferred dead-letter")
	}
	if deferredDeadLetter("garbage", now, defaultDeferredMaxAge) {
		t.Fatal("unparsable id must stay pending")
	}
}

func TestOptionsFromConfigCapacity(t *testing.T) {
	approx := true
	ack := true
	o := OptionsFromConfig(config.RedisStreamMQConfig{
		ClaimMinIdleMs:  1000,
		ClaimBatch:      8,
		MaxDelivery:     3,
		DLQSuffix:       ":dlq",
		MaxLen:          1000,
		DLQMaxLen:       0, // follow
		MaxLenApprox:    &approx,
		TrimIntervalSec: 30,
		AckXDel:         &ack,
	})
	if o.MaxLen != 1000 || o.DLQMaxLen != 1000 {
		t.Fatalf("dlq should follow maxlen: %+v", o)
	}
	if !o.MaxLenApprox || !o.AckXDel || o.TrimInterval != 30*time.Second {
		t.Fatalf("capacity flags: %+v", o)
	}

	o2 := OptionsFromConfig(config.RedisStreamMQConfig{
		MaxLen:          -1,
		DLQMaxLen:       -1,
		TrimIntervalSec: -1,
	})
	if o2.MaxLen != 0 || o2.DLQMaxLen != 0 || o2.TrimInterval != 0 {
		t.Fatalf("disabled via -1: %+v", o2)
	}

	o3 := OptionsFromConfig(config.RedisStreamMQConfig{
		MaxLen:    5000,
		DLQMaxLen: 200,
	})
	if o3.DLQMaxLen != 200 {
		t.Fatalf("independent dlq maxlen: %+v", o3)
	}
}
