package audience

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/starlink/push/internal/domain"
)

// gin 开启 UseNumber 后 HTTP 入参里的数字是 json.Number，
// 而任务落库后重新解析是 float64；人群试算与真实拆分必须得到同一个人数。
func TestExtraIntAcceptsAllNumericSources(t *testing.T) {
	cases := map[string]any{
		"json.Number": json.Number("42"),
		"float64":     float64(42),
		"int":         42,
		"int64":       int64(42),
		"string":      "42",
	}
	for name, value := range cases {
		got, ok := extraInt(map[string]any{"total": value}, "total")
		if !ok || got != 42 {
			t.Fatalf("%s: got (%d, %v), want (42, true)", name, got, ok)
		}
	}
}

func TestExtraIntMissingOrInvalid(t *testing.T) {
	if _, ok := extraInt(nil, "total"); ok {
		t.Fatal("nil extra must not resolve")
	}
	if _, ok := extraInt(map[string]any{}, "total"); ok {
		t.Fatal("missing key must not resolve")
	}
	if _, ok := extraInt(map[string]any{"total": "abc"}, "total"); ok {
		t.Fatal("non-numeric string must not resolve")
	}
}

func TestDemoProviderHonoursJSONNumberTotal(t *testing.T) {
	p := NewDemoProvider(nil)
	page, err := p.Resolve(context.Background(), domain.AudienceQuery{
		AudienceRef: "demo",
		Extra:       map[string]any{"total": json.Number("7")},
		PageSize:    100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Users) != 7 {
		t.Fatalf("expected 7 users from json.Number total, got %d", len(page.Users))
	}
}
