package config

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const (
	publishedCredentialKey = "717cb4d0fd1db07a30442806c2987599580f6d7c6e63b9bddf509bc183a086d3"
	publishedSessionKey    = "b6f1d2c4a8e0937516243b8c5d7e9f0a1b2c3d4e5f60718293a4b5c6d7e8f9a0"
	// A fresh key, as `openssl rand -hex 32` would print one.
	freshKey = "3b9f6c1e0a8d47f2b5c9e1d3a7f04b6c8e2d1f9a3c5b7e0d4f6a8c2e1b3d5f70"
)

func productionAPIValues() map[string]string {
	values := validSecurityValues()
	values["OPENPLAN_ENVIRONMENT"] = "production"
	values["OPENPLAN_PUBLIC_URL"] = "https://app.example.com"
	values["OIDC_ISSUER_URL"] = "https://id.example.com/realms/openplan"
	values["POSTGRES_URL"] = "postgres://user:pass@localhost:5432/db?sslmode=disable"
	values["TEMPORAL_ADDRESS"] = "localhost:7233"
	values["VALKEY_URL"] = "redis://localhost:6379"
	values["SESSION_ENCRYPTION_KEY"] = freshKey
	values["CREDENTIAL_ENCRYPTION_KEY"] = freshKey
	return values
}

// reencodeBase64 writes the same key bytes as unpadded base64, the other
// encoding the cipher accepts.
func reencodeBase64(t *testing.T, hexKey string) string {
	t.Helper()
	raw, err := hex.DecodeString(hexKey)
	if err != nil {
		t.Fatalf("decode %q: %v", hexKey, err)
	}
	return base64.RawStdEncoding.EncodeToString(raw)
}

func TestLoadAPIConfigRefusesPublishedKeysInProduction(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"SESSION_ENCRYPTION_KEY", "CREDENTIAL_ENCRYPTION_KEY"} {
		for _, test := range []struct {
			label string
			value string
		}{
			{label: "credential key", value: publishedCredentialKey},
			// The two published keys are refused in either variable: swapping
			// them is no better than using them.
			{label: "session key", value: publishedSessionKey},
			// The comparison is on the key, not its spelling.
			{label: "upper-case hex", value: strings.ToUpper(publishedCredentialKey)},
			{label: "base64", value: reencodeBase64(t, publishedSessionKey)},
		} {
			t.Run(name+"/"+test.label, func(t *testing.T) {
				t.Parallel()

				values := productionAPIValues()
				values[name] = test.value
				_, err := LoadAPIConfig(mapConfigEnv(values))
				if !errors.Is(err, ErrInvalidConfig) {
					t.Fatalf("error = %v, want ErrInvalidConfig", err)
				}
				for _, want := range []string{name, "openssl rand -hex 32"} {
					if !strings.Contains(err.Error(), want) {
						t.Fatalf("error = %q, want it to contain %q", err, want)
					}
				}
			})
		}
	}
}

func TestLoadAPIConfigAcceptsFreshKeysInProduction(t *testing.T) {
	t.Parallel()

	if _, err := LoadAPIConfig(mapConfigEnv(productionAPIValues())); err != nil {
		t.Fatalf("LoadAPIConfig returned error: %v", err)
	}
}

// Development keeps working with the published keys: they are what the
// evaluation stack and a fresh checkout run on.
func TestLoadAPIConfigAcceptsPublishedKeysInDevelopment(t *testing.T) {
	t.Parallel()

	values := productionAPIValues()
	values["OPENPLAN_ENVIRONMENT"] = "development"
	values["SESSION_ENCRYPTION_KEY"] = publishedSessionKey
	values["CREDENTIAL_ENCRYPTION_KEY"] = publishedCredentialKey
	if _, err := LoadAPIConfig(mapConfigEnv(values)); err != nil {
		t.Fatalf("LoadAPIConfig returned error: %v", err)
	}
}

// The refusal is only as good as its list. Every encryption key the
// repository ships must be on it, so changing an example key without adding
// it fails here instead of quietly letting the new one through.
func TestPublishedEncryptionKeysCoverTheRepository(t *testing.T) {
	t.Parallel()

	// Matches `NAME=value`, `NAME: value` and `NAME: ${NAME:-value}`.
	keyPattern := regexp.MustCompile(`[A-Z_]+_ENCRYPTION_KEY[=:]\s*(?:\$\{[A-Z_]+:-)?([0-9A-Za-z+/=]{32,})`)
	for _, file := range []string{".env.example", "docker-compose.yaml", "docker-compose.release.yaml"} {
		content, err := os.ReadFile(filepath.Join("..", "..", file))
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		matches := keyPattern.FindAllStringSubmatch(string(content), -1)
		if len(matches) == 0 {
			t.Fatalf("%s: found no encryption keys; has the file moved or its format changed?", file)
		}
		for _, match := range matches {
			if !slices.Contains(publishedEncryptionKeys, match[1]) {
				t.Errorf("%s ships encryption key %s, which publishedEncryptionKeys does not list", file, match[1])
			}
		}
	}
}

func TestLoadAPIConfigRefusesPublishedValkeyPasswordsInProduction(t *testing.T) {
	t.Parallel()

	for _, password := range []string{"api-local-only", "executor-local-only"} {
		t.Run(password, func(t *testing.T) {
			t.Parallel()

			values := productionAPIValues()
			values["VALKEY_URL"] = "redis://api:" + password + "@valkey:6379/0"
			_, err := LoadAPIConfig(mapConfigEnv(values))
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("error = %v, want ErrInvalidConfig", err)
			}
			if !strings.Contains(err.Error(), "VALKEY_URL") {
				t.Fatalf("error = %q, want it to name VALKEY_URL", err)
			}
			if strings.Contains(err.Error(), password) {
				t.Fatalf("error = %q quotes the password", err)
			}
		})
	}
}

func TestLoadAPIConfigAcceptsAFreshValkeyPasswordInProduction(t *testing.T) {
	t.Parallel()

	values := productionAPIValues()
	values["VALKEY_URL"] = "rediss://api:" + freshKey + "@valkey:6379/0"
	if _, err := LoadAPIConfig(mapConfigEnv(values)); err != nil {
		t.Fatalf("LoadAPIConfig returned error: %v", err)
	}
}

func TestLoadAPIConfigAcceptsPublishedValkeyPasswordsInDevelopment(t *testing.T) {
	t.Parallel()

	values := productionAPIValues()
	values["OPENPLAN_ENVIRONMENT"] = "development"
	values["VALKEY_URL"] = "redis://api:api-local-only@localhost:56379/0"
	if _, err := LoadAPIConfig(mapConfigEnv(values)); err != nil {
		t.Fatalf("LoadAPIConfig returned error: %v", err)
	}
}

// As for the encryption keys: every Valkey password the repository ships
// must be on the list the production guard refuses.
func TestPublishedValkeyPasswordsCoverTheRepository(t *testing.T) {
	t.Parallel()

	// Matches `VALKEY_API_PASSWORD=value` and `${VALKEY_API_PASSWORD:-value}`.
	passwordPattern := regexp.MustCompile(`VALKEY_(?:API|EXECUTOR)_PASSWORD(?:=|:-)([^}\s"]+)`)
	for _, file := range []string{".env.example", "docker-compose.yaml", "docker-compose.release.yaml"} {
		content, err := os.ReadFile(filepath.Join("..", "..", file))
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		matches := passwordPattern.FindAllStringSubmatch(string(content), -1)
		if len(matches) == 0 {
			t.Fatalf("%s: found no Valkey passwords; has the file moved or its format changed?", file)
		}
		for _, match := range matches {
			if !slices.Contains(publishedValkeyPasswords, match[1]) {
				t.Errorf("%s ships Valkey password %s, which publishedValkeyPasswords does not list", file, match[1])
			}
		}
	}
}
