package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := writeConfig(t, `
server:
  addr: ":8080"
  typo_timeout: 5
auth:
  enabled: false
mysql:
  dsn: test
redis:
  addr: localhost:6379
`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected unknown config field to be rejected")
	}
}

func TestLoadRejectsShortSessionSecret(t *testing.T) {
	path := writeConfig(t, `
auth:
  enabled: true
  session_secret: short
mysql:
  dsn: test
redis:
  addr: localhost:6379
`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected short session secret to be rejected")
	}
}
