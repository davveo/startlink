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

func TestRenderTemplateWithPolicy(t *testing.T) {
	vars := map[string]string{"name": "Ada"}
	defaults := map[string]string{"score": "100"}

	got, err := RenderTemplateWithPolicy("{{name}}-{{score}}-{{x}}", vars, "default", defaults)
	if err != nil || got != "Ada-100-" {
		t.Fatalf("default: got %q err=%v", got, err)
	}

	got, err = RenderTemplateWithPolicy("{{name}}-{{x}}", vars, "keep", nil)
	if err != nil || got != "Ada-{{x}}" {
		t.Fatalf("keep: got %q err=%v", got, err)
	}

	_, err = RenderTemplateWithPolicy("{{name}}-{{x}}", vars, "error", nil)
	if err == nil {
		t.Fatal("error policy should fail")
	}
}
