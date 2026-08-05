package mq

import (
	"errors"
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
