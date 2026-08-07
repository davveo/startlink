package callback

import (
	"context"
	"testing"

	"github.com/starlink/push/internal/config"
)

func TestVerifierDisabled(t *testing.T) {
	v := NewVerifier(config.CallbackConfig{}, nil)
	if err := v.Verify(context.Background(), "", "", "", []byte(`{"ok":true}`)); err != nil {
		t.Fatalf("disabled verifier returned error: %v", err)
	}
}

func TestVerifierRejectsMalformedRequest(t *testing.T) {
	v := NewVerifier(config.CallbackConfig{
		SignatureRequired: true,
		SignatureSecret:   "01234567890123456789012345678901",
		MaxSkewSec:        300,
	}, nil)
	if err := v.Verify(context.Background(), "bad", "short", "bad", nil); err == nil {
		t.Fatal("expected malformed signature to be rejected")
	}
}
