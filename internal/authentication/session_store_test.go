package authentication

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func newTestOpaqueToken(t *testing.T) string {
	t.Helper()
	token, err := NewOpaqueToken()
	if err != nil {
		t.Fatalf("NewOpaqueToken: %v", err)
	}
	return token
}

func TestNewOpaqueTokenIsUnpredictableAndURLSafe(t *testing.T) {
	first, second := newTestOpaqueToken(t), newTestOpaqueToken(t)
	if first == second {
		t.Fatal("two session IDs are identical, so they are not random")
	}
	// 32 bytes base64url-encoded without padding.
	if len(first) != 43 {
		t.Fatalf("session ID length = %d, want 43", len(first))
	}
	if strings.ContainsAny(first, "+/=") {
		t.Fatalf("session ID %q is not URL-safe", first)
	}
}

func TestHashSessionIDIsStableAndNotTheInput(t *testing.T) {
	raw, other := newTestOpaqueToken(t), newTestOpaqueToken(t)
	hash := HashSessionID(raw)
	if hash == raw {
		t.Fatal("hash equals the raw ID, so the database would hold a usable cookie")
	}
	if hash != HashSessionID(raw) {
		t.Fatal("hash is not stable, so a session could never be looked up twice")
	}
	// Different inputs must hash to different outputs, else collisions break lookup.
	if hash == HashSessionID(other) {
		t.Fatal("different session IDs produced the same hash, collisions would break lookup")
	}
	// Output must be 64 lowercase hex chars (SHA-256).
	if _, err := hex.DecodeString(hash); err != nil || len(hash) != 64 || strings.ToLower(hash) != hash {
		t.Fatalf("hash %q is not 64 lowercase hex characters (SHA-256)", hash)
	}
}

func TestExpiresAtIsTheEarlierBound(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	idleTTL := time.Hour

	for name, absoluteTTL := range map[string]time.Duration{
		"idle bound is earlier":     8 * time.Hour,
		"absolute bound is earlier": 10 * time.Minute,
	} {
		t.Run(name, func(t *testing.T) {
			session := Session{LastSeenAt: now, AbsoluteExpiresAt: now.Add(absoluteTTL)}
			if got, want := session.ExpiresAt(idleTTL), now.Add(min(idleTTL, absoluteTTL)); !got.Equal(want) {
				t.Fatalf("ExpiresAt = %v, want %v", got, want)
			}
		})
	}
}

func TestIsLive(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	idleTTL := time.Hour
	live := Session{LastSeenAt: now, AbsoluteExpiresAt: now.Add(8 * time.Hour)}

	tests := []struct {
		name    string
		session Session
		at      time.Time
		want    bool
	}{
		{"fresh", live, now, true},
		{"just inside the idle bound", live, now.Add(59 * time.Minute), true},
		{"past the idle bound", live, now.Add(61 * time.Minute), false},
		{
			name:    "at the exact expiry boundary",
			session: live,
			at:      live.ExpiresAt(idleTTL),
			want:    false,
		},
		{
			name:    "past the absolute bound despite recent activity",
			session: Session{LastSeenAt: now.Add(8 * time.Hour), AbsoluteExpiresAt: now.Add(8 * time.Hour)},
			at:      now.Add(8*time.Hour + time.Second),
			want:    false,
		},
		{
			name:    "revoked",
			session: Session{LastSeenAt: now, AbsoluteExpiresAt: now.Add(8 * time.Hour), RevokedAt: now},
			at:      now,
			want:    false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.session.IsLive(test.at, idleTTL); got != test.want {
				t.Fatalf("IsLive = %v, want %v", got, test.want)
			}
		})
	}
}
