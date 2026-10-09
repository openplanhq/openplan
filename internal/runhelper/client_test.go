package runhelper

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vishu42/openplan/internal/runuser"
)

// TestHelperMain is not a test. It is the helper that Client starts in these
// tests: this test binary, re-run after "--", standing in for the executor.
func TestHelperMain(t *testing.T) {
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) < 2 {
		return
	}
	if args[1] == "print-environment" {
		fmt.Print(strings.Join(os.Environ(), "\x00"))
		os.Exit(0)
	}
	os.Exit(Main(args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func testClient() Client {
	return Client{Command: []string{os.Args[0], "-test.run=^TestHelperMain$", "--"}}
}

func TestClientPacksAndUnpacksAsTheUser(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	user := runuser.DevelopmentUsers(1)[0]
	from, to := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(from, "tfplan"), []byte("the plan"), 0o600); err != nil {
		t.Fatal(err)
	}

	bundle, err := testClient().Pack(ctx, user, from)
	if err != nil {
		t.Fatal(err)
	}
	if err := testClient().Unpack(ctx, user, to, bundle); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(to, "tfplan")); string(got) != "the plan" {
		t.Fatalf("plan = %q", got)
	}
	if err := testClient().CheckRoot(ctx, user, filepath.Join(from, "missing")); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("CheckRoot = %v", err)
	}
}

// The helper runs as a pool user, so any process of that user can read its
// /proc/<pid>/environ. It must start with nothing from the executor's.
func TestClientStartsTheHelperWithAnEmptyEnvironment(t *testing.T) {
	t.Setenv("OPENPLAN_TEST_CANARY", "leaked")

	var stdout bytes.Buffer
	if err := testClient().run(context.Background(), runuser.DevelopmentUsers(1)[0], nil, &stdout, "print-environment"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "OPENPLAN_TEST_CANARY") {
		t.Fatalf("helper environment = %q, want empty", stdout.String())
	}
}
