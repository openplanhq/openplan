package authentication

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakeSessionStore is a SessionStore over an in-memory map. The middleware
// uses only SessionByHash and TouchSession; any other method reaches the nil
// embedded interface and panics. touched counts calls to TouchSession so tests
// can assert the touch interval is honored.
type fakeSessionStore struct {
	SessionStore
	byHash  map[string]Session
	touched int
	// touchErr, when set, fails every TouchSession.
	touchErr error
}

func (store *fakeSessionStore) SessionByHash(_ context.Context, idHash string) (Session, error) {
	session, ok := store.byHash[idHash]
	if !ok {
		return Session{}, ErrSessionNotFound
	}
	return session, nil
}

func (store *fakeSessionStore) TouchSession(_ context.Context, idHash string, seenAt time.Time) error {
	store.touched++
	if store.touchErr != nil {
		return store.touchErr
	}
	if session, ok := store.byHash[idHash]; ok {
		session.LastSeenAt = seenAt
		store.byHash[idHash] = session
	}
	return nil
}

// serve runs request through middleware around a handler that answers 200,
// and reports the response along with the principal that handler saw, nil when
// it never ran.
func serve(middleware func(http.Handler) http.Handler, request *http.Request) (*httptest.ResponseRecorder, *Principal) {
	var seen *Principal
	handler := middleware(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		principal, _ := PrincipalFromContext(request.Context())
		seen = &principal
		response.WriteHeader(http.StatusOK)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder, seen
}

// sessionRequest is a GET of /v1/me carrying raw as the session cookie.
func sessionRequest(raw string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: raw})
	return request
}

func TestRequireAuthentication(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	live := Session{
		IDHash:            HashSessionID("session-token"),
		Subject:           "user-123",
		Name:              "Ada",
		PreferredUsername: "ada",
		Email:             "ada@example.test",
		LastSeenAt:        now,
		AbsoluteExpiresAt: now.Add(8 * time.Hour),
	}
	sessionCookie := &http.Cookie{Name: SessionCookieName, Value: "session-token"}

	for _, test := range []struct {
		name          string
		path          string // a protected API path when empty
		authorization string
		cookie        *http.Cookie
		stored        func(*Session)
		status        int
	}{
		{name: "public health", path: "/healthz", status: http.StatusOK},
		{name: "no credential", status: http.StatusUnauthorized},
		{name: "unknown cookie value", cookie: &http.Cookie{Name: SessionCookieName, Value: "not-a-session"}, status: http.StatusUnauthorized},
		// The shape a real client sends after logout clears the cookie.
		{name: "empty cookie", cookie: &http.Cookie{Name: SessionCookieName, Value: ""}, status: http.StatusUnauthorized},
		{name: "other cookies are ignored", cookie: &http.Cookie{Name: "some_other_cookie", Value: "value"}, status: http.StatusUnauthorized},
		// An Authorization header is not a credential. Accepting one made the
		// ID token — which RP-initiated logout necessarily puts in a URL — a
		// key to every /v1 route, and a signed token cannot be revoked.
		{name: "bearer header is ignored", authorization: "Bearer any-token", status: http.StatusUnauthorized},
		{name: "bearer header does not override a cookie", authorization: "Bearer any-token", cookie: sessionCookie, status: http.StatusOK},
		{name: "malformed header is ignored too", authorization: "Basic ignored", cookie: sessionCookie, status: http.StatusOK},
		{name: "session cookie", cookie: sessionCookie, status: http.StatusOK},
		{name: "idle expired", cookie: sessionCookie, status: http.StatusUnauthorized, stored: func(session *Session) {
			session.LastSeenAt = now.Add(-2 * time.Hour)
			session.AbsoluteExpiresAt = now.Add(6 * time.Hour)
		}},
		{name: "absolute expired", cookie: sessionCookie, status: http.StatusUnauthorized, stored: func(session *Session) {
			session.AbsoluteExpiresAt = now.Add(-time.Minute)
		}},
		{name: "revoked", cookie: sessionCookie, status: http.StatusUnauthorized, stored: func(session *Session) {
			session.RevokedAt = now.Add(-time.Minute)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			stored := live
			if test.stored != nil {
				test.stored(&stored)
			}
			sessions := &fakeSessionStore{byHash: map[string]Session{stored.IDHash: stored}}
			request := httptest.NewRequest(http.MethodGet, cmp.Or(test.path, "/v1/stacks"), nil)
			if test.authorization != "" {
				request.Header.Set("Authorization", test.authorization)
			}
			if test.cookie != nil {
				request.AddCookie(test.cookie)
			}

			response, principal := serve(RequireAuthentication(sessions, time.Hour, func() time.Time { return now }, "/healthz"), request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			switch {
			case test.status == http.StatusUnauthorized:
				if principal != nil || response.Body.String() != `{"code":"unauthorized"}` {
					t.Fatalf("handler ran = %t, body = %q", principal != nil, response.Body.String())
				}
			case test.path == "":
				want := Principal{Subject: live.Subject, Name: live.Name, PreferredUsername: live.PreferredUsername, Email: live.Email, ExpiresAt: now.Add(time.Hour)}
				if principal == nil || *principal != want {
					t.Fatalf("principal = %#v, want %#v", principal, want)
				}
			}
		})
	}
}

// The principal is all value types now, so a reader cannot reach through it to
// mutate what the next reader sees. It used to carry a role slice that could be.
func TestPrincipalFromContextIsNotMutableByAReader(t *testing.T) {
	ctx := ContextWithPrincipal(context.Background(), Principal{Subject: "user-123", Name: "Ada"})
	principal, ok := PrincipalFromContext(ctx)
	if !ok {
		t.Fatal("principal is missing")
	}
	principal.Name = "mutated"
	second, ok := PrincipalFromContext(ctx)
	if !ok || second.Name != "Ada" {
		t.Fatalf("principal after mutation = %#v, ok = %t", second, ok)
	}
}

// A server built without WithAuth (most of internal/api's own tests, plus any
// future caller that forgets it) has a nil SessionStore. A cookie must 401
// against that, not panic a nil interface mid-request.
func TestNilSessionStoreRejectsCookieInsteadOfPanicking(t *testing.T) {
	response, principal := serve(RequireAuthentication(nil, time.Hour, nil), sessionRequest("session-token"))
	if response.Code != http.StatusUnauthorized || principal != nil {
		t.Fatalf("status = %d, handler ran = %t; want 401 without running the handler", response.Code, principal != nil)
	}
}

func TestTouchOnlyAfterTheTouchInterval(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)

	for _, test := range []struct {
		name     string
		lastSeen time.Time
		touches  int
	}{
		// A write per request is what the interval avoids.
		{name: "fresh session is not written back", lastSeen: now.Add(-time.Minute), touches: 0},
		{name: "stale session is written back", lastSeen: now.Add(-SessionTouchInterval - time.Second), touches: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := Session{
				IDHash:            HashSessionID("session-token"),
				Subject:           "user-1",
				LastSeenAt:        test.lastSeen,
				AbsoluteExpiresAt: now.Add(8 * time.Hour),
			}
			sessions := &fakeSessionStore{byHash: map[string]Session{session.IDHash: session}}
			response, _ := serve(RequireAuthentication(sessions, time.Hour, func() time.Time { return now }), sessionRequest("session-token"))
			if response.Code != http.StatusOK || sessions.touched != test.touches {
				t.Fatalf("status = %d, touched %d times; want 200 and %d", response.Code, sessions.touched, test.touches)
			}
		})
	}
}

// The bound reported to the browser is the one this request just wrote, and
// only if the write landed.
func TestExpiresAtReportsTheTouchOnlyWhenItLands(t *testing.T) {
	const idleTTL = time.Hour
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	lastSeen := now.Add(-59 * time.Minute)

	for _, test := range []struct {
		name     string
		touchErr error
		want     time.Time
	}{
		// Reporting the pre-touch bound is not merely stale: it is the moment
		// the client is already asking about, so an idle client's proactive
		// re-authentication can never find a later bound to rearm on and gives
		// up on a session that is not ending.
		{name: "touch written", want: now.Add(idleTTL)},
		// A failed touch shortens the session, and saying otherwise would
		// promise a bound the database does not hold.
		{name: "touch failed", touchErr: errors.New("write failed"), want: lastSeen.Add(idleTTL)},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := Session{
				IDHash:            HashSessionID("session-token"),
				Subject:           "user-1",
				LastSeenAt:        lastSeen,
				AbsoluteExpiresAt: now.Add(8 * time.Hour),
			}
			sessions := &fakeSessionStore{byHash: map[string]Session{session.IDHash: session}, touchErr: test.touchErr}
			_, principal := serve(RequireAuthentication(sessions, idleTTL, func() time.Time { return now }), sessionRequest("session-token"))
			if principal == nil || !principal.ExpiresAt.Equal(test.want) {
				t.Fatalf("principal = %#v, want ExpiresAt %v", principal, test.want)
			}
		})
	}
}
