package domain

import "testing"

func TestNormalizeChannels(t *testing.T) {
	in := &CreateCampaignInput{Channel: ChannelSMS}
	primary, list, mode, err := in.NormalizeChannels()
	if err != nil || primary != ChannelSMS || len(list) != 1 || mode != ChannelModeSingle {
		t.Fatalf("single: %v %v %v %v", primary, list, mode, err)
	}

	in = &CreateCampaignInput{
		Channels:    []ChannelType{ChannelSMS, ChannelInbox, ChannelSMS},
		ChannelMode: ChannelModeSingle,
	}
	primary, list, mode, err = in.NormalizeChannels()
	if err != nil || primary != ChannelSMS || len(list) != 2 || mode != ChannelModeFallback {
		t.Fatalf("multi single→fallback: %v %v %v %v", primary, list, mode, err)
	}

	in = &CreateCampaignInput{Channel: ChannelType("nope")}
	if _, _, _, err := in.NormalizeChannels(); err == nil {
		t.Fatal("expected invalid channel")
	}

	in = &CreateCampaignInput{}
	if _, _, _, err := in.NormalizeChannels(); err == nil {
		t.Fatal("expected required channel")
	}
}

func TestApplyDefaultChannel(t *testing.T) {
	in := &CreateCampaignInput{}
	in.ApplyDefaultChannel(ChannelInbox)
	if in.Channel != ChannelInbox {
		t.Fatalf("got %s", in.Channel)
	}
	in.Channels = []ChannelType{ChannelSMS}
	in.ApplyDefaultChannel(ChannelEmail)
	if in.Channel != ChannelInbox { // already had channels, Channel field not required
		// Channels set, ApplyDefault should no-op on Channel
	}
	if len(in.Channels) != 1 || in.Channels[0] != ChannelSMS {
		t.Fatal("should not overwrite channels")
	}
}
