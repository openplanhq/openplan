package authentication

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestNewOIDCVerifierLoadsDiscoveryAndInitialJWKS(t *testing.T) {
	s := newKeyedOIDCTestServer(t)
	startVerifier(t, s.config(time.Now()))

	if discovery, jwks := s.requestCounts(); discovery != 1 || jwks != 1 {
		t.Fatalf("unexpected startup requests: discovery=%d jwks=%d", discovery, jwks)
	}
}

// Every refusal is the one opaque sentinel, and requireUnavailable holds it to
// that sentinel's exact message: nothing from the provider's response, the
// discovery document, or the issuer URL can reach a caller.
func TestNewOIDCVerifierRefusesAnUnusableProvider(t *testing.T) {
	oversized := strings.Repeat("x", maxProviderResponseBytes+1)

	for _, test := range []struct {
		name    string
		prepare func(*oidcTestServer)
	}{
		{name: "issuer mismatch", prepare: func(s *oidcTestServer) { s.discoveryIssuer = s.issuer + "/unexpected" }},
		// Without an authorization or token endpoint the API cannot run the
		// flow at all, so failing at construction beats failing on the first
		// login.
		{name: "missing flow endpoints", prepare: func(s *oidcTestServer) { s.omitEndpoints = true }},
		{name: "unavailable", prepare: func(s *oidcTestServer) { s.unavailableBody = "provider-private-detail" }},
		{name: "unavailable with a large body", prepare: func(s *oidcTestServer) {
			s.unavailableBody = "provider-private-detail-" + strings.Repeat("x", 20_000)
		}},
		{name: "unavailable with an oversized body", prepare: func(s *oidcTestServer) { s.unavailableBody = oversized }},
		{name: "oversized discovery", prepare: func(s *oidcTestServer) { s.discoveryBody = oversized }},
		{name: "oversized JWKS", prepare: func(s *oidcTestServer) { s.jwksBody = oversized }},
		{name: "malformed JWKS", prepare: func(s *oidcTestServer) {
			s.jwksBody = `{"keys":[{"kid":"provider-private-malformed-jwks-detail"}`
		}},
		{name: "duplicate key IDs", prepare: func(s *oidcTestServer) { s.published = []string{"key-a", "key-a"} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := newKeyedOIDCTestServer(t)
			s.update(func() { test.prepare(s) })

			result := make(chan error, 1)
			go func() {
				_, err := NewOIDCVerifier(context.Background(), s.config(time.Now()))
				result <- err
			}()
			select {
			case err := <-result:
				requireUnavailable(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("NewOIDCVerifier() did not return")
			}
		})
	}
}

func TestNewOIDCVerifierAllowsProviderResponseAtSizeLimit(t *testing.T) {
	s := newKeyedOIDCTestServer(t)

	discovery, err := json.Marshal(discoveryDocument{
		Issuer:                s.issuer,
		JWKSURI:               s.server.URL + "/jwks",
		AuthorizationEndpoint: s.authorizationEndpoint,
		TokenEndpoint:         s.tokenEndpoint,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	s.update(func() {
		s.discoveryBody = string(discovery) + strings.Repeat(" ", maxProviderResponseBytes-len(discovery))
	})

	startVerifier(t, s.config(time.Now()))
}

func TestNewOIDCVerifierRejectsInvalidConfiguration(t *testing.T) {
	issuer, err := url.Parse("https://issuer.example/issuer-private-path")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}

	for _, test := range []struct {
		name   string
		mutate func(*OIDCVerifierConfig)
	}{
		{name: "nil issuer", mutate: func(cfg *OIDCVerifierConfig) { cfg.IssuerURL = nil }},
		{name: "empty audience", mutate: func(cfg *OIDCVerifierConfig) { cfg.Audience = "" }},
		{name: "negative discovery ttl", mutate: func(cfg *OIDCVerifierConfig) { cfg.DiscoveryTTL = -time.Second }},
		{name: "negative minimum refresh interval", mutate: func(cfg *OIDCVerifierConfig) { cfg.JWKSMinRefreshInterval = -time.Second }},
		{name: "negative maximum refresh interval", mutate: func(cfg *OIDCVerifierConfig) { cfg.JWKSMaxRefreshInterval = -time.Second }},
		{name: "negative refresh cooldown", mutate: func(cfg *OIDCVerifierConfig) { cfg.RefreshCooldown = -time.Second }},
		{name: "minimum interval exceeds maximum interval", mutate: func(cfg *OIDCVerifierConfig) {
			cfg.JWKSMinRefreshInterval = 2 * time.Minute
			cfg.JWKSMaxRefreshInterval = time.Minute
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := OIDCVerifierConfig{IssuerURL: issuer, Audience: "audience"}
			test.mutate(&cfg)

			_, err := NewOIDCVerifier(context.Background(), cfg)
			requireUnavailable(t, err)
		})
	}
}

func TestOIDCVerifierCloseIsIdempotent(t *testing.T) {
	s := newKeyedOIDCTestServer(t)

	verifier, err := NewOIDCVerifier(context.Background(), s.config(time.Now()))
	if err != nil {
		t.Fatalf("NewOIDCVerifier() error = %v", err)
	}
	if err := verifier.Close(context.Background()); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := verifier.Close(context.Background()); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}
