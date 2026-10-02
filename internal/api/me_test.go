package api

import (
	"testing"
	"time"

	"github.com/vishu42/openplan/internal/authentication"
)

func TestMeFromPrincipalReportsSessionExpiry(t *testing.T) {
	expiry := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	me := meFromPrincipal(
		authentication.Principal{Subject: "user-123", Name: "Ada", ExpiresAt: expiry},
		"tenant_123",
		globalCapabilities{},
	)
	if me.SessionExpiresAt != "2026-08-25T12:00:00Z" {
		t.Fatalf("SessionExpiresAt = %q", me.SessionExpiresAt)
	}
}

func TestMeFromPrincipalOmitsZeroExpiry(t *testing.T) {
	me := meFromPrincipal(authentication.Principal{Subject: "user-123"}, "tenant_123", globalCapabilities{})
	if me.SessionExpiresAt != "" {
		t.Fatalf("SessionExpiresAt = %q, want empty", me.SessionExpiresAt)
	}
}
