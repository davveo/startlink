package scheduler

import (
	"errors"
	"testing"

	"github.com/starlink/push/internal/domain"
)

func TestAdvancePageToken(t *testing.T) {
	_, err := advancePageToken("0", &domain.AudiencePage{HasMore: true, NextPageToken: ""})
	if !errors.Is(err, domain.ErrAudiencePageStuck) {
		t.Fatalf("want stuck on empty token, got %v", err)
	}
	_, err = advancePageToken("abc", &domain.AudiencePage{NextPageToken: "abc"})
	if !errors.Is(err, domain.ErrAudiencePageStuck) {
		t.Fatalf("want stuck on same token, got %v", err)
	}
	next, err := advancePageToken("0", &domain.AudiencePage{NextPageToken: "200"})
	if err != nil || next != "200" {
		t.Fatalf("got next=%q err=%v", next, err)
	}
}
