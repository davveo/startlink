package webhook

import (
	"testing"

	"github.com/starlink/push/internal/config"
)

func TestValidateURLAllowlist(t *testing.T) {
	c := New(config.WebhookConfig{AllowedHosts: []string{"hooks.example.com"}})
	if err := c.validateURL("https://hooks.example.com/callback"); err != nil {
		t.Fatalf("allowed webhook rejected: %v", err)
	}
	if err := c.validateURL("https://127.0.0.1/callback"); err == nil {
		t.Fatal("unexpected host should be rejected")
	}
	if err := c.validateURL("http://hooks.example.com/callback"); err == nil {
		t.Fatal("plain HTTP should be rejected")
	}
}
