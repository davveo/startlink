package domain

import "testing"

func TestMergeExtra(t *testing.T) {
	got := MergeExtra(
		map[string]any{"a": 1, "phone": "campaign"},
		map[string]any{"phone": "user", "token": "t1"},
	)
	if got["a"] != 1 || got["phone"] != "user" || got["token"] != "t1" {
		t.Fatalf("unexpected merge: %#v", got)
	}
	if MergeExtra(nil, nil) != nil {
		t.Fatal("nil+nil should be nil")
	}
}
