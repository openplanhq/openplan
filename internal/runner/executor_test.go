package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

// printEnvironmentCommand turns this test binary into a child that prints its
// own environment. It travels in argv rather than the usual
// GO_WANT_HELPER_PROCESS variable because the empty-env case has no
// environment to carry it.
const printEnvironmentCommand = "print-environment"

// TestHelperProcess is not a test. It is the child the environment tests start
// through osExecCommandExecutor, so they observe what a real subprocess
// receives rather than what cmd.Env was set to: the leak in #172 included Go
// filling in a nil cmd.Env, which no inspection of cmd.Env would catch.
func TestHelperProcess(t *testing.T) {
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) != 2 || args[1] != printEnvironmentCommand {
		return
	}
	// NUL-separated, so a value containing a newline cannot forge an entry.
	for _, entry := range os.Environ() {
		fmt.Print(entry, "\x00")
	}
	// Exit before the testing framework prints PASS onto stdout.
	os.Exit(0)
}

// childEnvironment starts this test binary as a real subprocess through
// osExecCommandExecutor with env, and returns the environment the child saw.
func childEnvironment(t *testing.T, env []string) map[string]string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := osExecCommandExecutor{}.Run(context.Background(), "", env, &stdout, &stderr,
		os.Args[0], "-test.run=^TestHelperProcess$", "--", printEnvironmentCommand)
	if err != nil {
		t.Fatalf("helper process: %v\nstderr: %s", err, stderr.String())
	}
	environment := map[string]string{}
	for entry := range strings.SplitSeq(stdout.String(), "\x00") {
		if entry == "" {
			continue
		}
		name, value, _ := strings.Cut(entry, "=")
		environment[name] = value
	}
	return environment
}

// The executor's environment holds the artifact store's write credentials and
// the Temporal address, and the subprocess runs template code that can print
// whatever it receives into its run log. Probing with a canary rather than
// naming those variables pins the property: a variable added to the executor
// later stays out without anyone remembering to list it here.
func TestOSExecCommandExecutorWithholdsExecutorEnvironment(t *testing.T) {
	const canary = "OPENPLAN_TEST_CANARY"
	t.Setenv(canary, "leaked")

	cases := []struct {
		name string
		env  []string
	}{
		{name: "explicit env", env: []string{"RUN_CREDENTIAL=value"}},
		// os/exec gives a nil cmd.Env the parent's whole environment, so a run
		// with no credentials and no variables leaks as much as any other.
		{name: "empty env", env: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if value, ok := childEnvironment(t, tc.env)[canary]; ok {
				t.Fatalf("child received %s=%q from the executor's environment", canary, value)
			}
		})
	}
}

func TestOSExecCommandExecutorPassesAllowlistedEnvironment(t *testing.T) {
	for _, name := range executorEnvironmentAllowlist {
		t.Setenv(name, "from-executor-"+name)
	}

	child := childEnvironment(t, nil)

	for _, name := range executorEnvironmentAllowlist {
		if got, want := child[name], "from-executor-"+name; got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if got := child["TF_IN_AUTOMATION"]; got != "1" {
		t.Errorf("TF_IN_AUTOMATION = %q, want %q", got, "1")
	}
}

// An allowlisted variable the executor does not set must stay unset in the
// child, not arrive empty: some readers treat the two differently.
func TestOSExecCommandExecutorLeavesUnsetAllowlistedVariablesUnset(t *testing.T) {
	name := executorEnvironmentAllowlist[len(executorEnvironmentAllowlist)-1]
	t.Setenv(name, "")
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}

	if value, ok := childEnvironment(t, nil)[name]; ok {
		t.Fatalf("child received %s=%q, want it unset", name, value)
	}
}

// A run's credentials and TF_VAR_* arrive through env and must reach tofu as
// given, even when the executor sets the same name.
func TestOSExecCommandExecutorPrefersExplicitEnvironment(t *testing.T) {
	name := executorEnvironmentAllowlist[0]
	t.Setenv(name, "from-executor")

	child := childEnvironment(t, []string{name + "=from-call", "TF_VAR_region=eu-west-1"})

	if got := child[name]; got != "from-call" {
		t.Errorf("%s = %q, want the explicit value %q", name, got, "from-call")
	}
	if got := child["TF_VAR_region"]; got != "eu-west-1" {
		t.Errorf("TF_VAR_region = %q, want %q", got, "eu-west-1")
	}
}
