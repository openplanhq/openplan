package authentication

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"maps"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jws"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

type oidcTestServer struct {
	t *testing.T

	server          *httptest.Server
	issuer          string
	issuerPathToken string

	mu                    sync.Mutex
	keys                  map[string]*rsa.PrivateKey
	published             []string
	discoveryIssuer       string
	jwksPath              string
	unavailableBody       string
	discoveryBody         string
	jwksBody              string
	authorizationEndpoint string
	tokenEndpoint         string
	endSessionEndpoint    string
	omitEndpoints         bool
	discoveryRequests     int
	jwksRequests          int
	clock                 time.Time
}

type jwksDocument struct {
	Keys []map[string]string `json:"keys"`
}

func newOIDCTestServer(t *testing.T) *oidcTestServer {
	t.Helper()

	s := &oidcTestServer{
		t:               t,
		issuerPathToken: "issuer-private-path",
		jwksPath:        "/jwks",
		keys:            make(map[string]*rsa.PrivateKey),
	}
	s.server = httptest.NewServer(http.HandlerFunc(s.serveHTTP))
	s.issuer = s.server.URL + "/" + s.issuerPathToken
	s.discoveryIssuer = s.issuer
	s.authorizationEndpoint = s.server.URL + "/authorize"
	s.tokenEndpoint = s.server.URL + "/token"
	s.endSessionEndpoint = s.server.URL + "/logout"
	t.Cleanup(s.server.Close)

	return s
}

// newKeyedOIDCTestServer starts a test IdP that publishes one RSA key, key-a.
func newKeyedOIDCTestServer(t *testing.T) *oidcTestServer {
	t.Helper()
	s := newOIDCTestServer(t)
	s.addRSAKey(t, "key-a")
	s.publish("key-a")
	return s
}

// startVerifier builds a verifier from cfg and closes it when the test ends.
func startVerifier(t *testing.T, cfg OIDCVerifierConfig) *OIDCVerifier {
	t.Helper()
	v, err := NewOIDCVerifier(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewOIDCVerifier() error = %v", err)
	}
	t.Cleanup(func() {
		if err := v.Close(context.Background()); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return v
}

// requireUnavailable asserts err is ErrVerifierUnavailable with the sentinel's
// exact message, so nothing from the provider or the token can ride along.
func requireUnavailable(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrVerifierUnavailable) || err.Error() != ErrVerifierUnavailable.Error() {
		t.Fatalf("error = %v, want exactly ErrVerifierUnavailable", err)
	}
}

func (s *oidcTestServer) addRSAKey(t *testing.T, keyID string) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("rsa.GenerateKey() error = %v", err)
	}
	s.update(func() { s.keys[keyID] = key })
}

func (s *oidcTestServer) publish(keyIDs ...string) {
	s.t.Helper()

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, keyID := range keyIDs {
		if _, ok := s.keys[keyID]; !ok {
			s.t.Fatalf("unknown key ID %q", keyID)
		}
	}
	s.published = append([]string(nil), keyIDs...)
}

// update changes how the provider behaves, under the lock its handlers read
// that behaviour with.
func (s *oidcTestServer) update(change func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	change()
}

// key returns the private key held under keyID, published or not.
func (s *oidcTestServer) key(t *testing.T, keyID string) *rsa.PrivateKey {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.keys[keyID] == nil {
		t.Fatalf("unknown key ID %q", keyID)
	}
	return s.keys[keyID]
}

func (s *oidcTestServer) config(now time.Time) OIDCVerifierConfig {
	s.t.Helper()
	s.clock = now

	issuerURL, err := url.Parse(s.issuer)
	if err != nil {
		s.t.Fatalf("url.Parse() error = %v", err)
	}
	return OIDCVerifierConfig{
		IssuerURL:  issuerURL,
		Audience:   "test-audience",
		HTTPClient: s.server.Client(),
		Clock: func() time.Time {
			return now
		},
	}
}

func (s *oidcTestServer) requestCounts() (discovery, jwks int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.discoveryRequests, s.jwksRequests
}

func (s *oidcTestServer) requireJWKSRequests(t *testing.T, want int) {
	t.Helper()
	if _, jwks := s.requestCounts(); jwks != want {
		t.Fatalf("JWKS requests = %d, want %d", jwks, want)
	}
}

func (s *oidcTestServer) serveHTTP(writer http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	jwksPath := s.jwksPath
	s.mu.Unlock()

	switch request.URL.Path {
	case "/" + s.issuerPathToken + "/.well-known/openid-configuration":
		s.serveDiscovery(writer)
	case jwksPath:
		s.serveJWKS(writer)
	default:
		http.NotFound(writer, request)
	}
}

func (s *oidcTestServer) serveDiscovery(writer http.ResponseWriter) {
	type document struct {
		Issuer                string `json:"issuer"`
		JWKSURI               string `json:"jwks_uri"`
		AuthorizationEndpoint string `json:"authorization_endpoint,omitempty"`
		TokenEndpoint         string `json:"token_endpoint,omitempty"`
		EndSessionEndpoint    string `json:"end_session_endpoint,omitempty"`
	}

	s.mu.Lock()
	s.discoveryRequests++
	doc := document{Issuer: s.discoveryIssuer, JWKSURI: s.server.URL + s.jwksPath}
	if !s.omitEndpoints {
		doc.AuthorizationEndpoint = s.authorizationEndpoint
		doc.TokenEndpoint = s.tokenEndpoint
		doc.EndSessionEndpoint = s.endSessionEndpoint
	}
	unavailable, override := s.unavailableBody, s.discoveryBody
	s.mu.Unlock()

	s.respond(writer, unavailable, override, doc)
}

func (s *oidcTestServer) serveJWKS(writer http.ResponseWriter) {
	s.mu.Lock()
	s.jwksRequests++
	jwks := jwksDocument{Keys: make([]map[string]string, 0, len(s.published))}
	for _, keyID := range s.published {
		jwks.Keys = append(jwks.Keys, rsaJWK(keyID, &s.keys[keyID].PublicKey))
	}
	unavailable, override := s.unavailableBody, s.jwksBody
	s.mu.Unlock()

	s.respond(writer, unavailable, override, jwks)
}

// respond serves an outage while unavailable is set, the override body while
// that is, and doc as JSON otherwise.
func (s *oidcTestServer) respond(writer http.ResponseWriter, unavailable, override string, doc any) {
	switch {
	case unavailable != "":
		http.Error(writer, unavailable, http.StatusServiceUnavailable)
	case override != "":
		_, _ = writer.Write([]byte(override))
	default:
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(doc); err != nil {
			s.t.Errorf("Encode response: %v", err)
		}
	}
}

func rsaJWK(keyID string, key *rsa.PublicKey) map[string]string {
	return map[string]string{
		"alg": "RS256",
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		"kid": keyID,
		"kty": "RSA",
		"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"use": "sig",
	}
}

func TestOIDCVerifierExposesDiscoveredEndpoints(t *testing.T) {
	server := newKeyedOIDCTestServer(t)
	verifier := startVerifier(t, server.config(time.Now()))

	want := Endpoints{Authorization: server.authorizationEndpoint, Token: server.tokenEndpoint, EndSession: server.endSessionEndpoint}
	if endpoints := verifier.Endpoints(); endpoints != want {
		t.Fatalf("Endpoints() = %#v, want %#v", endpoints, want)
	}
}

func TestOIDCVerifierAcceptsProviderWithoutEndSessionEndpoint(t *testing.T) {
	// end_session_endpoint is optional; logout degrades to clearing our cookie.
	server := newKeyedOIDCTestServer(t)
	server.endSessionEndpoint = ""

	if endpoints := startVerifier(t, server.config(time.Now())).Endpoints(); endpoints.EndSession != "" {
		t.Fatalf("EndSession = %q, want empty", endpoints.EndSession)
	}
}

func TestOIDCVerifierErrorsAreExactlySafeCategories(t *testing.T) {
	s := newKeyedOIDCTestServer(t)
	v := startVerifier(t, s.config(time.Now()))

	_, err := v.Verify(context.Background(), "not-a-jwt")
	if !errors.Is(err, ErrInvalidToken) || err.Error() != "invalid access token" {
		t.Fatalf("invalid error = %v", err)
	}

	s.addRSAKey(t, "key-b")
	s.update(func() { s.unavailableBody = "provider-detail" })
	_, err = v.Verify(context.Background(), s.sign(t, "key-b", nil))
	if !errors.Is(err, ErrVerifierUnavailable) || err.Error() != "token verifier unavailable" {
		t.Fatalf("unavailable error = %v", err)
	}
}

func TestOIDCVerifierUsesCachedKeyWithoutRepeatedJWKSFetch(t *testing.T) {
	s := newKeyedOIDCTestServer(t)
	v := startVerifier(t, s.config(time.Now()))

	raw := s.sign(t, "key-a", nil)
	for range 2 {
		if _, err := v.Verify(context.Background(), raw); err != nil {
			t.Fatal(err)
		}
	}
	s.requireJWKSRequests(t, 1)
}

func TestOIDCVerifierRefreshesJWKSForRotatedKey(t *testing.T) {
	for _, test := range []struct {
		name      string
		keyID     string
		published []string
	}{
		{name: "new key ID", keyID: "key-b", published: []string{"key-a", "key-b"}},
		{name: "same key ID, new key", keyID: "key-a", published: []string{"key-a"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := newKeyedOIDCTestServer(t)
			v := startVerifier(t, s.config(time.Now()))

			s.addRSAKey(t, test.keyID)
			s.publish(test.published...)
			if _, err := v.Verify(context.Background(), s.sign(t, test.keyID, nil)); err != nil {
				t.Fatalf("Verify() error = %v", err)
			}
			s.requireJWKSRequests(t, 2)
		})
	}
}

func TestOIDCVerifierCoordinatesConcurrentUnknownKIDRefresh(t *testing.T) {
	s := newKeyedOIDCTestServer(t)
	v := startVerifier(t, s.config(time.Now()))

	s.addRSAKey(t, "key-b")
	s.publish("key-a", "key-b")
	raw := s.sign(t, "key-b", nil)
	start, errs := make(chan struct{}), make(chan error, 16)
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			<-start
			_, err := v.Verify(context.Background(), raw)
			errs <- err
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Verify() error = %v", err)
		}
	}
	s.requireJWKSRequests(t, 2)
}

// A cached key outlives a provider outage only while the JWKS it came from is
// within its freshness bound; past that, the verifier fails closed.
func TestOIDCVerifierTrustsCachedKeysOnlyWhileFreshDuringProviderOutage(t *testing.T) {
	for _, test := range []struct {
		name     string
		elapsed  time.Duration
		wantErr  error
		wantJWKS int
	}{
		{name: "fresh", elapsed: 9 * time.Second, wantJWKS: 1},
		{name: "expired", elapsed: 11 * time.Second, wantErr: ErrVerifierUnavailable, wantJWKS: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := newKeyedOIDCTestServer(t)
			now := time.Now().UTC().Truncate(time.Second)
			cfg := s.config(now)
			cfg.JWKSMinRefreshInterval = 10 * time.Second
			cfg.JWKSMaxRefreshInterval = 20 * time.Second
			cfg.Clock = func() time.Time {
				return now
			}
			v := startVerifier(t, cfg)

			now = now.Add(test.elapsed)
			s.update(func() { s.unavailableBody = "idp-down" })
			if _, err := v.Verify(context.Background(), s.sign(t, "key-a", nil)); !errors.Is(err, test.wantErr) {
				t.Fatalf("Verify() error = %v, want %v", err, test.wantErr)
			}
			s.requireJWKSRequests(t, test.wantJWKS)
		})
	}
}

func TestOIDCVerifierFailsClosedAfterJWKSFreshnessExpiresDuringProviderOutage(t *testing.T) {
	s := newKeyedOIDCTestServer(t)
	cfg := s.config(time.Now())
	cfg.Clock = time.Now
	cfg.JWKSMinRefreshInterval = time.Second
	cfg.JWKSMaxRefreshInterval = time.Second
	v := startVerifier(t, cfg)

	time.Sleep(2 * time.Second)
	s.requireJWKSRequests(t, 1) // no automatic refresh behind the verifier's back
	s.update(func() { s.unavailableBody = "idp-down" })
	_, err := v.Verify(context.Background(), s.sign(t, "key-a", nil))
	if !errors.Is(err, ErrVerifierUnavailable) {
		t.Fatalf("Verify() error = %v, want ErrVerifierUnavailable", err)
	}
	s.requireJWKSRequests(t, 2)
}

func TestOIDCVerifierReturnsUnavailableWhenNoUsableKeyCanBeFetched(t *testing.T) {
	s := newKeyedOIDCTestServer(t)
	v := startVerifier(t, s.config(time.Now()))

	s.addRSAKey(t, "key-b")
	s.update(func() { s.unavailableBody = "idp-down" })
	_, err := v.Verify(context.Background(), s.sign(t, "key-b", nil))
	if !errors.Is(err, ErrVerifierUnavailable) {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestOIDCVerifierValidatedTokenAllowsMissingNotBefore(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	tok := jwt.New()
	_ = tok.Set(jwt.IssuerKey, "https://issuer.example")
	_ = tok.Set(jwt.AudienceKey, []string{"test-audience"})
	_ = tok.Set(jwt.ExpirationKey, now.Add(time.Hour))
	_ = tok.Set(jwt.SubjectKey, "user-123")
	payload, err := json.Marshal(tok)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	v := &OIDCVerifier{
		cfg: OIDCVerifierConfig{
			Audience: "test-audience",
			Clock: func() time.Time {
				return now
			},
		},
		discovery: discoveryDocument{Issuer: "https://issuer.example"},
	}
	got, err := v.validatedToken(payload)
	if err != nil {
		t.Fatalf("validatedToken() error = %v", err)
	}
	if got.Subject != "user-123" {
		t.Fatalf("validatedToken().Subject = %q, want user-123", got.Subject)
	}
}

func TestOIDCVerifierRejectsInvalidTokens(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	for _, test := range []struct {
		name    string
		prepare func(t *testing.T, s *oidcTestServer)
		raw     func(t *testing.T, s *oidcTestServer) string
	}{
		{
			name: "empty input",
			raw: func(*testing.T, *oidcTestServer) string {
				return ""
			},
		},
		{
			name: "input larger than 16 KiB",
			raw: func(t *testing.T, s *oidcTestServer) string {
				return s.sign(t, "key-a", func(tok jwt.Token) {
					_ = tok.Set("padding", strings.Repeat("x", maxTokenBytes))
				})
			},
		},
		{
			name: "not three segments",
			raw: func(*testing.T, *oidcTestServer) string {
				return "not.a.compact.jws"
			},
		},
		{
			name: "missing kid",
			raw: func(t *testing.T, s *oidcTestServer) string {
				return signJWS(t, "", jwa.RS256(), s.key(t, "key-a"), s.accessToken(nil))
			},
		},
		{
			name: "none algorithm",
			raw: func(t *testing.T, s *oidcTestServer) string {
				return s.compactWithHeader(t, map[string]any{"alg": "none", "kid": "key-a"})
			},
		},
		{
			name: "HS256 algorithm",
			raw: func(t *testing.T, s *oidcTestServer) string {
				return signJWS(t, "key-a", jwa.HS256(), []byte("test-hmac-key"), s.accessToken(nil))
			},
		},
		{
			name: "unsupported asymmetric algorithm",
			raw: func(t *testing.T, s *oidcTestServer) string {
				return s.compactWithHeader(t, map[string]any{"alg": "ES256K", "kid": "key-a"})
			},
		},
		{
			name: "JWK use mismatch",
			prepare: func(t *testing.T, s *oidcTestServer) {
				s.setJWK(t, "key-a", func(key map[string]string) {
					key["use"] = "enc"
				})
			},
			raw: func(t *testing.T, s *oidcTestServer) string {
				return s.sign(t, "key-a", nil)
			},
		},
		{
			name: "JWK algorithm mismatch",
			prepare: func(t *testing.T, s *oidcTestServer) {
				s.setJWK(t, "key-a", func(key map[string]string) {
					key["alg"] = "RS512"
				})
			},
			raw: func(t *testing.T, s *oidcTestServer) string {
				return s.sign(t, "key-a", nil)
			},
		},
		{
			name: "changed signature",
			raw: func(t *testing.T, s *oidcTestServer) string {
				return tamperSignature(t, s.sign(t, "key-a", nil))
			},
		},
		{
			name: "expired expiration",
			raw:  signedWith(jwt.ExpirationKey, now.Add(-clockSkew-time.Second)),
		},
		{
			name: "future not before",
			raw:  signedWith(jwt.NotBeforeKey, now.Add(clockSkew+time.Second)),
		},
		{
			name: "wrong issuer",
			raw:  signedWith(jwt.IssuerKey, "https://other-issuer.example"),
		},
		{
			name: "wrong audience",
			raw:  signedWith(jwt.AudienceKey, []string{"other-audience"}),
		},
		{
			name: "missing subject",
			raw: func(t *testing.T, s *oidcTestServer) string {
				return s.sign(t, "key-a", func(tok jwt.Token) {
					_ = tok.Remove(jwt.SubjectKey)
				})
			},
		},
		{
			name: "empty subject",
			raw:  signedWith(jwt.SubjectKey, ""),
		},
		{
			name: "non-string presentation claim",
			raw:  signedWith("name", []string{"Ada Lovelace"}),
		},
		// OIDC Core 1.0 §3.1.3.7 steps 4-5: azp must equal our client ID
		// whenever it is present, and is required once aud carries more than
		// one value.
		{
			// The exploitable path: an ID token minted by the same issuer for a
			// different client, with our client ID pushed into aud by a stray
			// audience mapper. If azp names that other client, this must not
			// authenticate as us.
			name: "multi-audience token with wrong azp",
			raw:  signedWith(jwt.AudienceKey, []string{"test-audience", "other-client"}, "azp", "other-client"),
		},
		{
			name: "multi-audience token with absent azp",
			raw:  signedWith(jwt.AudienceKey, []string{"test-audience", "other-client"}),
		},
		{
			// The substitution azp actually guards against: another client in
			// the same realm holds an ID token minted for itself, and a stray
			// audience mapper has replaced our audience into it, leaving aud a
			// single entry that names us. Only azp still names the client the
			// token was issued to, so it must be consulted even when aud carries
			// one value.
			name: "single-audience token with wrong azp",
			raw:  signedWith("azp", "other-client"),
		},
		{
			name: "non-string azp",
			raw:  signedWith("azp", []string{"test-audience"}),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := newKeyedOIDCTestServer(t)
			if test.prepare != nil {
				test.prepare(t, s)
			}
			v := startVerifier(t, s.config(now))

			_, err := v.Verify(context.Background(), test.raw(t, s))
			if !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("Verify() error = %v, want ErrInvalidToken", err)
			}
		})
	}
}

// signedWith builds a token for the default identity with the given claims,
// named and valued in pairs, overriding the defaults.
func signedWith(claims ...any) func(t *testing.T, s *oidcTestServer) string {
	return func(t *testing.T, s *oidcTestServer) string {
		return s.sign(t, "key-a", func(tok jwt.Token) {
			for index := 0; index < len(claims); index += 2 {
				_ = tok.Set(claims[index].(string), claims[index+1])
			}
		})
	}
}

func TestOIDCVerifierRedactsTokenAndProviderDetails(t *testing.T) {
	const (
		rawTokenFragment = "raw-token-private-fragment"
		claimString      = "claim-private-fragment"
		fixtureResponse  = "fixture-failure-response"
	)

	s := newKeyedOIDCTestServer(t)
	v := startVerifier(t, s.config(time.Now()))
	s.update(func() { s.unavailableBody = fixtureResponse })

	for _, raw := range []string{
		rawTokenFragment + ".invalid",
		signedWith(jwt.IssuerKey, claimString)(t, s),
	} {
		_, err := v.Verify(context.Background(), raw)
		if !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("Verify() error = %v, want ErrInvalidToken", err)
		}
		for _, secret := range []string{rawTokenFragment, claimString, fixtureResponse} {
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("Verify() leaked %q in error %q", secret, err)
			}
		}
	}
	s.addRSAKey(t, "key-b")
	_, err := v.Verify(context.Background(), s.sign(t, "key-b", nil))
	requireUnavailable(t, err)
	s.requireJWKSRequests(t, 2)
}

func (s *oidcTestServer) sign(t *testing.T, keyID string, mutate func(jwt.Token)) string {
	t.Helper()
	return signJWS(t, keyID, jwa.RS256(), s.key(t, keyID), s.accessToken(mutate))
}

// signLogout signs a logout token with keyID, filling in iss, aud, and a fresh
// iat unless claims sets them. A logout token carries none of the ID-token
// claims accessToken defaults, so it is not built from one.
func (s *oidcTestServer) signLogout(t *testing.T, keyID string, claims map[string]any) string {
	t.Helper()
	merged := map[string]any{"iss": s.issuer, "aud": []string{"test-audience"}, "iat": time.Now().Unix()}
	maps.Copy(merged, claims)
	return signJWS(t, keyID, jwa.RS256(), s.key(t, keyID), merged)
}

// signJWS signs claims as a compact JWS, naming keyID in the protected header
// unless it is empty.
func signJWS(t *testing.T, keyID string, algorithm jwa.SignatureAlgorithm, key, claims any) string {
	t.Helper()

	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	protected := jws.NewHeaders()
	if keyID != "" {
		if err := protected.Set("kid", keyID); err != nil {
			t.Fatalf("protected.Set() error = %v", err)
		}
	}
	raw, err := jws.Sign(payload, jws.WithKey(algorithm, key, jws.WithProtectedHeaders(protected)))
	if err != nil {
		t.Fatalf("jws.Sign() error = %v", err)
	}
	return string(raw)
}

func (s *oidcTestServer) compactWithHeader(t *testing.T, header map[string]any) string {
	t.Helper()

	encodedHeader, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("json.Marshal(header) error = %v", err)
	}
	payload, err := json.Marshal(s.accessToken(nil))
	if err != nil {
		t.Fatalf("json.Marshal(payload) error = %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(encodedHeader) + "." + base64.RawURLEncoding.EncodeToString(payload) + "."
}

func (s *oidcTestServer) accessToken(mutate func(jwt.Token)) jwt.Token {
	s.mu.Lock()
	now := s.clock
	issuer := s.issuer
	s.mu.Unlock()
	if now.IsZero() {
		now = time.Now()
	}

	tok := jwt.New()
	_ = tok.Set(jwt.IssuerKey, issuer)
	_ = tok.Set(jwt.AudienceKey, []string{"test-audience"})
	_ = tok.Set(jwt.ExpirationKey, now.Add(time.Hour))
	_ = tok.Set(jwt.NotBeforeKey, now.Add(-time.Minute))
	_ = tok.Set(jwt.SubjectKey, "user-123")
	if mutate != nil {
		mutate(tok)
	}
	return tok
}

func (s *oidcTestServer) setJWK(t *testing.T, keyID string, mutate func(map[string]string)) {
	t.Helper()

	serialized := rsaJWK(keyID, &s.key(t, keyID).PublicKey)
	mutate(serialized)
	body, err := json.Marshal(jwksDocument{Keys: []map[string]string{serialized}})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	s.update(func() { s.jwksBody = string(body) })
}

func tamperSignature(t *testing.T, raw string) string {
	t.Helper()

	segments := strings.Split(raw, ".")
	if len(segments) != 3 || segments[2] == "" {
		t.Fatalf("unexpected compact JWS %q", raw)
	}
	if segments[2][0] == 'A' {
		segments[2] = "B" + segments[2][1:]
	} else {
		segments[2] = "A" + segments[2][1:]
	}
	return strings.Join(segments, ".")
}

func TestOIDCVerifierAcceptsProviderTokenShapes(t *testing.T) {
	ada := func(tok jwt.Token) {
		_ = tok.Set("name", "Ada Lovelace")
		_ = tok.Set("preferred_username", "ada")
		_ = tok.Set("email", "ada@example.test")
	}
	adaToken := VerifiedToken{Subject: "user-123", Name: "Ada Lovelace", PreferredUsername: "ada", Email: "ada@example.test"}

	for _, test := range []struct {
		name   string
		mutate func(jwt.Token)
		want   VerifiedToken
	}{
		{
			name:   "Okta shaped token without typ or realm_access",
			mutate: ada,
			want:   adaToken,
		},
		{
			// realm_access is malformed on purpose: authorization is OpenFGA's,
			// so the claim is not read and cannot fail verification.
			name: "realm_access shaped token with a malformed realm_access",
			mutate: func(tok jwt.Token) {
				ada(tok)
				_ = tok.Set("realm_access", map[string]any{"roles": []any{"platform-admin", 1}})
			},
			want: adaToken,
		},
		{
			name: "realm_access shaped token with typ Bearer and realm_access",
			mutate: func(tok jwt.Token) {
				ada(tok)
				_ = tok.Set("typ", "Bearer")
				_ = tok.Set("realm_access", map[string]any{"roles": []string{"platform-admin"}})
			},
			want: adaToken,
		},
		{
			name: "realm_access shaped ID token with typ ID",
			mutate: func(tok jwt.Token) {
				ada(tok)
				_ = tok.Set("typ", "ID")
			},
			want: adaToken,
		},
		{
			// Every claim but sub is optional. nonce in particular is optional
			// in the code flow: an absent claim is a valid token, not a
			// malformed one, and the callback decides whether it had to match.
			// azp is optional too while aud carries one value.
			name: "minimal token carrying only the subject",
			want: VerifiedToken{Subject: "user-123"},
		},
		{
			// sid is copied into the session at sign-in: it is the key a
			// back-channel logout arrives on.
			name: "nonce and session ID",
			mutate: func(tok jwt.Token) {
				_ = tok.Set("nonce", "nonce-value")
				_ = tok.Set("sid", "idp-sid-1")
			},
			want: VerifiedToken{Subject: "user-123", Nonce: "nonce-value", SessionID: "idp-sid-1"},
		},
		{
			name: "string audience",
			mutate: func(tok jwt.Token) {
				_ = tok.Set(jwt.AudienceKey, "test-audience")
			},
			want: VerifiedToken{Subject: "user-123"},
		},
		{
			name: "single-audience token with matching azp",
			mutate: func(tok jwt.Token) {
				_ = tok.Set("azp", "test-audience")
			},
			want: VerifiedToken{Subject: "user-123"},
		},
		{
			name: "multi-audience token with matching azp",
			mutate: func(tok jwt.Token) {
				_ = tok.Set(jwt.AudienceKey, []string{"test-audience", "other-client"})
				_ = tok.Set("azp", "test-audience")
			},
			want: VerifiedToken{Subject: "user-123"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := newKeyedOIDCTestServer(t)
			now := time.Now()
			v := startVerifier(t, s.config(now))

			got, err := v.Verify(context.Background(), s.sign(t, "key-a", test.mutate))
			if err != nil {
				t.Fatalf("Verify() error = %v", err)
			}
			want := test.want
			want.ExpiresAt = now.Add(time.Hour).Truncate(time.Second).UTC()
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Verify() = %#v, want %#v", got, want)
			}
		})
	}
}

// Moving the key cache to a new jwks_uri runs only when discovery is re-read
// after its TTL and the provider has moved the endpoint, so that is what this
// arranges: rotate the key, move the endpoint, advance past the TTL, and
// require the verifier to follow.
func TestOIDCVerifierReplacesKeyCacheWhenDiscoveryMovesTheJWKSURI(t *testing.T) {
	s := newKeyedOIDCTestServer(t)

	start := time.Now()
	clock := start
	cfg := s.config(start)
	cfg.DiscoveryTTL = 15 * time.Minute
	cfg.Clock = func() time.Time { return clock }
	v := startVerifier(t, cfg)

	if _, err := v.Verify(context.Background(), s.sign(t, "key-a", nil)); err != nil {
		t.Fatalf("Verify() with the original key error = %v", err)
	}
	originalURL := v.jwksURL()

	// The provider rotates its signing key and serves the new set from a
	// different endpoint.
	s.addRSAKey(t, "key-b")
	s.update(func() { s.jwksPath = "/jwks-rotated" })
	s.publish("key-b")

	// Before the TTL elapses discovery is not re-read, so the move is not yet
	// visible and the unknown key ID is simply refused.
	if _, err := v.Verify(context.Background(), s.sign(t, "key-b", nil)); err == nil {
		t.Fatal("Verify() with the rotated key succeeded before the discovery TTL elapsed, want an error")
	}
	if got := v.jwksURL(); got != originalURL {
		t.Fatalf("jwks url = %q, want it unchanged at %q before the TTL elapsed", got, originalURL)
	}

	clock = start.Add(cfg.DiscoveryTTL + time.Minute)

	if _, err := v.Verify(context.Background(), s.sign(t, "key-b", nil)); err != nil {
		t.Fatalf("Verify() with the rotated key error = %v", err)
	}
	rotatedURL := v.jwksURL()
	if rotatedURL == originalURL {
		t.Fatalf("jwks url = %q, want the moved endpoint", rotatedURL)
	}
	if !strings.HasSuffix(rotatedURL, "/jwks-rotated") {
		t.Fatalf("jwks url = %q, want it to end in /jwks-rotated", rotatedURL)
	}

	// The replaced cache holds the new set only: the retired key is gone
	// rather than lingering alongside its replacement.
	if _, err := v.Verify(context.Background(), s.sign(t, "key-a", nil)); err == nil {
		t.Fatal("Verify() with the retired key succeeded, want an error")
	}
}

// jwksURL reads the URL the verifier's current key cache is registered against.
func (v *OIDCVerifier) jwksURL() string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.keys == nil {
		return ""
	}
	return v.keys.url
}
