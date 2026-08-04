package push

import "testing"

func TestRenderTemplate(t *testing.T) {
	got := RenderTemplate("hi {{ name }} / {{missing}}", map[string]string{"name": "Ada"})
	if got != "hi Ada / " {
		t.Fatalf("got %q", got)
	}
	if RenderTemplate("plain", nil) != "plain" {
		t.Fatal("nil vars")
	}
}
