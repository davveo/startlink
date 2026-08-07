package push

import (
	"errors"
	"testing"

	"github.com/starlink/push/internal/domain"
)

func TestClassifyAllSuccessOutcomesAllOK(t *testing.T) {
	if err := classifyAllSuccessOutcomes([]sendOutcome{
		{ch: domain.ChannelSMS, ok: true},
		{ch: domain.ChannelInbox, ok: true},
	}); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

// 频控不可判定属于本轮不可判定，必须盖过其它渠道的抑制终态，
// 否则消息会被判死，永远等不到 Redis 恢复后的重投。
func TestClassifyAllSuccessOutcomesRetryableBeatsSuppressed(t *testing.T) {
	err := classifyAllSuccessOutcomes([]sendOutcome{
		{ch: domain.ChannelSMS, err: errSuppressed},
		{ch: domain.ChannelInbox, err: domain.ErrFrequencyUnavailable},
	})
	if !errors.Is(err, domain.ErrFrequencyUnavailable) {
		t.Fatalf("expected retryable frequency error, got %v", err)
	}
}

// 抑制是确定性终态，重试 5 次结果相同且最终进 DLQ：
// 视为已处理直接 ACK，结果由 push_records 的 suppressed 状态体现。
func TestClassifyAllSuccessOutcomesSuppressedAcks(t *testing.T) {
	if err := classifyAllSuccessOutcomes([]sendOutcome{
		{ch: domain.ChannelSMS, ok: true},
		{ch: domain.ChannelInbox, err: errSuppressed},
	}); err != nil {
		t.Fatalf("suppressed must not trigger retry, got %v", err)
	}
	if err := classifyAllSuccessOutcomes([]sendOutcome{
		{ch: domain.ChannelSMS, err: errSuppressed},
		{ch: domain.ChannelInbox, err: errSuppressed},
	}); err != nil {
		t.Fatalf("all suppressed must not trigger retry, got %v", err)
	}
}

// 抑制被 ACK 之后，同批次里真正的失败仍必须冒泡出来触发重试
func TestClassifyAllSuccessOutcomesSuppressedDoesNotMaskFailure(t *testing.T) {
	sentinel := errors.New("provider down")
	err := classifyAllSuccessOutcomes([]sendOutcome{
		{ch: domain.ChannelSMS, err: errSuppressed},
		{ch: domain.ChannelInbox, err: sentinel},
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected provider error, got %v", err)
	}
}

func TestClassifyAllSuccessOutcomesPlainFailure(t *testing.T) {
	sentinel := errors.New("provider down")
	err := classifyAllSuccessOutcomes([]sendOutcome{
		{ch: domain.ChannelSMS, ok: true},
		{ch: domain.ChannelInbox, err: sentinel},
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected provider error, got %v", err)
	}
}
