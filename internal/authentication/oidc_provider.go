package authentication

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/lestrrat-go/httprc/v3"
	"github.com/lestrrat-go/httprc/v3/errsink"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
)

var (
	errProviderResponseTooLarge = errors.New("provider response exceeds size limit")
	errRefreshCooldown          = errors.New("oidc refresh cooldown")
	errJWKSCacheExpired         = errors.New("oidc JWKS cache expired")
	errKeyCacheUnavailable      = errors.New("OIDC key cache is unavailable")
)

// manualJWKSRefreshInterval prevents jwk.Cache from refreshing outside the
// verifier's coordinated refresh path. Only a successful synchronous refresh
// may extend the verifier's bounded freshness window.
const manualJWKSRefreshInterval = 100 * 365 * 24 * time.Hour

type discoveryDocument struct {
	Issuer                string `json:"issuer"`
	JWKSURI               string `json:"jwks_uri"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	EndSessionEndpoint    string `json:"end_session_endpoint"`
}

type keyCache struct {
	url        string
	cache      *jwk.Cache
	set        jwk.CachedSet
	freshUntil time.Time
}

type OIDCVerifier struct {
	cfg         OIDCVerifierConfig
	mu          sync.RWMutex
	discovery   discoveryDocument
	discovered  time.Time
	keys        *keyCache
	refreshMu   sync.Mutex
	refreshedAt time.Time
}

func NewOIDCVerifier(ctx context.Context, cfg OIDCVerifierConfig) (*OIDCVerifier, error) {
	cfg = cfg.withDefaults()
	if !validOIDCVerifierConfig(cfg) {
		return nil, ErrVerifierUnavailable
	}

	providerClient := cfg.HTTPClient
	cfg.HTTPClient = hardenedProviderClient(providerClient, nil)
	discovery, err := fetchDiscovery(ctx, cfg.IssuerURL, cfg.HTTPClient)
	if err != nil {
		return nil, ErrVerifierUnavailable
	}

	// providerClient, not cfg.HTTPClient: the raw configured client is hardened
	// once inside newKeyCache. cfg.HTTPClient is already the hardened one by
	// this point, and a key cache replaced on a later discovery re-hardens that.
	keys, err := newKeyCache(ctx, cfg, discovery.JWKSURI, providerClient)
	if err != nil {
		return nil, ErrVerifierUnavailable
	}

	return &OIDCVerifier{
		cfg:        cfg,
		discovery:  discovery,
		discovered: cfg.Clock(),
		keys:       keys,
	}, nil
}

// errDuplicateJWKSKeyIDs reports a JWKS that maps one key ID to more than one
// key. Which key a token means is then ambiguous, so the set is refused rather
// than resolved arbitrarily.
var errDuplicateJWKSKeyIDs = errors.New("OIDC JWKS contains duplicate key IDs")

// newKeyCache builds one registered, freshness-bounded JWKS cache for jwksURI.
// It is the whole of what construction and a key rotation both need, and was
// written out twice before this -- forty lines of security-relevant setup that
// a hardening change could reach one copy of and miss the other.
//
// client is a parameter rather than read from cfg because the two callers pass
// different things, and always have: construction passes the raw configured
// client, while a discovery that moves jwks_uri passes the already-hardened one
// off the verifier.
//
// Every failure shuts down the cache it created, so a caller never has to clean
// up something it was never handed. On success the caller owns the cache and
// must Shutdown it eventually.
func newKeyCache(ctx context.Context, cfg OIDCVerifierConfig, jwksURI string, client *http.Client) (*keyCache, error) {
	registrationCtx, cancelRegistration := context.WithCancel(ctx)
	defer cancelRegistration()

	// A transport failure reported while registering cancels registrationCtx,
	// so Register gives up rather than going on against a provider that is
	// unreachable or answering with something oversized.
	cache, err := jwk.NewCache(ctx, httprc.NewClient(
		httprc.WithErrorSink(errsink.NewFunc(func(context.Context, error) {
			cancelRegistration()
		})),
	))
	if err != nil {
		return nil, err
	}
	keepCache := false
	defer func() {
		if !keepCache {
			_ = cache.Shutdown(context.WithoutCancel(ctx))
		}
	}()

	if err := cache.Register(
		registrationCtx,
		jwksURI,
		jwk.WithHTTPClient(hardenedProviderClient(client, cancelRegistration)),
		jwk.WithMinInterval(cfg.JWKSMinRefreshInterval),
		jwk.WithMaxInterval(cfg.JWKSMaxRefreshInterval),
	); err != nil {
		return nil, err
	}

	set, err := cache.CachedSet(jwksURI)
	if err != nil {
		return nil, err
	}
	freshUntil, err := pinJWKS(ctx, cfg, cache, jwksURI, set)
	if err != nil {
		return nil, err
	}

	keepCache = true
	return &keyCache{url: jwksURI, cache: cache, set: set, freshUntil: freshUntil}, nil
}

func (v *OIDCVerifier) Close(ctx context.Context) error {
	v.refreshMu.Lock()
	defer v.refreshMu.Unlock()
	return v.swapKeys(ctx, nil)
}

// swapKeys installs next as the verifier's key cache and shuts down the one it
// replaces.
func (v *OIDCVerifier) swapKeys(ctx context.Context, next *keyCache) error {
	v.mu.Lock()
	previous := v.keys
	v.keys = next
	v.mu.Unlock()

	if previous == nil || previous.cache == nil {
		return nil
	}
	return previous.cache.Shutdown(ctx)
}

func (v *OIDCVerifier) keyFor(ctx context.Context, kid string, algorithm jwa.SignatureAlgorithm) (any, error) {
	publicKey, found, err := v.cachedKeyFor(kid, algorithm)
	if err != nil && !errors.Is(err, errJWKSCacheExpired) {
		return nil, err
	}
	if found {
		return publicKey, nil
	}
	publicKey, _, err = v.refreshedKeyFor(ctx, kid, algorithm)
	return publicKey, err
}

// refreshedKeyFor forces a JWKS refresh and looks kid up again. The refresh's
// own error comes back alongside the key, because a caller that does find the
// key may still need to know whether the set it came from is current.
func (v *OIDCVerifier) refreshedKeyFor(ctx context.Context, kid string, algorithm jwa.SignatureAlgorithm) (any, error, error) {
	refreshErr := v.refreshKeys(ctx, true)
	publicKey, found, err := v.cachedKeyFor(kid, algorithm)
	switch {
	case errors.Is(err, errJWKSCacheExpired):
		return nil, refreshErr, ErrVerifierUnavailable
	case err != nil:
		return nil, refreshErr, err
	case !found && refreshErr != nil:
		return nil, refreshErr, ErrVerifierUnavailable
	case !found:
		return nil, refreshErr, ErrInvalidToken
	}
	return publicKey, refreshErr, nil
}

func (v *OIDCVerifier) cachedKeyFor(kid string, algorithm jwa.SignatureAlgorithm) (any, bool, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	keys := v.keys
	if keys == nil || keys.set == nil {
		return nil, false, ErrVerifierUnavailable
	}
	if !keys.freshUntil.After(v.cfg.Clock()) {
		return nil, false, errJWKSCacheExpired
	}
	if kid == "" {
		return nil, false, ErrInvalidToken
	}

	selected, err := findKey(keys.set, kid)
	if errors.Is(err, errDuplicateJWKSKeyIDs) {
		return nil, false, ErrInvalidToken
	}
	if err != nil || selected == nil {
		return nil, false, err
	}
	usage, hasUsage := selected.KeyUsage()
	configuredAlgorithm, hasAlgorithm := selected.Algorithm()
	if (hasUsage && usage != "sig") || (hasAlgorithm && configuredAlgorithm.String() != algorithm.String()) {
		return nil, false, ErrInvalidToken
	}

	var publicKey any
	if err := jwk.Export(selected, &publicKey); err != nil || publicKey == nil {
		return nil, false, ErrInvalidToken
	}
	return publicKey, true, nil
}

// findKey returns the key in set whose ID is kid, or nil when there is none.
// Keys without an ID are skipped, and a set naming any ID twice is refused
// with errDuplicateJWKSKeyIDs whether or not that ID is kid.
func findKey(set jwk.Set, kid string) (jwk.Key, error) {
	length := set.Len()
	if length < 0 {
		return nil, ErrVerifierUnavailable
	}
	keyIDs := make(map[string]struct{}, length)
	var selected jwk.Key
	for index := range length {
		key, ok := set.Key(index)
		if !ok {
			return nil, ErrVerifierUnavailable
		}
		keyID, ok := key.KeyID()
		if !ok || keyID == "" {
			continue
		}
		if _, duplicate := keyIDs[keyID]; duplicate {
			return nil, errDuplicateJWKSKeyIDs
		}
		keyIDs[keyID] = struct{}{}
		if keyID == kid {
			selected = key
		}
	}
	return selected, nil
}

func (v *OIDCVerifier) refreshKeys(ctx context.Context, force bool) error {
	v.refreshMu.Lock()
	defer v.refreshMu.Unlock()

	if force {
		now := v.cfg.Clock()
		if !v.refreshedAt.IsZero() && now.Sub(v.refreshedAt) < v.cfg.RefreshCooldown {
			return errRefreshCooldown
		}
		v.refreshedAt = now
	}

	if err := v.refreshDiscoveryIfDue(ctx); err != nil {
		return err
	}

	v.mu.RLock()
	keys := v.keys
	v.mu.RUnlock()
	if keys == nil || keys.cache == nil || keys.set == nil {
		return errKeyCacheUnavailable
	}

	set, err := keys.cache.Refresh(ctx, keys.url)
	if err != nil {
		return err
	}
	freshUntil, err := pinJWKS(ctx, v.cfg, keys.cache, keys.url, set)
	if err != nil {
		return err
	}
	v.mu.Lock()
	if v.keys == keys {
		keys.freshUntil = freshUntil
	}
	v.mu.Unlock()
	return nil
}

func (v *OIDCVerifier) refreshDiscoveryIfDue(ctx context.Context) error {
	now := v.cfg.Clock()
	v.mu.RLock()
	discovered, current := v.discovered, v.keys
	v.mu.RUnlock()
	if now.Sub(discovered) < v.cfg.DiscoveryTTL {
		return nil
	}

	discovery, err := fetchDiscovery(ctx, v.cfg.IssuerURL, v.cfg.HTTPClient)
	if err != nil {
		return err
	}
	if current == nil {
		return errKeyCacheUnavailable
	}
	if current.url != discovery.JWKSURI {
		next, err := newKeyCache(ctx, v.cfg, discovery.JWKSURI, v.cfg.HTTPClient)
		if err != nil {
			return err
		}
		if err := v.swapKeys(context.WithoutCancel(ctx), next); err != nil {
			return err
		}
	}

	v.mu.Lock()
	v.discovery = discovery
	v.discovered = now
	v.mu.Unlock()
	return nil
}

// pinJWKS vets a freshly fetched key set and returns how long it may be
// trusted, then pushes jwk.Cache's own next refresh out of reach so only the
// verifier's coordinated refresh path can extend that bound.
func pinJWKS(ctx context.Context, cfg OIDCVerifierConfig, cache *jwk.Cache, jwksURI string, set jwk.Set) (time.Time, error) {
	// An empty kid matches no key, so this only vets the set.
	if _, err := findKey(set, ""); err != nil {
		return time.Time{}, err
	}
	resource, err := cache.LookupResource(ctx, jwksURI)
	if err != nil {
		return time.Time{}, err
	}
	lifetime := min(max(time.Until(resource.Next()), cfg.JWKSMinRefreshInterval), cfg.JWKSMaxRefreshInterval)
	resource.SetNext(time.Now().Add(manualJWKSRefreshInterval))
	return cfg.Clock().Add(lifetime), nil
}

func validOIDCVerifierConfig(cfg OIDCVerifierConfig) bool {
	if cfg.IssuerURL == nil || cfg.Audience == "" ||
		cfg.DiscoveryTTL <= 0 || cfg.JWKSMinRefreshInterval <= 0 ||
		cfg.JWKSMaxRefreshInterval <= 0 || cfg.RefreshCooldown <= 0 ||
		cfg.JWKSMinRefreshInterval > cfg.JWKSMaxRefreshInterval {
		return false
	}
	return validProviderURL(cfg.IssuerURL)
}

// fetchDiscovery reads the provider's discovery document and refuses one that
// names another issuer or advertises an unusable endpoint. Construction and
// every later re-read go through it, so neither can accept a document the
// other would refuse.
func fetchDiscovery(ctx context.Context, issuerURL *url.URL, client *http.Client) (discoveryDocument, error) {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		issuerURL.String()+"/.well-known/openid-configuration",
		nil,
	)
	if err != nil {
		return discoveryDocument{}, err
	}

	response, err := client.Do(request)
	if err != nil {
		return discoveryDocument{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return discoveryDocument{}, errors.New("unexpected discovery status")
	}

	var discovery discoveryDocument
	if err := json.NewDecoder(response.Body).Decode(&discovery); err != nil {
		return discoveryDocument{}, err
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		return discoveryDocument{}, err
	}
	if discovery.Issuer != issuerURL.String() {
		return discoveryDocument{}, errors.New("OIDC discovery issuer mismatch")
	}

	// end_session_endpoint is optional: a provider without one degrades to
	// local logout. The flow cannot run without the other two.
	jwksURL, jwksOK := providerEndpoint(discovery.JWKSURI)
	_, authorizationOK := providerEndpoint(discovery.AuthorizationEndpoint)
	_, tokenOK := providerEndpoint(discovery.TokenEndpoint)
	_, endSessionOK := providerEndpoint(discovery.EndSessionEndpoint)
	if !jwksOK || !authorizationOK || !tokenOK || (!endSessionOK && discovery.EndSessionEndpoint != "") {
		return discoveryDocument{}, errors.New("OIDC discovery advertises an unusable endpoint")
	}
	discovery.JWKSURI = jwksURL.String()
	return discovery, nil
}

// providerEndpoint parses a URL the provider advertises, accepting only an
// absolute HTTP(S) URL that carries no user information.
func providerEndpoint(raw string) (*url.URL, bool) {
	parsed, err := url.Parse(raw)
	return parsed, err == nil && validProviderURL(parsed) && parsed.User == nil
}

func validProviderURL(value *url.URL) bool {
	return value != nil && value.IsAbs() && value.Hostname() != "" &&
		(value.Scheme == "http" || value.Scheme == "https")
}

func hardenedProviderClient(client *http.Client, onFailure func()) *http.Client {
	if onFailure == nil {
		onFailure = func() {}
	}
	hardened := *client
	if hardened.Timeout == 0 {
		hardened.Timeout = defaultHTTPTimeout
	}
	hardened.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	transport := hardened.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	hardened.Transport = providerResponseLimitTransport{base: transport, onFailure: onFailure}
	return &hardened
}

type providerResponseLimitTransport struct {
	base      http.RoundTripper
	onFailure func()
}

func (t providerResponseLimitTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if err != nil || response == nil || response.Body == nil {
		t.onFailure()
		return response, err
	}
	if response.StatusCode != http.StatusOK {
		t.onFailure()
	}
	response.Body = &providerResponseBody{
		ReadCloser: response.Body,
		remaining:  maxProviderResponseBytes,
		onFailure:  t.onFailure,
	}
	return response, nil
}

type providerResponseBody struct {
	io.ReadCloser
	remaining int64
	exceeded  bool
	onFailure func()
}

func (b *providerResponseBody) Read(buffer []byte) (int, error) {
	if b.exceeded {
		return 0, errProviderResponseTooLarge
	}
	if b.remaining == 0 {
		if len(buffer) == 0 {
			return 0, nil
		}
		read, err := b.ReadCloser.Read(buffer[:1])
		if read == 0 {
			return 0, err
		}
		b.exceeded = true
		b.onFailure()
		return 0, errProviderResponseTooLarge
	}
	if int64(len(buffer)) > b.remaining {
		buffer = buffer[:b.remaining]
	}

	read, err := b.ReadCloser.Read(buffer)
	b.remaining -= int64(read)
	return read, err
}
