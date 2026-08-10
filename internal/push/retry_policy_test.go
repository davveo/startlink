package push

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/starlink/push/internal/adapter/channel"
	"github.com/starlink/push/internal/config"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
)

type countingRetrySender struct {
	ch    domain.ChannelType
	calls atomic.Int32
}

func (s *countingRetrySender) Channel() domain.ChannelType { return s.ch }

func (s *countingRetrySender) Send(ctx context.Context, _ domain.SendRequest) (*domain.SendResult, error) {
	s.calls.Add(1)
	select {
	case <-ctx.Done():
		return &domain.SendResult{Success: false, ErrorMsg: ctx.Err().Error(), Retryable: true}, nil
	default:
	}
	return &domain.SendResult{Success: false, ErrorMsg: "always fail", Retryable: true}, nil
}

type snapTasks struct {
	port.TaskRepository
	status domain.TaskStatus
}

func (s snapTasks) GetMainTask(context.Context, uint64) (*domain.MainTask, error) {
	return &domain.MainTask{ID: 1, Status: s.status, Version: 1}, nil
}

func TestDoSendUsesPerChannelMaxRetry(t *testing.T) {
	smsSender := &countingRetrySender{ch: domain.ChannelSMS}
	inboxSender := &countingRetrySender{ch: domain.ChannelInbox}
	reg := channel.NewRegistry()
	reg.Register(smsSender)
	reg.Register(inboxSender)

	smsRetry := 1
	inboxRetry := 4
	g := NewGateway(
		reg,
		nil,
		nil,
		snapTasks{status: domain.TaskStatusRunning},
		nil,
		config.PusherConfig{
			MaxRetry:     3,
			RetryBackoff: config.RetryBackoffFixed,
			RetryBaseMs:  1,
			RetryMaxMs:   1,
			TimeoutSec:   2,
			DedupTTLSec:  60,
			Channels: map[string]config.ChannelSenderConfig{
				"sms":   {MaxRetry: &smsRetry, RetryBackoff: config.RetryBackoffFixed, RetryBaseMs: 1},
				"inbox": {MaxRetry: &inboxRetry, RetryBackoff: config.RetryBackoffFixed, RetryBaseMs: 1},
			},
		},
		config.FreqConfig{},
		nil,
		nil,
		true,
	)

	msg := domain.PushMessage{MsgID: "m1", MainTaskID: 1, UserID: "u1", Channel: domain.ChannelSMS}
	_, _ = g.doSend(context.Background(), msg, domain.ChannelSMS, "t", "b", nil, false, 0)
	if got := smsSender.calls.Load(); got != 2 { // max_retry=1 → 2 attempts
		t.Fatalf("sms attempts want 2, got %d", got)
	}

	_, _ = g.doSend(context.Background(), msg, domain.ChannelInbox, "t", "b", nil, false, 0)
	if got := inboxSender.calls.Load(); got != 5 { // max_retry=4 → 5 attempts
		t.Fatalf("inbox attempts want 5, got %d", got)
	}
}

func TestDoSendHonorsPerAttemptTimeout(t *testing.T) {
	slow := &slowSender{ch: domain.ChannelSMS, delay: 200 * time.Millisecond}
	reg := channel.NewRegistry()
	reg.Register(slow)
	zero := 0
	g := NewGateway(
		reg, nil, nil, snapTasks{status: domain.TaskStatusRunning}, nil,
		config.PusherConfig{
			MaxRetry: 0, RetryBaseMs: 1, RetryMaxMs: 1, TimeoutSec: 10,
			Channels: map[string]config.ChannelSenderConfig{
				"sms": {MaxRetry: &zero, TimeoutSec: 1}, // 1ms 太短；用极小超时
			},
		},
		config.FreqConfig{}, nil, nil, true,
	)
	// 覆盖为 5ms 超时：BuildRetryTable 已用 TimeoutSec=1s；这里直接改表
	g.retries.ByChannel[domain.ChannelSMS] = config.ChannelRetryPolicy{
		MaxRetry: 0,
		Backoff:  config.RetryBackoffFixed,
		Base:     time.Millisecond,
		Max:      time.Millisecond,
		Timeout:  5 * time.Millisecond,
	}

	start := time.Now()
	ok, err := g.doSend(context.Background(), domain.PushMessage{
		MsgID: "m", MainTaskID: 1, UserID: "u", Channel: domain.ChannelSMS,
	}, domain.ChannelSMS, "t", "b", nil, false, 0)
	elapsed := time.Since(start)
	if ok {
		t.Fatal("expected failure")
	}
	if err == nil {
		t.Fatal("expected error from failed send")
	}
	if elapsed > 150*time.Millisecond {
		t.Fatalf("timeout should cut short send, elapsed=%v", elapsed)
	}
}

type slowSender struct {
	ch    domain.ChannelType
	delay time.Duration
}

func (s *slowSender) Channel() domain.ChannelType { return s.ch }

func (s *slowSender) Send(ctx context.Context, _ domain.SendRequest) (*domain.SendResult, error) {
	select {
	case <-ctx.Done():
		return &domain.SendResult{Success: false, ErrorMsg: ctx.Err().Error(), Retryable: true}, nil
	case <-time.After(s.delay):
		return &domain.SendResult{Success: true, ProviderID: "x"}, nil
	}
}
