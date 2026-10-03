package authentication

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestVerifyLogoutTokenAcceptsAWellFormedToken(t *testing.T) {
	s := newKeyedOIDCTestServer(t)
	verifier := startVerifier(t, s.config(time.Now()))
	events := map[string]any{backchannelLogoutEvent: map[string]any{}}

	for _, test := range []struct {
		name   string
		claims map[string]any
		want   LogoutToken
	}{
		{
			name:   "sub and sid",
			claims: map[string]any{"sub": "user-1", "sid": "idp-sid-1", "events": events},
			want:   LogoutToken{Subject: "user-1", SessionID: "idp-sid-1"},
		},
		{
			// A provider is not required to send sid at all — optionalStringClaim's
			// absent-vs-wrong-type distinction exists precisely so this still
			// verifies, with SessionID left empty rather than the whole token
			// rejected.
			name:   "sub without sid",
			claims: map[string]any{"sub": "user-1", "events": events},
			want:   LogoutToken{Subject: "user-1"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := verifier.VerifyLogoutToken(context.Background(), s.signLogout(t, "key-a", test.claims))
			if err != nil || got != test.want {
				t.Fatalf("VerifyLogoutToken() = %+v, %v; want %+v", got, err, test.want)
			}
		})
	}
}

func TestVerifyLogoutTokenRejections(t *testing.T) {
	s := newKeyedOIDCTestServer(t)
	verifier := startVerifier(t, s.config(time.Now()))
	validEvents := map[string]any{backchannelLogoutEvent: map[string]any{}}

	tests := map[string]map[string]any{
		"no events claim": {
			"sub": "user-1", "sid": "idp-sid-1",
		},
		"wrong event": {
			"sub": "user-1", "sid": "idp-sid-1",
			"events": map[string]any{"http://example.test/other": map[string]any{}},
		},
		"neither sub nor sid": {
			"events": validEvents,
		},
		"carries a nonce": {
			// OIDC Back-Channel Logout 1.0 §2.4 forbids nonce. Its presence
			// means an ID token is being replayed as a logout token, which
			// would let anyone holding one revoke sessions.
			"sub": "user-1", "sid": "idp-sid-1", "events": validEvents, "nonce": "n-1",
		},
		"stale iat": {
			"sub": "user-1", "sid": "idp-sid-1", "events": validEvents,
			"iat": time.Now().Add(-10 * time.Minute).Unix(),
		},
		"future iat": {
			// Not itself a forgery vector — verifiedPayload has already checked
			// the signature by this point — but the freshness bound should be
			// symmetric rather than only ever looking backward.
			"sub": "user-1", "sid": "idp-sid-1", "events": validEvents,
			"iat": time.Now().Add(10 * time.Minute).Unix(),
		},
	}

	for name, claims := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := verifier.VerifyLogoutToken(context.Background(), s.signLogout(t, "key-a", claims))
			// Every post-signature rejection must collapse to the one opaque
			// sentinel: this endpoint is reachable by an attacker, and leaking
			// the underlying jwt.Parse error would hand them a probe.
			if !errors.Is(err, ErrInvalidLogoutToken) {
				t.Fatalf("VerifyLogoutToken() error = %v, want ErrInvalidLogoutToken", err)
			}
		})
	}
}

func TestVerifyLogoutTokenRejectsAForeignSignature(t *testing.T) {
	s := newKeyedOIDCTestServer(t)
	verifier := startVerifier(t, s.config(time.Now()))
	// A key the provider holds but never publishes, so its JWKS cannot
	// validate anything signed with it.
	s.addRSAKey(t, "foreign-key")

	raw := s.signLogout(t, "foreign-key", map[string]any{
		"sub": "user-1", "sid": "idp-sid-1",
		"events": map[string]any{backchannelLogoutEvent: map[string]any{}},
	})
	if _, err := verifier.VerifyLogoutToken(context.Background(), raw); err == nil {
		t.Fatal("a token signed by an unknown key was accepted")
	}
}
