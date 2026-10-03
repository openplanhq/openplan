package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/vishu42/openplan/internal/app"
	"github.com/vishu42/openplan/internal/authentication"
	"github.com/vishu42/openplan/internal/encryption"
)

type stubFlow struct {
	authorizationURL string
	authorizationErr error
	idToken          string
	exchangeErr      error
	endSessionURL    string

	gotState        string
	gotNonce        string
	gotCodeVerifier string
	gotCode         string
	gotIDTokenHint  string
}

func (f *stubFlow) AuthorizationURL(state, nonce, codeVerifier string) (string, error) {
	f.gotState, f.gotNonce, f.gotCodeVerifier = state, nonce, codeVerifier
	return f.authorizationURL, f.authorizationErr
}

func (f *stubFlow) Exchange(_ context.Context, code, codeVerifier string) (string, error) {
	f.gotCode, f.gotCodeVerifier = code, codeVerifier
	return f.idToken, f.exchangeErr
}

func (f *stubFlow) EndSessionURL(idTokenHint, _ string) string {
	f.gotIDTokenHint = idTokenHint
	return f.endSessionURL
}

type stubVerifier struct {
	token authentication.VerifiedToken
	err   error
}

func (v stubVerifier) Verify(context.Context, string) (authentication.VerifiedToken, error) {
	return v.token, v.err
}

// authTestOption adjusts the AuthConfig a test server is built with.
type authTestOption func(*AuthConfig)

func withSessions(store authentication.SessionStore) authTestOption {
	return func(cfg *AuthConfig) { cfg.Sessions = store }
}

func withLogoutTokenVerifier(verifier LogoutTokenVerifier) authTestOption {
	return func(cfg *AuthConfig) { cfg.LogoutTokenVerifier = verifier }
}

// fakeLogoutVerifier returns a fixed LogoutToken or error, so handler tests
// need no live IdP or real logout-token signing.
type fakeLogoutVerifier struct {
	token authentication.LogoutToken
	err   error
}

func (v fakeLogoutVerifier) VerifyLogoutToken(context.Context, string) (authentication.LogoutToken, error) {
	return v.token, v.err
}

// fakeSessionStore is an in-memory authentication.SessionStore.
type fakeSessionStore struct {
	created                    []authentication.Session
	byHash                     map[string]authentication.Session
	touched                    int
	revoked                    map[string]int
	revokedBySID               map[string]int
	revokedBySubject           map[string]int
	revokedBySubjectWithoutSID map[string]int
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{
		byHash:                     map[string]authentication.Session{},
		revoked:                    map[string]int{},
		revokedBySID:               map[string]int{},
		revokedBySubject:           map[string]int{},
		revokedBySubjectWithoutSID: map[string]int{},
	}
}

func (f *fakeSessionStore) CreateSession(_ context.Context, session authentication.Session) error {
	f.created = append(f.created, session)
	f.byHash[session.IDHash] = session
	return nil
}

func (f *fakeSessionStore) SessionByHash(_ context.Context, idHash string) (authentication.Session, error) {
	session, ok := f.byHash[idHash]
	if !ok {
		return authentication.Session{}, authentication.ErrSessionNotFound
	}
	return session, nil
}

func (f *fakeSessionStore) TouchSession(_ context.Context, idHash string, seenAt time.Time) error {
	f.touched++
	if session, ok := f.byHash[idHash]; ok {
		session.LastSeenAt = seenAt
		f.byHash[idHash] = session
	}
	return nil
}

func (f *fakeSessionStore) RevokeSession(_ context.Context, idHash string, at time.Time) error {
	f.revoked[idHash]++
	if session, ok := f.byHash[idHash]; ok {
		session.RevokedAt = at
		f.byHash[idHash] = session
	}
	return nil
}

func (f *fakeSessionStore) RevokeSessionsByIDPSessionID(_ context.Context, idpSessionID string, at time.Time) (int, error) {
	f.revokedBySID[idpSessionID]++
	return f.revokeLive(at, func(session authentication.Session) bool { return session.IDPSessionID == idpSessionID }), nil
}

func (f *fakeSessionStore) RevokeSessionsBySubject(_ context.Context, subject string, at time.Time) (int, error) {
	f.revokedBySubject[subject]++
	return f.revokeLive(at, func(session authentication.Session) bool { return session.Subject == subject }), nil
}

func (f *fakeSessionStore) RevokeSessionsBySubjectWithoutIDPSession(_ context.Context, subject string, at time.Time) (int, error) {
	f.revokedBySubjectWithoutSID[subject]++
	return f.revokeLive(at, func(session authentication.Session) bool {
		return session.Subject == subject && session.IDPSessionID == ""
	}), nil
}

// revokeLive revokes every live session matching match and reports how many.
func (f *fakeSessionStore) revokeLive(at time.Time, match func(authentication.Session) bool) int {
	count := 0
	for hash, session := range f.byHash {
		if match(session) && session.RevokedAt.IsZero() {
			session.RevokedAt = at
			f.byHash[hash] = session
			count++
		}
	}
	return count
}

func (f *fakeSessionStore) DeleteSessionsExpiredBefore(_ context.Context, cutoff time.Time) (int, error) {
	count := 0
	for hash, session := range f.byHash {
		if session.AbsoluteExpiresAt.Before(cutoff) {
			delete(f.byHash, hash)
			count++
		}
	}
	return count, nil
}

// testSealer is the cipher every test server seals its login transactions
// with, so a test can seal or open one the server will accept.
func testSealer(t *testing.T) *encryption.Cipher {
	t.Helper()
	sealer, err := encryption.NewCipher("01234567890123456789012345678901")
	if err != nil {
		t.Fatalf("NewCipher returned error: %v", err)
	}
	return sealer
}

// testAuthConfig is a complete AuthConfig around flow and verifier.
//
// NewServer panics on a WithAuth wired without a session store, so it carries
// one even when the test at hand never exercises it; withSessions overrides
// this with a caller-owned store when a test needs to assert against it.
func testAuthConfig(t *testing.T, flow AuthFlow, verifier authentication.Verifier) AuthConfig {
	t.Helper()
	return AuthConfig{
		Flow:               flow,
		Verifier:           verifier,
		Sealer:             testSealer(t),
		PublicURL:          "http://localhost:5173",
		SecureCookies:      false,
		Sessions:           newFakeSessionStore(),
		SessionAbsoluteTTL: authentication.DefaultSessionAbsoluteTTL,
		SessionIdleTTL:     authentication.DefaultSessionIdleTTL,
	}
}

func newAuthTestServer(t *testing.T, flow *stubFlow, verifier authentication.Verifier, options ...authTestOption) *Server {
	t.Helper()
	// The callback projects the signed-in user through the service, so a real
	// one is wired here even for the tests that only assert on cookies.
	return newAuthTestServerWithUsers(t, flow, verifier, &apiFakeUserRepository{}, options...)
}

// newAuthTestServerWithUsers is newAuthTestServer for the tests that assert on
// the identity projection itself, and so need a handle on the repository the
// callback writes through.
func newAuthTestServerWithUsers(
	t *testing.T,
	flow *stubFlow,
	verifier authentication.Verifier,
	users app.UserRepository,
	options ...authTestOption,
) *Server {
	t.Helper()
	cfg := testAuthConfig(t, flow, verifier)
	for _, option := range options {
		option(&cfg)
	}
	return NewServer(app.NewService(app.Service{Users: users}), "tenant_123", WithAuth(cfg))
}

// newPublicPathsTestServer is built through NewAuthenticatedServer, unlike
// newAuthTestServer's plain NewServer: it runs RequireAuthentication, so it is
// the one that catches a public-paths regression.
func newPublicPathsTestServer(t *testing.T, flow *stubFlow) *Server {
	t.Helper()
	service := app.NewService(app.Service{Users: &apiFakeUserRepository{}})
	return NewAuthenticatedServer(service, "tenant_123", false, WithAuth(testAuthConfig(t, flow, stubVerifier{})))
}

// runCallback seals a transaction under a known state and the given nonce,
// issues the callback request carrying it, and returns the recorder. Tests
// that need a different transaction still build one with callbackRequest.
func runCallback(t *testing.T, server *Server, nonce string) *httptest.ResponseRecorder {
	t.Helper()
	transaction := authentication.Transaction{State: "state-1", Nonce: nonce, CodeVerifier: "verifier-1", ReturnTo: "/stacks/abc"}
	request := callbackRequest(t, testSealer(t), transaction, url.Values{"code": {"code-1"}, "state": {"state-1"}})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response
}

func cookieByName(response *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, cookie := range (&http.Response{Header: response.Header()}).Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func TestAuthLoginRedirectsAndSetsTransactionCookie(t *testing.T) {
	flow := &stubFlow{authorizationURL: "https://idp.test/authorize?state=x"}
	server := newAuthTestServer(t, flow, stubVerifier{})

	request := httptest.NewRequest(http.MethodGet, "/v1/auth/login?return_to=/stacks/abc", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", response.Code)
	}
	if location := response.Header().Get("Location"); location != flow.authorizationURL {
		t.Fatalf("Location = %q", location)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
	}

	cookie := cookieByName(response, authentication.TransactionCookieName)
	if cookie == nil {
		t.Fatal("transaction cookie is missing")
	}
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("transaction cookie = %#v", cookie)
	}
	if strings.Contains(cookie.Value, flow.gotState) || strings.Contains(cookie.Value, "/stacks/abc") {
		t.Fatal("transaction cookie is not sealed")
	}
	if flow.gotState == "" || flow.gotNonce == "" || flow.gotCodeVerifier == "" {
		t.Fatalf("flow parameters = %q/%q/%q", flow.gotState, flow.gotNonce, flow.gotCodeVerifier)
	}
	if flow.gotState == flow.gotNonce {
		t.Fatal("state and nonce are the same value")
	}
}

func TestAuthLoginRejectsOffOriginReturnTo(t *testing.T) {
	flow := &stubFlow{authorizationURL: "https://idp.test/authorize"}
	server := newAuthTestServer(t, flow, stubVerifier{})

	request := httptest.NewRequest(http.MethodGet, "/v1/auth/login?return_to=https://evil.test/steal", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	cookie := cookieByName(response, authentication.TransactionCookieName)
	if cookie == nil {
		t.Fatal("transaction cookie is missing")
	}
	transaction, err := authentication.OpenTransaction(testSealer(t), cookie.Value)
	if err != nil {
		t.Fatalf("OpenTransaction returned error: %v", err)
	}
	if transaction.ReturnTo != "/" {
		t.Fatalf("ReturnTo = %q, want /", transaction.ReturnTo)
	}
}

func callbackRequest(t *testing.T, sealer *encryption.Cipher, transaction authentication.Transaction, query url.Values) *http.Request {
	t.Helper()
	sealed, err := authentication.SealTransaction(sealer, transaction)
	if err != nil {
		t.Fatalf("SealTransaction returned error: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/auth/callback?"+query.Encode(), nil)
	request.AddCookie(&http.Cookie{Name: authentication.TransactionCookieName, Value: sealed})
	return request
}

func TestAuthCallbackCreatesSessionSetsOpaqueCookieAndRedirects(t *testing.T) {
	sessions := newFakeSessionStore()
	flow := &stubFlow{idToken: "raw.id.token"}
	verifier := stubVerifier{token: authentication.VerifiedToken{
		Subject: "user-1", Name: "Ada Lovelace", Email: "ada@example.test",
		SessionID: "idp-sid-1", Nonce: "nonce-1",
		// A 60-second ID token must still produce a full-length session: how
		// long a session lasts is openplan's to decide, not the token's.
		ExpiresAt: time.Now().Add(time.Minute),
	}}
	server := newAuthTestServer(t, flow, verifier, withSessions(sessions))

	response := runCallback(t, server, "nonce-1")

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body = %s", response.Code, response.Body.String())
	}
	if location := response.Header().Get("Location"); location != "/stacks/abc" {
		t.Fatalf("Location = %q", location)
	}
	// The cookie is now an opaque reference to the session row, not the ID
	// token itself.
	session := cookieByName(response, authentication.SessionCookieName)
	if session == nil || session.Value == "" || session.Value == "raw.id.token" || !session.HttpOnly {
		t.Fatalf("session cookie = %#v", session)
	}
	cleared := cookieByName(response, authentication.TransactionCookieName)
	if cleared == nil || cleared.MaxAge != -1 {
		t.Fatalf("transaction cookie = %#v, want cleared", cleared)
	}
	if flow.gotCode != "code-1" || flow.gotCodeVerifier != "verifier-1" {
		t.Fatalf("exchange args = %q/%q", flow.gotCode, flow.gotCodeVerifier)
	}
	if body := response.Body.String(); strings.Contains(body, "raw.id.token") {
		t.Fatal("callback response body carries the token")
	}
	if strings.Count(session.Value, ".") == 2 {
		t.Fatalf("cookie value %q still looks like a JWT; it must be an opaque reference", session.Value)
	}
	if len(sessions.created) != 1 {
		t.Fatalf("created %d sessions, want 1", len(sessions.created))
	}

	created := sessions.created[0]
	if created.IDHash != authentication.HashSessionID(session.Value) {
		t.Fatal("the stored hash does not match the cookie the browser was given")
	}
	if created.IDToken != "raw.id.token" {
		t.Fatal("the ID token was not kept; logout needs it for id_token_hint")
	}
	if created.IDPSessionID != "idp-sid-1" {
		t.Fatalf("IDPSessionID = %q, want idp-sid-1", created.IDPSessionID)
	}
	if created.AbsoluteExpiresAt.Sub(created.CreatedAt) != authentication.DefaultSessionAbsoluteTTL {
		t.Fatalf("absolute window = %v, want 8h regardless of the token's exp", created.AbsoluteExpiresAt.Sub(created.CreatedAt))
	}
}

func TestAuthCallbackFailuresAreIndistinguishable(t *testing.T) {
	sealer := testSealer(t)
	good := authentication.Transaction{State: "state-1", Nonce: "nonce-1", CodeVerifier: "verifier-1", ReturnTo: "/stacks"}

	var bodies []string
	var headers []string
	for _, test := range []struct {
		name     string
		flow     *stubFlow
		verifier stubVerifier
		build    func(*testing.T) *http.Request
	}{
		{
			name: "idp error",
			flow: &stubFlow{}, verifier: stubVerifier{},
			build: func(t *testing.T) *http.Request {
				return callbackRequest(t, sealer, good, url.Values{"error": {"access_denied"}, "state": {"state-1"}})
			},
		},
		{
			name: "no transaction cookie",
			flow: &stubFlow{}, verifier: stubVerifier{},
			build: func(t *testing.T) *http.Request {
				return httptest.NewRequest(http.MethodGet, "/v1/auth/callback?code=code-1&state=state-1", nil)
			},
		},
		{
			name: "state mismatch",
			flow: &stubFlow{idToken: "raw.id.token"}, verifier: stubVerifier{},
			build: func(t *testing.T) *http.Request {
				return callbackRequest(t, sealer, good, url.Values{"code": {"code-1"}, "state": {"other-state"}})
			},
		},
		{
			name: "exchange fails",
			flow: &stubFlow{exchangeErr: context.Canceled}, verifier: stubVerifier{},
			build: func(t *testing.T) *http.Request {
				return callbackRequest(t, sealer, good, url.Values{"code": {"code-1"}, "state": {"state-1"}})
			},
		},
		{
			name: "verification fails",
			flow: &stubFlow{idToken: "raw.id.token"}, verifier: stubVerifier{err: authentication.ErrInvalidToken},
			build: func(t *testing.T) *http.Request {
				return callbackRequest(t, sealer, good, url.Values{"code": {"code-1"}, "state": {"state-1"}})
			},
		},
		{
			name:     "nonce mismatch",
			flow:     &stubFlow{idToken: "raw.id.token"},
			verifier: stubVerifier{token: authentication.VerifiedToken{Subject: "user-123", Nonce: "other-nonce"}},
			build: func(t *testing.T) *http.Request {
				return callbackRequest(t, sealer, good, url.Values{"code": {"code-1"}, "state": {"state-1"}})
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := newAuthTestServer(t, test.flow, test.verifier)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, test.build(t))

			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", response.Code)
			}
			if cookieByName(response, authentication.SessionCookieName) != nil {
				t.Fatal("a failed callback set a session cookie")
			}
			// A failure leaving the transaction cookie live allows replay
			// within its 600-second Max-Age.
			clearedTx := cookieByName(response, authentication.TransactionCookieName)
			if clearedTx == nil || clearedTx.MaxAge != -1 {
				t.Fatalf("transaction cookie = %#v, want cleared on failure", clearedTx)
			}
			bodies = append(bodies, response.Body.String())
			headers = append(headers, normalizedHeaderDump(response.Header()))
		})
	}

	// Distinguishable failures are an oracle: they tell an attacker which check
	// they tripped.
	for i := 1; i < len(bodies); i++ {
		if bodies[i] != bodies[0] {
			t.Fatalf("failure bodies differ:\n%q\n%q", bodies[0], bodies[i])
		}
	}
	// Bodies are not the only oracle: a future divergent Set-Cookie,
	// WWW-Authenticate, or X-Reason would leak the same information through a
	// header instead, and only comparing bodies would stay green.
	for i := 1; i < len(headers); i++ {
		if headers[i] != headers[0] {
			t.Fatalf("failure headers differ:\n%s\n---\n%s", headers[0], headers[i])
		}
	}
}

// normalizedHeaderDump renders response headers deterministically: sorted
// keys, and sorted values within a key, so every Set-Cookie is included and
// header order (which carries no meaning) cannot cause a false mismatch.
func normalizedHeaderDump(header http.Header) string {
	keys := make([]string, 0, len(header))
	for key := range header {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var dump strings.Builder
	for _, key := range keys {
		values := append([]string(nil), header[key]...)
		sort.Strings(values)
		for _, value := range values {
			dump.WriteString(key)
			dump.WriteString(": ")
			dump.WriteString(value)
			dump.WriteString("\n")
		}
	}
	return dump.String()
}

// logOut signs out a browser holding the cookie for a stored session whose ID
// token is idToken, and returns the response alongside the store.
func logOut(t *testing.T, flow *stubFlow, idToken string) (*httptest.ResponseRecorder, *fakeSessionStore) {
	t.Helper()
	const raw = "session-token"
	sessions := newFakeSessionStore()
	sessions.byHash[authentication.HashSessionID(raw)] = authentication.Session{
		IDHash:  authentication.HashSessionID(raw),
		Subject: "user-1",
		IDToken: idToken,
	}
	server := newAuthTestServer(t, flow, stubVerifier{}, withSessions(sessions))

	request := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	request.AddCookie(&http.Cookie{Name: authentication.SessionCookieName, Value: raw})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response, sessions
}

func TestAuthLogoutRevokesClearsCookieAndRedirectsToIdP(t *testing.T) {
	flow := &stubFlow{endSessionURL: "https://idp.test/logout?id_token_hint=stored.id.token"}
	response, sessions := logOut(t, flow, "stored.id.token")

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", response.Code)
	}
	if location := response.Header().Get("Location"); location != flow.endSessionURL {
		t.Fatalf("Location = %q", location)
	}
	cleared := cookieByName(response, authentication.SessionCookieName)
	if cleared == nil || cleared.Value != "" || cleared.MaxAge != -1 {
		t.Fatalf("session cookie = %#v, want cleared", cleared)
	}
	// stubFlow records what it was handed rather than building a URL.
	if flow.gotIDTokenHint != "stored.id.token" {
		t.Fatalf("id_token_hint = %q, want the ID token stored on the session", flow.gotIDTokenHint)
	}
	if sessions.revoked[authentication.HashSessionID("session-token")] != 1 {
		t.Fatal("logout did not revoke the session row; a copied cookie would still work")
	}
	// The ID token may appear only in the Location header. A response body
	// carrying it would be readable by any script on the origin, which is
	// exactly what HttpOnly exists to prevent.
	if body := response.Body.String(); strings.Contains(body, "stored.id.token") {
		t.Fatal("logout response body carries the ID token")
	}
}

func TestAuthLogoutWithoutProviderSupportRedirectsHome(t *testing.T) {
	response, _ := logOut(t, &stubFlow{}, "raw.id.token")

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", response.Code)
	}
	if location := response.Header().Get("Location"); location != "http://localhost:5173/" {
		t.Fatalf("Location = %q", location)
	}
}

func TestLogoutWithoutASessionStillRedirects(t *testing.T) {
	sessions := newFakeSessionStore()
	server := newAuthTestServer(t, &stubFlow{}, stubVerifier{}, withSessions(sessions))

	request := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 even with no session", recorder.Code)
	}
}

func TestAuthRoutesArePublic(t *testing.T) {
	// The middleware must let the login routes through: a user with no session
	// cannot obtain one from behind an authentication gate.
	server := newPublicPathsTestServer(t, &stubFlow{authorizationURL: "https://idp.test/authorize"})

	request := httptest.NewRequest(http.MethodGet, "/v1/auth/login", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 — /v1/auth/login is behind the auth gate", response.Code)
	}

	// Without LogoutTokenVerifier configured the handler itself answers 503,
	// but that 503 — not a 401 from the middleware — is exactly what proves
	// the route reached the handler unauthenticated.
	backchannelRequest := httptest.NewRequest(http.MethodPost, "/v1/auth/backchannel-logout", strings.NewReader("logout_token=anything"))
	backchannelRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	backchannelResponse := httptest.NewRecorder()
	server.ServeHTTP(backchannelResponse, backchannelRequest)

	if backchannelResponse.Code == http.StatusUnauthorized {
		t.Fatalf("status = %d, want anything but 401 — /v1/auth/backchannel-logout must stay public even with no bearer token or cookie", backchannelResponse.Code)
	}
}

// The callback is the only place an identity is verified, so it is the only
// place the projection can be written. Without this, a user could sign in and
// still be invisible to the grants UI.
func TestAuthCallbackProjectsSignedInUser(t *testing.T) {
	users := &apiFakeUserRepository{}
	flow := &stubFlow{idToken: "raw.id.token"}
	verifier := stubVerifier{token: authentication.VerifiedToken{
		Subject:           "user-123",
		Name:              "Ada Lovelace",
		PreferredUsername: "ada",
		Email:             "ada@example.com",
		Nonce:             "nonce-1",
	}}
	server := newAuthTestServerWithUsers(t, flow, verifier, users)

	if response := runCallback(t, server, "nonce-1"); response.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body = %s", response.Code, response.Body.String())
	}

	if len(users.users) != 1 {
		t.Fatalf("projected %d users, want 1", len(users.users))
	}
	projected := users.users[0]
	if projected.Sub != "user-123" || projected.DisplayName != "Ada Lovelace" || projected.Email != "ada@example.com" {
		t.Fatalf("projected = %#v", projected)
	}
}

// A provider is not obliged to send `name`. The projection must still label the
// person with something, or the grants list shows a blank row.
func TestAuthCallbackProjectsFallbackDisplayName(t *testing.T) {
	users := &apiFakeUserRepository{}
	flow := &stubFlow{idToken: "raw.id.token"}
	verifier := stubVerifier{token: authentication.VerifiedToken{
		Subject: "user-123",
		Email:   "ada@example.com",
		Nonce:   "nonce-1",
	}}
	server := newAuthTestServerWithUsers(t, flow, verifier, users)

	if response := runCallback(t, server, "nonce-1"); response.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body = %s", response.Code, response.Body.String())
	}
	if len(users.users) != 1 || users.users[0].DisplayName != "ada@example.com" {
		t.Fatalf("projected = %#v, want the email as display name", users.users)
	}
}

// Failing the sign-in is deliberate: continuing would sign someone in who is
// then ungrantable and invisible to search, with nothing saying why.
func TestAuthCallbackFailsWhenProjectionWriteFails(t *testing.T) {
	sessions := newFakeSessionStore()
	flow := &stubFlow{idToken: "raw.id.token"}
	verifier := stubVerifier{token: authentication.VerifiedToken{Subject: "user-123", Nonce: "nonce-1"}}
	users := &apiFakeUserRepository{upsertErr: errors.New("database is down")}
	server := newAuthTestServerWithUsers(t, flow, verifier, users, withSessions(sessions))

	response := runCallback(t, server, "nonce-1")

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", response.Code, response.Body.String())
	}
	// The projection is written first precisely so this holds: no session row
	// can exist for a subject that was never projected.
	if count := len(sessions.created); count != 0 {
		t.Fatalf("created %d sessions, want 0 — the session must not outlive a failed projection", count)
	}
}

// The callback writes the identity projection on every sign-in, so a Service
// without a user repository is as half-wired as an AuthConfig without a
// session store. The wiring check exists to say so once at boot rather than
// nil-panic on the first login.
func TestNewServerPanicsWithoutAUserRepository(t *testing.T) {
	cfg := testAuthConfig(t, &stubFlow{authorizationURL: "https://idp.test/authorize"}, stubVerifier{})

	defer func() {
		if recover() == nil {
			t.Fatal("NewServer did not panic on a Service without a user repository")
		}
	}()
	NewServer(app.NewService(app.Service{}), "tenant_123", WithAuth(cfg))
}

// The identity provider is the only way in. openplan checks no password, so
// there is no local sign-in route to reach, and no methods endpoint for a
// sign-in screen to choose between ways in: there is one.
func TestAuthServesNoLocalSignInOrMethodsRoute(t *testing.T) {
	// The public-paths list is in play: a route still listed there would
	// answer a signed-out caller instead of 401.
	server := newPublicPathsTestServer(t, &stubFlow{authorizationURL: "https://idp.test/authorize"})

	login := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(`{"username":"root","password":"x"}`))
	login.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, login)
	// 405, not 401: GET /v1/auth/login exists, so the path matches, but no
	// handler takes a POST.
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /v1/auth/login status = %d, want 405", response.Code)
	}

	// 401, not 404: with no route and no public-path entry, a signed-out
	// caller meets the authentication gate like on any other unknown path.
	methods := httptest.NewRecorder()
	server.ServeHTTP(methods, httptest.NewRequest(http.MethodGet, "/v1/auth/methods", nil))
	if methods.Code != http.StatusUnauthorized {
		t.Fatalf("GET /v1/auth/methods status = %d, want 401", methods.Code)
	}
}
