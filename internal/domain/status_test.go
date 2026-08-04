package domain

import "testing"

func TestPushStatusCanTransitTo(t *testing.T) {
	cases := []struct {
		from, to PushStatus
		ok       bool
	}{
		{PushStatusQueued, PushStatusSending, true},
		{PushStatusSending, PushStatusSent, true},
		{PushStatusSent, PushStatusDelivered, true},
		{PushStatusDelivered, PushStatusClicked, true},
		{PushStatusClicked, PushStatusDelivered, false},
		{PushStatusClicked, PushStatusFailed, false},
		{PushStatusSent, PushStatusQueued, false},
		{PushStatusFailed, PushStatusSending, true},
		{PushStatusSending, PushStatusQueued, true},
		{PushStatusDelivered, PushStatusDelivered, true},
	}
	for _, c := range cases {
		if got := c.from.CanTransitTo(c.to); got != c.ok {
			t.Fatalf("%s -> %s: got %v want %v", c.from, c.to, got, c.ok)
		}
	}
}
