package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/vishu42/openplan/internal/authentication"
	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/encryption"
	"github.com/vishu42/openplan/internal/strval"
)

// DefaultOpenFGAStoreName is the store the embedded server adopts when
// OPENFGA_STORE_NAME names none. Bootstrap reconciles against this name, so it
// is configuration rather than an identifier anyone has to record.
const DefaultOpenFGAStoreName = "openplan"

type RuntimeMode string

const (
	RuntimeDevelopment RuntimeMode = "development"
	RuntimeProduction  RuntimeMode = "production"
)

type SecurityConfig struct {
	Mode     RuntimeMode
	TenantID domain.TenantID
	// OIDC is the identity provider, and the only way in: openplan holds no
	// credentials of its own.
	OIDC OIDCConfig
	// Root is the identity a fresh install is administered from. It is not
	// optional: seeding is what makes an install administrable at all, since
	// granting admin requires already being one.
	Root RootConfig

	PublicURL            *url.URL
	SessionEncryptionKey Secret
	SessionAbsoluteTTL   time.Duration
	SessionIdleTTL       time.Duration

	OpenFGA OpenFGAConfig
}

// RootConfig is the seeded bootstrap administrator.
type RootConfig struct {
	// Subject is the OIDC sub the identity provider issues for root. openplan
	// seeds only its root tuple; the identity itself lives in the provider.
	Subject string
}

type OIDCConfig struct {
	IssuerURL    *url.URL
	ClientID     string
	ClientSecret Secret
}

// OpenFGAConfig is what is left once OpenFGA runs in process: the name of the
// store to adopt, and nothing else.
type OpenFGAConfig struct {
	StoreName string
}

type Secret struct {
	value string
}

func newSecret(value string) Secret {
	return Secret{value: value}
}

func (secret Secret) Empty() bool {
	return secret.value == ""
}

func (secret Secret) Value() string {
	return secret.value
}

func (Secret) String() string {
	return "[REDACTED]"
}

func (Secret) GoString() string {
	return "[REDACTED]"
}

func (cfg OpenFGAConfig) String() string {
	return fmt.Sprintf("OpenFGAConfig{StoreName:%q}", cfg.StoreName)
}

func (cfg OpenFGAConfig) GoString() string {
	return cfg.String()
}

func (cfg SecurityConfig) String() string {
	return fmt.Sprintf(
		"SecurityConfig{Mode:%q TenantID:%q OIDC:{IssuerURL:%v ClientID:%q ClientSecret:%s} Root:{Subject:%q} PublicURL:%v SessionEncryptionKey:%s SessionAbsoluteTTL:%s SessionIdleTTL:%s OpenFGA:%s}",
		cfg.Mode,
		cfg.TenantID,
		cfg.OIDC.IssuerURL,
		cfg.OIDC.ClientID,
		cfg.OIDC.ClientSecret,
		cfg.Root.Subject,
		cfg.PublicURL,
		cfg.SessionEncryptionKey,
		cfg.SessionAbsoluteTTL,
		cfg.SessionIdleTTL,
		cfg.OpenFGA,
	)
}

func (cfg SecurityConfig) GoString() string {
	return cfg.String()
}

func loadSecurityConfig(getenv func(string) string) (SecurityConfig, error) {
	mode, err := parseRuntimeMode(getenv("OPENPLAN_ENVIRONMENT"))
	if err != nil {
		return SecurityConfig{}, err
	}

	tenantID := strings.TrimSpace(getenv("OPENPLAN_TENANT_ID"))
	if tenantID == "" {
		return SecurityConfig{}, authConfigError("OPENPLAN_TENANT_ID is required")
	}
	if !validTenantID(tenantID) {
		return SecurityConfig{}, authConfigError("OPENPLAN_TENANT_ID must start with an ASCII alphanumeric character, contain only ASCII alphanumerics, underscore, or hyphen, and be at most 128 characters")
	}

	oidc, err := loadOIDCConfig(getenv)
	if err != nil {
		return SecurityConfig{}, err
	}

	root, err := loadRootConfig(getenv)
	if err != nil {
		return SecurityConfig{}, err
	}

	publicURL, err := parseConfigURL("OPENPLAN_PUBLIC_URL", getenv("OPENPLAN_PUBLIC_URL"))
	if err != nil {
		return SecurityConfig{}, err
	}
	publicURL.Path = strings.TrimRight(publicURL.Path, "/")

	sessionKey := newSecret(strings.TrimSpace(getenv("SESSION_ENCRYPTION_KEY")))
	if sessionKey.Empty() {
		return SecurityConfig{}, authConfigError("SESSION_ENCRYPTION_KEY is required")
	}
	if _, err := encryption.NewCipher(sessionKey.Value()); err != nil {
		return SecurityConfig{}, authConfigError("SESSION_ENCRYPTION_KEY must be a 32-byte raw, base64, or hex key")
	}

	sessionAbsoluteTTL, err := optionalPositiveDuration(getenv, "OPENPLAN_SESSION_ABSOLUTE_TTL", authentication.DefaultSessionAbsoluteTTL)
	if err != nil {
		return SecurityConfig{}, err
	}
	sessionIdleTTL, err := optionalPositiveDuration(getenv, "OPENPLAN_SESSION_IDLE_TTL", authentication.DefaultSessionIdleTTL)
	if err != nil {
		return SecurityConfig{}, err
	}
	// An idle bound past the absolute cap can never be reached, so it is a
	// configuration mistake rather than a permissive setting.
	if sessionIdleTTL > sessionAbsoluteTTL {
		return SecurityConfig{}, authConfigError("OPENPLAN_SESSION_IDLE_TTL must not exceed OPENPLAN_SESSION_ABSOLUTE_TTL")
	}
	// The cookie path only writes LastSeenAt back once every
	// authentication.SessionTouchInterval (authentication/middleware.go), and IsLive is
	// evaluated before that write on every request. An idle bound at or below
	// the interval expires the session before it can ever be observed as
	// touched, so it can never slide — a silent hard cap rather than the
	// sliding window the setting promises.
	if sessionIdleTTL <= authentication.SessionTouchInterval {
		return SecurityConfig{}, authConfigError("OPENPLAN_SESSION_IDLE_TTL must exceed the %s session touch interval, or an idle session expires before it can ever slide", authentication.SessionTouchInterval)
	}

	openFGA, err := loadOpenFGAConfig(getenv)
	if err != nil {
		return SecurityConfig{}, err
	}
	if mode == RuntimeProduction {
		if oidc.IssuerURL.Scheme != "https" {
			return SecurityConfig{}, authConfigError("OIDC_ISSUER_URL must use HTTPS in production")
		}
		if publicURL.Scheme != "https" {
			return SecurityConfig{}, authConfigError("OPENPLAN_PUBLIC_URL must use HTTPS in production")
		}
	}

	// No provider-specific configuration is read here. Identity reaches the
	// API as an OIDC token from whatever provider OIDC_ISSUER_URL names, and
	// display names come from the local projection rather than a vendor admin
	// API.

	return SecurityConfig{
		Mode:     mode,
		TenantID: domain.TenantID(tenantID),
		OIDC:     oidc,
		Root:     root,

		PublicURL:            publicURL,
		SessionEncryptionKey: sessionKey,
		SessionAbsoluteTTL:   sessionAbsoluteTTL,
		SessionIdleTTL:       sessionIdleTTL,

		OpenFGA: openFGA,
	}, nil
}

// loadRootConfig reads the seeded bootstrap administrator.
//
// A sub, never an email or a username. An upstream connector can assert any
// email address, while the sub Dex issues is derived from the connector as
// well as the user, so no other connector can produce it.
//
// The retired local-account settings are refused by name. openplan holds no
// passwords now, and a root password left in an environment would be a secret
// that protects nothing while looking as if it does.
func loadRootConfig(getenv func(string) string) (RootConfig, error) {
	for _, retired := range []string{"OPENPLAN_ROOT_PASSWORD", "OPENPLAN_ROOT_USERNAME"} {
		if getenv(retired) != "" {
			return RootConfig{}, authConfigError("%s is retired: root is an identity-provider user, named by OPENPLAN_ROOT_SUBJECT", retired)
		}
	}

	subject := strings.TrimSpace(getenv("OPENPLAN_ROOT_SUBJECT"))
	if subject == "" {
		return RootConfig{}, authConfigError("OPENPLAN_ROOT_SUBJECT is required: it names the identity-provider user a fresh install is administered from")
	}
	if !strval.SafeOpaque(subject) {
		return RootConfig{}, authConfigError("OPENPLAN_ROOT_SUBJECT must not contain whitespace or control characters")
	}
	return RootConfig{Subject: subject}, nil
}

// loadOIDCConfig reads the identity provider. All three settings are required:
// the provider is the only way in.
func loadOIDCConfig(getenv func(string) string) (OIDCConfig, error) {
	if strings.TrimSpace(getenv("OIDC_AUDIENCE")) != "" {
		return OIDCConfig{}, authConfigError("OIDC_AUDIENCE is retired: set OIDC_CLIENT_ID to the OAuth client ID instead")
	}

	rawIssuer := strings.TrimSpace(getenv("OIDC_ISSUER_URL"))
	clientID := strings.TrimSpace(getenv("OIDC_CLIENT_ID"))
	clientSecret := newSecret(strings.TrimSpace(getenv("OIDC_CLIENT_SECRET")))

	issuerURL, err := parseConfigURL("OIDC_ISSUER_URL", rawIssuer)
	if err != nil {
		return OIDCConfig{}, err
	}
	if clientID == "" {
		return OIDCConfig{}, authConfigError("OIDC_CLIENT_ID is required")
	}
	if !strval.SafeOpaque(clientID) {
		return OIDCConfig{}, authConfigError("OIDC_CLIENT_ID must not contain whitespace or control characters")
	}
	if clientSecret.Empty() {
		return OIDCConfig{}, authConfigError("OIDC_CLIENT_SECRET is required")
	}
	return OIDCConfig{IssuerURL: issuerURL, ClientID: clientID, ClientSecret: clientSecret}, nil
}

// loadOpenFGAConfig reads the one setting an embedded OpenFGA still has.
//
// OPENFGA_API_URL, OPENFGA_STORE_ID, OPENFGA_MODEL_ID, OPENFGA_API_TOKEN and
// OPENFGA_HTTP_TIMEOUT are all gone. There is no service to address, no token
// to present, and no timeout to tune; the store and model are resolved in
// process from the model in this repository, so there is nothing for an
// operator to record between two startup phases and paste into an environment.
//
//	""        → StoreName "openplan"
//	"  acme " → StoreName "acme"
func loadOpenFGAConfig(getenv func(string) string) (OpenFGAConfig, error) {
	storeName := strings.TrimSpace(getenv("OPENFGA_STORE_NAME"))
	if storeName == "" {
		storeName = DefaultOpenFGAStoreName
	}
	if !strval.SafeOpaque(storeName) {
		return OpenFGAConfig{}, authConfigError("OPENFGA_STORE_NAME must not contain whitespace or control characters")
	}
	return OpenFGAConfig{StoreName: storeName}, nil
}

func parseRuntimeMode(raw string) (RuntimeMode, error) {
	switch strings.TrimSpace(raw) {
	case "", string(RuntimeDevelopment):
		return RuntimeDevelopment, nil
	case string(RuntimeProduction):
		return RuntimeProduction, nil
	default:
		return "", authConfigError("OPENPLAN_ENVIRONMENT must be development or production")
	}
}

func parseConfigURL(name, raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, authConfigError("%s is required", name)
	}
	return parseAbsoluteURL(name, raw)
}

func parseAbsoluteURL(name, raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, authConfigError("%s must be an absolute HTTP or HTTPS URL", name)
	}
	if parsed.User != nil {
		return nil, authConfigError("%s must not include user information", name)
	}
	if parsed.RawQuery != "" || parsed.ForceQuery {
		return nil, authConfigError("%s must not include a query", name)
	}
	if parsed.Fragment != "" || strings.Contains(raw, "#") {
		return nil, authConfigError("%s must not include a fragment", name)
	}
	return parsed, nil
}

func validTenantID(value string) bool {
	if value == "" || len(value) > 128 || !asciiAlphanumeric(value[0]) {
		return false
	}
	for index := 1; index < len(value); index++ {
		character := value[index]
		if !asciiAlphanumeric(character) && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func asciiAlphanumeric(character byte) bool {
	return character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9'
}

func optionalPositiveDuration(getenv func(string) string, name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, authConfigError("%s must be a positive duration", name)
	}
	return value, nil
}

func authConfigError(format string, arguments ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidConfig, fmt.Sprintf(format, arguments...))
}
