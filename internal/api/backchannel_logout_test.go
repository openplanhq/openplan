package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vishu42/openplan/internal/authentication"
)

// postLogoutToken posts body to the back-channel logout endpoint of a server
// backed by sessions, whose logout tokens verifier vouches for. A nil verifier
// is a deployment with none configured.
func postLogoutToken(t *testing.T, sessions *fakeSessionStore, verifier LogoutTokenVerifier, body string) *httptest.ResponseRecorder {
	t.Helper()
	server := newAuthTestServer(t, &stubFlow{}, stubVerifier{}, withSessions(sessions), withLogoutTokenVerifier(verifier))

	request := httptest.NewRequest(http.MethodPost, "/v1/auth/backchannel-logout", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	return recorder
}

// logoutFor is a verifier that vouches for a logout token naming subject and
// sessionID.
func logoutFor(subject, sessionID string) fakeLogoutVerifier {
	return fakeLogoutVerifier{token: authentication.LogoutToken{Subject: subject, SessionID: sessionID}}
}

// seedSession puts one live session in the store, so a revoke has something to
// match. Without a row every revoke reports zero, and a test cannot tell the
// narrow key working from the narrow key finding nothing.
func seedSession(sessions *fakeSessionStore, idHash, subject, idpSessionID string) {
	sessions.byHash[idHash] = authentication.Session{
		IDHash:       idHash,
		Subject:      subject,
		IDPSessionID: idpSessionID,
	}
}

func TestBackchannelLogoutRevokesBySessionID(t *testing.T) {
	sessions := newFakeSessionStore()
	seedSession(sessions, "hash-1", "user-1", "idp-sid-1")

	if recorder := postLogoutToken(t, sessions, logoutFor("user-1", "idp-sid-1"), "logout_token=anything"); recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if sessions.revokedBySID["idp-sid-1"] != 1 {
		t.Fatal("no session was revoked for the sid")
	}
	if len(sessions.revokedBySubject) != 0 {
		t.Fatal("fell back to subject even though the sid revoked a session — one device's logout must not sign the user out everywhere")
	}
}

// TestBackchannelLogoutDoesNotSweepOtherDevicesWhenTheSidIsAlreadyRevoked is
// the ordinary case that makes a bare subject-wide fallback wrong. Signing out
// on the laptop revokes that row locally and then sends the browser to the
// IdP, whose back-channel notification arrives naming a sid that now matches
// no *live* row. Falling back to the subject there would sign the phone out
// too, every single time anyone logged out of anything.
func TestBackchannelLogoutDoesNotSweepOtherDevicesWhenTheSidIsAlreadyRevoked(t *testing.T) {
	sessions := newFakeSessionStore()
	seedSession(sessions, "laptop", "user-1", "idp-sid-laptop")
	seedSession(sessions, "phone", "user-1", "idp-sid-phone")
	if err := sessions.RevokeSession(t.Context(), "laptop", time.Unix(1, 0).UTC()); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}

	if recorder := postLogoutToken(t, sessions, logoutFor("user-1", "idp-sid-laptop"), "logout_token=anything"); recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if session := sessions.byHash["phone"]; !session.RevokedAt.IsZero() {
		t.Fatal("the phone session was revoked by the laptop's logout — a sid that matches no live row is not a licence to sign the user out everywhere")
	}
}

// TestBackchannelLogoutFallsBackWhenTheSessionIDMatchesNothing covers the
// provider that puts sid in the logout token but not in the ID token the
// session was created from: every row's idp_session_id is empty, so the narrow
// key matches nothing and, without the fallback, a disabled user keeps their
// openplan session until the absolute bound — the delay this endpoint exists to
// remove.
func TestBackchannelLogoutFallsBackWhenTheSessionIDMatchesNothing(t *testing.T) {
	sessions := newFakeSessionStore()
	seedSession(sessions, "unkeyed", "user-1", "")
	// Same user, but this one carries a sid of its own, so it is reachable by
	// the narrow key and must not be swept up by the fallback.
	seedSession(sessions, "keyed", "user-1", "idp-sid-other")

	if recorder := postLogoutToken(t, sessions, logoutFor("user-1", "idp-sid-1"), "logout_token=anything"); recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if session := sessions.byHash["unkeyed"]; session.RevokedAt.IsZero() {
		t.Fatal("the session with no stored sid survived — nothing else can ever reach it")
	}
	if session := sessions.byHash["keyed"]; !session.RevokedAt.IsZero() {
		t.Fatal("a session addressable by its own sid was revoked by the fallback")
	}
}

func TestBackchannelLogoutFallsBackToSubject(t *testing.T) {
	sessions := newFakeSessionStore()

	if recorder := postLogoutToken(t, sessions, logoutFor("user-1", ""), "logout_token=anything"); recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if sessions.revokedBySubject["user-1"] != 1 {
		t.Fatal("no session was revoked for the subject")
	}
}

func TestBackchannelLogoutRejectsABadToken(t *testing.T) {
	for _, test := range []struct {
		name     string
		verifier fakeLogoutVerifier
		body     string
	}{
		{name: "forged token", verifier: fakeLogoutVerifier{err: authentication.ErrInvalidLogoutToken}, body: "logout_token=forged"},
		// This verifier would vouch for anything, so only the handler's own
		// check stands between a request with no token and a revoke.
		{name: "no token", verifier: logoutFor("user-1", ""), body: "other=field"},
	} {
		t.Run(test.name, func(t *testing.T) {
			sessions := newFakeSessionStore()
			if recorder := postLogoutToken(t, sessions, test.verifier, test.body); recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", recorder.Code)
			}
			if len(sessions.revokedBySID)+len(sessions.revokedBySubject) != 0 {
				t.Fatal("a rejected request still revoked sessions")
			}
		})
	}
}

func TestBackchannelLogoutIsUnauthenticatedAndUncached(t *testing.T) {
	// No cookie, no Authorization header: the IdP has neither.
	recorder := postLogoutToken(t, newFakeSessionStore(), logoutFor("", "idp-sid-1"), "logout_token=anything")

	if recorder.Code == http.StatusUnauthorized {
		t.Fatal("the endpoint requires authentication; the IdP cannot provide any")
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", recorder.Header().Get("Cache-Control"))
	}
}

func TestBackchannelLogoutWithoutAVerifierReturns503InsteadOfPanicking(t *testing.T) {
	// No verifier: a misconfigured deployment, not a bad request from the IdP.
	if recorder := postLogoutToken(t, newFakeSessionStore(), nil, "logout_token=anything"); recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
}
