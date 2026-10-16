package sandbox

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestBaseEnvironmentIsBuiltFromScratch(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		"HTTPS_PROXY":          "http://proxy:3128",
		"S3_SECRET_ACCESS_KEY": "secret",
		"TEMPORAL_ADDRESS":     "temporal:7233",
		"PATH":                 "/executor/only",
	}
	lookup := func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}

	want := []string{
		"PATH=" + guestPath, "HOME=/tmp", "TMPDIR=/tmp", "TF_IN_AUTOMATION=1",
		"HTTPS_PROXY=http://proxy:3128",
	}
	if got := baseEnvironment(lookup); !slices.Equal(got, want) {
		t.Fatalf("environment = %q, want %q", got, want)
	}
}

// SSL_CERT_FILE and SSL_CERT_DIR are forwarded as paths on the executor, which
// a sandbox's guest root doesn't have: each is mounted read-only at the path
// the variable names, or the CA it names would be silently ignored.
func TestTrustMountsCarryTheForwardedCAPaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "corp.pem")
	if err := os.WriteFile(file, []byte("-----BEGIN CERTIFICATE-----\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	certs, more := filepath.Join(dir, "certs"), filepath.Join(dir, "more")
	for _, d := range []string{certs, more} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	lookup := func(values map[string]string) func(string) (string, bool) {
		return func(name string) (string, bool) {
			value, ok := values[name]
			return value, ok
		}
	}

	mounts, err := trustMounts(lookup(map[string]string{"SSL_CERT_FILE": file, "SSL_CERT_DIR": certs + ":" + more + ":"}))
	if err != nil {
		t.Fatalf("trustMounts returned error: %v", err)
	}
	want := []Mount{{Path: file, ReadOnly: true}, {Path: certs, ReadOnly: true}, {Path: more, ReadOnly: true}}
	if !slices.Equal(mounts, want) {
		t.Fatalf("mounts = %+v, want %+v", mounts, want)
	}
	if mounts, err := trustMounts(lookup(nil)); err != nil || len(mounts) != 0 {
		t.Fatalf("nothing set: %+v, %v; want no mounts", mounts, err)
	}
	// Caught at startup, rather than as every sandbox's create failing.
	for _, bad := range []map[string]string{
		{"SSL_CERT_FILE": filepath.Join(dir, "missing.pem")},
		{"SSL_CERT_DIR": certs + ":relative/certs"},
	} {
		if _, err := trustMounts(lookup(bad)); err == nil {
			t.Fatalf("%v: accepted", bad)
		}
	}
}
