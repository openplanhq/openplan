package authentication

import (
	"net/http"
	"testing"
	"time"
)

func TestVerifierContractUsesSafeStableErrorsAndDefaults(t *testing.T) {
	t.Parallel()

	if got := ErrInvalidToken.Error(); got != "invalid access token" {
		t.Fatalf("ErrInvalidToken = %q", got)
	}
	if got := ErrVerifierUnavailable.Error(); got != "token verifier unavailable" {
		t.Fatalf("ErrVerifierUnavailable = %q", got)
	}
	cfg := OIDCVerifierConfig{}
	cfg = cfg.withDefaults()
	if cfg.DiscoveryTTL != 15*time.Minute || cfg.JWKSMinRefreshInterval != time.Minute ||
		cfg.JWKSMaxRefreshInterval != 15*time.Minute || cfg.RefreshCooldown != 5*time.Second {
		t.Fatalf("unexpected cache defaults: %#v", cfg)
	}
}

// The verifier only ever talks to the provider through a hardened copy of the
// configured client, so hardening must leave the caller's client as it was.
func TestHardenedProviderClientCopiesTheSuppliedClient(t *testing.T) {
	t.Parallel()

	client := &http.Client{Timeout: 3 * time.Second}
	hardened := hardenedProviderClient(client, nil)
	if hardened == client || client.Transport != nil || client.CheckRedirect != nil {
		t.Fatalf("hardening mutated the supplied client: %#v", client)
	}
	if hardened.Timeout != client.Timeout {
		t.Fatalf("hardened timeout = %s, want the supplied %s", hardened.Timeout, client.Timeout)
	}
	if got := hardenedProviderClient(&http.Client{}, nil).Timeout; got != defaultHTTPTimeout {
		t.Fatalf("hardened timeout = %s, want the default %s", got, defaultHTTPTimeout)
	}
}
