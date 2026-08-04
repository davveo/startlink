package domain

import "testing"

func TestResolvePriority(t *testing.T) {
	highScenes := []string{"otp", "payment"}
	if got := ResolvePriority(PriorityHigh, "marketing", highScenes); got != PriorityHigh {
		t.Fatalf("explicit high: %s", got)
	}
	if got := ResolvePriority(PriorityNormal, "otp", highScenes); got != PriorityNormal {
		t.Fatalf("explicit normal: %s", got)
	}
	if got := ResolvePriority("", "otp", highScenes); got != PriorityHigh {
		t.Fatalf("scene map: %s", got)
	}
	if got := ResolvePriority("", "marketing", highScenes); got != PriorityNormal {
		t.Fatalf("default: %s", got)
	}
}
