package domain

import (
	"testing"
	"time"
)

func TestIntersectChannels(t *testing.T) {
	task := []ChannelType{ChannelSMS, ChannelInbox, ChannelEmail}
	user := []ChannelType{ChannelInbox, ChannelAppPush}
	got := IntersectChannels(task, user)
	if len(got) != 1 || got[0] != ChannelInbox {
		t.Fatalf("got %#v", got)
	}
	if len(IntersectChannels(task, nil)) != 3 {
		t.Fatal("empty user should keep task chain")
	}
	if len(IntersectChannels(task, []ChannelType{ChannelWecom})) != 0 {
		t.Fatal("no overlap")
	}
}

func TestInSendWindowsAndQuietHours(t *testing.T) {
	if !InSendWindows(nil, time.Now()) {
		t.Fatal("empty windows allow all")
	}
	// 22:00-08:00 quiet
	noon := time.Date(2026, 8, 4, 12, 0, 0, 0, time.Local)
	if InQuietHours("22:00", "08:00", noon) {
		t.Fatal("noon should not be quiet")
	}
	night := time.Date(2026, 8, 4, 23, 0, 0, 0, time.Local)
	if !InQuietHours("22:00", "08:00", night) {
		t.Fatal("23:00 should be quiet")
	}
}
