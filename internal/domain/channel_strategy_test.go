package domain

import "testing"

func TestMatchRouteRules(t *testing.T) {
	rules := []ChannelRouteRule{
		{When: &RouteCondition{Var: "vip", Op: "eq", Value: "true"}, Channels: []ChannelType{ChannelSMS, ChannelAppPush}},
		{When: &RouteCondition{Var: "score", Op: "gte", Value: "80"}, Channels: []ChannelType{ChannelAppPush}},
		{Channels: []ChannelType{ChannelInbox}},
	}
	got := MatchRouteRules(rules, map[string]string{"vip": "true"}, nil, []ChannelType{ChannelEmail})
	if len(got) != 2 || got[0] != ChannelSMS {
		t.Fatalf("vip route: %v", got)
	}
	got = MatchRouteRules(rules, map[string]string{"score": "90"}, nil, nil)
	if len(got) != 1 || got[0] != ChannelAppPush {
		t.Fatalf("score route: %v", got)
	}
	got = MatchRouteRules(rules, map[string]string{"score": "10"}, nil, nil)
	if len(got) != 1 || got[0] != ChannelInbox {
		t.Fatalf("default route: %v", got)
	}
}

func TestSortChannelsByCost(t *testing.T) {
	chs := []ChannelType{ChannelSMS, ChannelInbox, ChannelAppPush}
	got := SortChannelsByCost(chs, nil)
	if got[0] != ChannelInbox || got[1] != ChannelAppPush || got[2] != ChannelSMS {
		t.Fatalf("default costs order: %v", got)
	}
	got = SortChannelsByCost(chs, map[ChannelType]int{ChannelSMS: 1, ChannelInbox: 5, ChannelAppPush: 3})
	if got[0] != ChannelSMS || got[1] != ChannelAppPush || got[2] != ChannelInbox {
		t.Fatalf("custom costs order: %v", got)
	}
}
