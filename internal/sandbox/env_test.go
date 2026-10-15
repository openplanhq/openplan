package sandbox

import (
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
