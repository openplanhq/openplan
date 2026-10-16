package runner

import (
	"bytes"
	"context"
	"io"
	"slices"
	"strings"
	"testing"
)

func TestProcessEnvironmentTakesOnlyTheAllowlist(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		"PATH":                 "/bin",
		"HTTPS_PROXY":          "http://proxy:3128",
		"S3_SECRET_ACCESS_KEY": "secret",
		"TF_VAR_region":        "eu-west-1",
	}
	lookup := func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}

	got := processEnvironment(lookup, []string{"AWS_ACCESS_KEY_ID=AKIA"})

	want := []string{"PATH=/bin", "HTTPS_PROXY=http://proxy:3128", "TF_IN_AUTOMATION=1", "AWS_ACCESS_KEY_ID=AKIA"}
	if !slices.Equal(got, want) {
		t.Fatalf("environment = %q, want %q", got, want)
	}
}

// The executor's own environment holds the artifact store's keys; a command
// must never see them.
func TestProcessExecutorRunsWithTheAllowlistedEnvironment(t *testing.T) {
	t.Setenv("S3_SECRET_ACCESS_KEY", "leak-me")

	var out bytes.Buffer
	err := ProcessExecutor{}.Run(context.Background(), t.TempDir(), []string{"EXTRA=1"}, &out, io.Discard, "/bin/sh", "-c", "env")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if strings.Contains(out.String(), "leak-me") {
		t.Fatalf("command saw the executor's secret: %s", out.String())
	}
	if !strings.Contains(out.String(), "EXTRA=1") {
		t.Fatalf("command did not get its own environment: %s", out.String())
	}
}
