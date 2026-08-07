package push

import (
	"errors"
	"testing"

	"github.com/starlink/push/internal/domain"
)

func TestClassifyParallelOutcomesSuccessDominatesDeferred(t *testing.T) {
	err := classifyParallelOutcomes([]sendOutcome{
		{ch: domain.ChannelSMS, err: domain.ErrChannelThrottled},
		{ch: domain.ChannelInbox, ok: true},
	})
	if err != nil {
		t.Fatalf("success must dominate deferred error, got %v", err)
	}
}

func TestClassifyParallelOutcomesReturnsDeferredWithoutSuccess(t *testing.T) {
	err := classifyParallelOutcomes([]sendOutcome{
		{ch: domain.ChannelSMS, err: errors.New("permanent")},
		{ch: domain.ChannelInbox, err: domain.ErrFrequencyUnavailable},
	})
	if !errors.Is(err, domain.ErrFrequencyUnavailable) {
		t.Fatalf("expected deferred frequency error, got %v", err)
	}
}
