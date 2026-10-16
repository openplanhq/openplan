package config

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/encryption"
	"github.com/vishu42/openplan/internal/githubapp"
)

const (
	DefaultHTTPAddress                 = ":8081"
	DefaultExecutorRunRoot             = "/tmp/openplan/runs"
	DefaultArtifactStoreFilesystemRoot = "/tmp/openplan/artifacts"
	DefaultExecutorMaxSessions         = 20
	DefaultExecutorSandboxRunsc        = "/usr/local/bin/runsc"
	DefaultExecutorSandboxState        = "/var/lib/openplan/sandboxes"
	DefaultExecutorSandboxMemoryMiB    = 2048
	DefaultExecutorSandboxCPUs         = 2
	DefaultExecutorSandboxPids         = 1024
	// DefaultExecutorSandboxSubnet is private and small, to stay clear of
	// the networks the executor itself reaches; EXECUTOR_SANDBOX_SUBNET
	// moves it where it collides.
	DefaultExecutorSandboxSubnet = "10.237.0.0/24"
)

var ErrInvalidConfig = errors.New("invalid config")

type APIConfig struct {
	DatabaseURL             string
	HTTPAddress             string
	TemporalAddress         string
	TemporalNamespace       string
	ArtifactStore           ArtifactStoreConfig
	Security                SecurityConfig
	CredentialEncryptionKey Secret
	// GitHubApp mints installation tokens for template sync, which runs on the
	// control plane.
	GitHubApp GitHubAppConfig
	// TerraformTimeout bounds each Terraform command a run issues. The control
	// plane owns it because the control plane is what schedules the command and
	// tells Temporal how long to wait; the executor never sees the value.
	TerraformTimeout time.Duration
	Debug            bool
}

// ExecutorConfig is the whole of the executor's configuration. It runs next to
// tenant Terraform, so it carries no database URL and no key: anything added
// here is readable by every template that runs.
type ExecutorConfig struct {
	TemporalAddress   string
	TemporalNamespace string
	// RunRoot is where each run's workspace is checked out.
	RunRoot       string
	ArtifactStore ArtifactStoreConfig
	// MaxSessions caps the sessions, and so the runs, this executor holds at
	// once. Each runs its activities in sandboxes of their own.
	MaxSessions int
	Sandbox     SandboxConfig
}

// SandboxConfig is how the executor starts each activity's gVisor sandbox
// (#245).
type SandboxConfig struct {
	// Runsc is the runsc binary, with its gvisor-bin/ sidecars beside it.
	Runsc string
	// State holds runsc's state and the sandboxes' bundles. It must be
	// ephemeral, as the run root is.
	State string
	// MemoryMiB, CPUs and Pids are each sandbox's limits.
	MemoryMiB int64
	CPUs      int64
	Pids      int64
	// Subnet holds the bridge's gateway and one address per running
	// sandbox. Destinations inside it are unreachable from sandboxes.
	Subnet netip.Prefix
}

type ArtifactStoreKind string

const (
	ArtifactStoreFilesystem ArtifactStoreKind = "filesystem"
	ArtifactStoreS3         ArtifactStoreKind = "s3"
)

type ArtifactStoreConfig struct {
	Kind           ArtifactStoreKind
	FilesystemRoot string
	S3             S3Config
}

type S3Config struct {
	Bucket          string
	Region          string
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	ForcePathStyle  bool
}

func LoadAPIConfig(getenv func(string) string) (APIConfig, error) {
	security, err := loadSecurityConfig(getenv)
	if err != nil {
		return APIConfig{}, err
	}
	artifactStore, err := loadArtifactStoreConfig(getenv)
	if err != nil {
		return APIConfig{}, err
	}

	cfg := APIConfig{
		DatabaseURL:       strings.TrimSpace(getenv("DATABASE_URL")),
		HTTPAddress:       strings.TrimSpace(getenv("HTTP_ADDRESS")),
		TemporalAddress:   strings.TrimSpace(getenv("TEMPORAL_ADDRESS")),
		TemporalNamespace: strings.TrimSpace(getenv("TEMPORAL_NAMESPACE")),
		ArtifactStore:     artifactStore,
		Security:          security,
	}
	if cfg.HTTPAddress == "" {
		cfg.HTTPAddress = DefaultHTTPAddress
	}
	cfg.Debug = parseBool(getenv("OPENPLAN_DEBUG"))
	credentialKey, err := loadCredentialEncryptionKey(getenv)
	if err != nil {
		return APIConfig{}, err
	}
	if security.Mode == RuntimeProduction {
		if err := refusePublishedKey("CREDENTIAL_ENCRYPTION_KEY", credentialKey); err != nil {
			return APIConfig{}, err
		}
	}
	cfg.CredentialEncryptionKey = credentialKey

	gitHubApp, err := loadGitHubAppConfig(getenv)
	if err != nil {
		return APIConfig{}, err
	}
	cfg.GitHubApp = gitHubApp

	terraformTimeout, err := loadTerraformTimeout(getenv)
	if err != nil {
		return APIConfig{}, err
	}
	cfg.TerraformTimeout = terraformTimeout

	if cfg.DatabaseURL == "" {
		return APIConfig{}, fmt.Errorf("%w: DATABASE_URL is required", ErrInvalidConfig)
	}
	if cfg.TemporalAddress == "" {
		return APIConfig{}, fmt.Errorf("%w: TEMPORAL_ADDRESS is required", ErrInvalidConfig)
	}

	return cfg, nil
}

func LoadExecutorConfig(getenv func(string) string) (ExecutorConfig, error) {
	artifactStore, err := loadArtifactStoreConfig(getenv)
	if err != nil {
		return ExecutorConfig{}, err
	}

	cfg := ExecutorConfig{
		TemporalAddress:   strings.TrimSpace(getenv("TEMPORAL_ADDRESS")),
		TemporalNamespace: strings.TrimSpace(getenv("TEMPORAL_NAMESPACE")),
		RunRoot:           strings.TrimSpace(getenv("EXECUTOR_RUN_ROOT")),
		ArtifactStore:     artifactStore,
	}
	if cfg.RunRoot == "" {
		cfg.RunRoot = DefaultExecutorRunRoot
	}
	cfg.MaxSessions = DefaultExecutorMaxSessions
	if raw := strings.TrimSpace(getenv("EXECUTOR_MAX_SESSIONS")); raw != "" {
		maxSessions, err := strconv.Atoi(raw)
		if err != nil || maxSessions < 1 {
			return ExecutorConfig{}, fmt.Errorf("%w: EXECUTOR_MAX_SESSIONS must be a positive integer, got %q", ErrInvalidConfig, raw)
		}
		cfg.MaxSessions = maxSessions
	}
	cfg.Sandbox = SandboxConfig{
		Runsc: DefaultExecutorSandboxRunsc, State: DefaultExecutorSandboxState,
		MemoryMiB: DefaultExecutorSandboxMemoryMiB, CPUs: DefaultExecutorSandboxCPUs, Pids: DefaultExecutorSandboxPids,
		Subnet: netip.MustParsePrefix(DefaultExecutorSandboxSubnet),
	}
	if raw := strings.TrimSpace(getenv("EXECUTOR_SANDBOX_SUBNET")); raw != "" {
		subnet, err := netip.ParsePrefix(raw)
		// /29 at most: the network, the gateway and broadcast leave five.
		if err != nil || !subnet.Addr().Is4() || subnet.Bits() > 29 {
			return ExecutorConfig{}, fmt.Errorf("%w: EXECUTOR_SANDBOX_SUBNET must be an IPv4 subnet of /29 or larger, got %q", ErrInvalidConfig, raw)
		}
		cfg.Sandbox.Subnet = subnet.Masked()
	}
	if v := strings.TrimSpace(getenv("EXECUTOR_RUNSC")); v != "" {
		cfg.Sandbox.Runsc = v
	}
	if v := strings.TrimSpace(getenv("EXECUTOR_SANDBOX_STATE")); v != "" {
		cfg.Sandbox.State = v
	}
	for _, setting := range []struct {
		name  string
		value *int64
	}{
		{"EXECUTOR_SANDBOX_MEMORY", &cfg.Sandbox.MemoryMiB},
		{"EXECUTOR_SANDBOX_CPUS", &cfg.Sandbox.CPUs},
		{"EXECUTOR_SANDBOX_PIDS", &cfg.Sandbox.Pids},
	} {
		raw := strings.TrimSpace(getenv(setting.name))
		if raw == "" {
			continue
		}
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 1 {
			return ExecutorConfig{}, fmt.Errorf("%w: %s must be a positive integer, got %q", ErrInvalidConfig, setting.name, raw)
		}
		*setting.value = parsed
	}
	if cfg.TemporalAddress == "" {
		return ExecutorConfig{}, fmt.Errorf("%w: TEMPORAL_ADDRESS is required", ErrInvalidConfig)
	}

	return cfg, nil
}

// loadTerraformTimeout reads OPENPLAN_TERRAFORM_TIMEOUT, the ceiling on a single
// Terraform command.
//
// The value is a Go duration ("90m", "1h30m") rather than a bare number,
// because a bare number here is the kind of setting where minutes and seconds
// are confused once and the mistake is found by an apply that dies. Anything
// unparseable or non-positive fails at startup: a deployment that meant to
// raise the ceiling should not silently fall back to the default it was trying
// to replace.
func loadTerraformTimeout(getenv func(string) string) (time.Duration, error) {
	value := strings.TrimSpace(getenv("OPENPLAN_TERRAFORM_TIMEOUT"))
	if value == "" {
		return domain.DefaultTerraformTimeout, nil
	}
	timeout, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%w: OPENPLAN_TERRAFORM_TIMEOUT must be a duration such as 45m or 2h", ErrInvalidConfig)
	}
	if timeout <= 0 {
		return 0, fmt.Errorf("%w: OPENPLAN_TERRAFORM_TIMEOUT must be greater than zero", ErrInvalidConfig)
	}
	return timeout, nil
}

// loadCredentialEncryptionKey validates CREDENTIAL_ENCRYPTION_KEY the same
// way SESSION_ENCRYPTION_KEY is validated: by constructing the cipher it
// backs. Unlike SESSION_ENCRYPTION_KEY, it is optional — an empty value means
// credential encryption is unavailable, not misconfigured — but a value that
// is present and malformed must fail at startup rather than at first use.
func loadCredentialEncryptionKey(getenv func(string) string) (Secret, error) {
	key := newSecret(strings.TrimSpace(getenv("CREDENTIAL_ENCRYPTION_KEY")))
	if key.Empty() {
		return key, nil
	}
	if _, err := encryption.NewCipher(key.Value()); err != nil {
		return Secret{}, fmt.Errorf("%w: CREDENTIAL_ENCRYPTION_KEY must be a 32-byte raw, base64, or hex key", ErrInvalidConfig)
	}
	return key, nil
}

// GitHubAppConfig identifies the GitHub App whose installations grant access to
// private template repositories.
//
// It lives on the API alone: the control plane mints installation tokens and
// seals them to the executor, which never holds the signing key.
type GitHubAppConfig struct {
	AppID      string
	PrivateKey Secret
}

// Enabled reports whether a GitHub App is configured. When it is not, source
// fetches stay unauthenticated and only public repositories are reachable.
func (cfg GitHubAppConfig) Enabled() bool {
	return cfg.AppID != "" && !cfg.PrivateKey.Empty()
}

// loadGitHubAppConfig validates GITHUB_APP_ID and GITHUB_APP_PRIVATE_KEY the
// same way loadCredentialEncryptionKey validates its key: by constructing the
// thing it backs, so a malformed value fails at startup rather than at the
// first clone.
//
// Both absent means the App is not configured, which is a supported deployment
// and not an error. Exactly one present is always a mistake -- and one that
// would otherwise surface much later as an unexplained authentication failure.
func loadGitHubAppConfig(getenv func(string) string) (GitHubAppConfig, error) {
	appID := strings.TrimSpace(getenv("GITHUB_APP_ID"))
	privateKey := newSecret(strings.TrimSpace(getenv("GITHUB_APP_PRIVATE_KEY")))

	if appID == "" && privateKey.Empty() {
		return GitHubAppConfig{}, nil
	}
	if appID == "" {
		return GitHubAppConfig{}, fmt.Errorf("%w: GITHUB_APP_ID is required when GITHUB_APP_PRIVATE_KEY is set", ErrInvalidConfig)
	}
	if privateKey.Empty() {
		return GitHubAppConfig{}, fmt.Errorf("%w: GITHUB_APP_PRIVATE_KEY is required when GITHUB_APP_ID is set", ErrInvalidConfig)
	}
	if _, err := strconv.ParseInt(appID, 10, 64); err != nil {
		return GitHubAppConfig{}, fmt.Errorf("%w: GITHUB_APP_ID must be the App's numeric id", ErrInvalidConfig)
	}
	if _, err := githubapp.ParsePrivateKey(privateKey.Value()); err != nil {
		return GitHubAppConfig{}, fmt.Errorf("%w: GITHUB_APP_PRIVATE_KEY must be an RSA private key as PEM or base64-encoded PEM", ErrInvalidConfig)
	}
	return GitHubAppConfig{AppID: appID, PrivateKey: privateKey}, nil
}

func loadArtifactStoreConfig(getenv func(string) string) (ArtifactStoreConfig, error) {
	kind := ArtifactStoreKind(strings.TrimSpace(getenv("ARTIFACT_STORE_KIND")))
	if kind == "" {
		kind = ArtifactStoreFilesystem
	}

	cfg := ArtifactStoreConfig{
		Kind:           kind,
		FilesystemRoot: strings.TrimSpace(getenv("ARTIFACT_STORE_FILESYSTEM_ROOT")),
		S3: S3Config{
			Bucket:          strings.TrimSpace(getenv("S3_BUCKET")),
			Region:          strings.TrimSpace(getenv("S3_REGION")),
			Endpoint:        strings.TrimSpace(getenv("S3_ENDPOINT")),
			AccessKeyID:     strings.TrimSpace(getenv("S3_ACCESS_KEY_ID")),
			SecretAccessKey: strings.TrimSpace(getenv("S3_SECRET_ACCESS_KEY")),
			ForcePathStyle:  parseBool(getenv("S3_FORCE_PATH_STYLE")),
		},
	}
	if cfg.FilesystemRoot == "" {
		cfg.FilesystemRoot = DefaultArtifactStoreFilesystemRoot
	}

	switch cfg.Kind {
	case ArtifactStoreFilesystem:
		return cfg, nil
	case ArtifactStoreS3:
		if cfg.S3.Bucket == "" {
			return ArtifactStoreConfig{}, fmt.Errorf("%w: S3_BUCKET is required", ErrInvalidConfig)
		}
		if cfg.S3.Region == "" {
			return ArtifactStoreConfig{}, fmt.Errorf("%w: S3_REGION is required", ErrInvalidConfig)
		}
		if cfg.S3.AccessKeyID == "" {
			return ArtifactStoreConfig{}, fmt.Errorf("%w: S3_ACCESS_KEY_ID is required", ErrInvalidConfig)
		}
		if cfg.S3.SecretAccessKey == "" {
			return ArtifactStoreConfig{}, fmt.Errorf("%w: S3_SECRET_ACCESS_KEY is required", ErrInvalidConfig)
		}
		return cfg, nil
	default:
		return ArtifactStoreConfig{}, fmt.Errorf("%w: ARTIFACT_STORE_KIND must be filesystem or s3", ErrInvalidConfig)
	}
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}
