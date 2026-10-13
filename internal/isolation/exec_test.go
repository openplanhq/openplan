package isolation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The commands that turn this test binary into a child: one that prints its
// own environment, its working directory, or sleeps. They travel in argv
// rather than the usual GO_WANT_HELPER_PROCESS variable because the empty-env
// case has no environment to carry them.
const (
	printEnvironmentCommand      = "print-environment"
	printWorkingDirectoryCommand = "print-working-directory"
	sleepCommand                 = "sleep"
)

// TestHelperProcess is not a test. It is the child the executor tests start,
// so they observe what a real subprocess receives rather than what cmd.Env was
// set to: the leak in #172 included Go filling in a nil cmd.Env, which no
// inspection of cmd.Env would catch.
func TestHelperProcess(t *testing.T) {
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) != 2 {
		return
	}
	switch args[1] {
	case printEnvironmentCommand:
		// NUL-separated, so a value containing a newline cannot forge an entry.
		for _, entry := range os.Environ() {
			fmt.Print(entry, "\x00")
		}
	case printWorkingDirectoryCommand:
		dir, _ := os.Getwd()
		fmt.Print(dir)
	case sleepCommand:
		time.Sleep(time.Minute)
	default:
		return
	}
	// Exit before the testing framework prints PASS onto stdout.
	os.Exit(0)
}

func helperArgs(command string) []string {
	return []string{"-test.run=^TestHelperProcess$", "--", command}
}

// skipAsRoot skips tests that run sessions as the current user: as root that
// is uid 0, which credential refuses, as it should.
func skipAsRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("sessions run as the current user here, and root is not a pool user")
	}
}

// childEnvironment starts this test binary as a real subprocess through
// HostExecutor with env, and returns the environment the child saw.
func childEnvironment(t *testing.T, env []string) map[string]string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := HostExecutor().Run(context.Background(), "", env, &stdout, &stderr, os.Args[0], helperArgs(printEnvironmentCommand)...)
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
func TestExecutorWithholdsExecutorEnvironment(t *testing.T) {
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

func TestExecutorPassesAllowlistedEnvironment(t *testing.T) {
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
func TestExecutorLeavesUnsetAllowlistedVariablesUnset(t *testing.T) {
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
func TestExecutorPrefersExplicitEnvironment(t *testing.T) {
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

// On the executor every subprocess belongs to a session. One outside a
// session would run as root, with root's three capabilities.
// The zero Executor too: one built without a constructor fails closed.
func TestSessionExecutorRefusesSubprocessesOutsideASession(t *testing.T) {
	t.Parallel()
	for name, executor := range map[string]Executor{"SessionExecutor": SessionExecutor(), "the zero Executor": {}} {
		err := executor.Run(context.Background(), "", nil, io.Discard, io.Discard, os.Args[0], helperArgs(printEnvironmentCommand)...)
		if !errors.Is(err, ErrNoSession) {
			t.Errorf("%s: Run = %v, want ErrNoSession", name, err)
		}
	}
}

// A session closed while its command ran had its user's processes killed by
// the reclaim, and its user may be the next session's already: the kill after
// the command must not reach that session.
func TestSessionExecutorKillsNothingOnceTheSessionHasEnded(t *testing.T) {
	t.Parallel()
	helper := &recordingHelper{}
	m := newTestManager(t.TempDir(), 1, helper)
	s := openTestSession(t, m, "run")
	if err := m.Close(context.Background(), "t", "run"); err != nil {
		t.Fatal(err)
	}

	if err := SessionExecutor().kill(context.Background(), s); err != nil || len(helper.killedUsers()) != 0 {
		t.Fatalf("kill = %v, killed %v; want nothing killed", err, helper.killedUsers())
	}
}

func TestSessionExecutorRefusesAClosedSession(t *testing.T) {
	t.Parallel()
	helper := &recordingHelper{}
	m := newTestManager(t.TempDir(), 1, helper)
	s := openTestSession(t, m, "run")
	ctx := s.Context(context.Background())
	if err := m.Close(context.Background(), "t", "run"); err != nil {
		t.Fatal(err)
	}

	err := SessionExecutor().Run(ctx, "", nil, io.Discard, io.Discard, os.Args[0], helperArgs(printEnvironmentCommand)...)
	if !errors.Is(err, ErrSessionClosed) || len(helper.killedUsers()) != 0 {
		t.Fatalf("Run = %v, killed %v; want ErrSessionClosed and nothing started", err, helper.killedUsers())
	}
}

func TestSessionExecutorGivesTheSessionItsHomeAndTempDir(t *testing.T) {
	t.Parallel()
	skipAsRoot(t)
	m := newTestManager(t.TempDir(), 1, &recordingHelper{})
	s := openTestSession(t, m, "run")
	ctx := s.Context(context.Background())

	var env, dir bytes.Buffer
	if err := SessionExecutor().Run(ctx, "", nil, &env, io.Discard, os.Args[0], helperArgs(printEnvironmentCommand)...); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(env.String(), "HOME="+s.home()+"\x00") || !strings.Contains(env.String(), "TMPDIR="+s.tempDir()+"\x00") {
		t.Fatalf("environment = %q, want the session's HOME and TMPDIR", env.String())
	}
	if err := SessionExecutor().Run(ctx, "", nil, &dir, io.Discard, os.Args[0], helperArgs(printWorkingDirectoryCommand)...); err != nil {
		t.Fatal(err)
	}
	got, _ := filepath.EvalSymlinks(dir.String())
	want, _ := filepath.EvalSymlinks(s.home())
	if got != want {
		t.Fatalf("working directory = %q, want the session's home %q", got, want)
	}
}

// A command's leftovers (nohup … &) die with it, success or failure.
func TestSessionExecutorKillsTheSessionAfterEachCommand(t *testing.T) {
	t.Parallel()
	skipAsRoot(t)
	helper := &recordingHelper{}
	m := newTestManager(t.TempDir(), 1, helper)
	s := openTestSession(t, m, "run")

	err := SessionExecutor().Run(s.Context(context.Background()), "", nil, io.Discard, io.Discard, os.Args[0], helperArgs(printEnvironmentCommand)...)
	if err != nil {
		t.Fatal(err)
	}
	if killed := helper.killedUsers(); len(killed) != 1 || killed[0] != s.user {
		t.Fatalf("killed = %v, want %v once", killed, s.user)
	}
}

func TestSessionExecutorKillsTheSessionOnCancel(t *testing.T) {
	t.Parallel()
	skipAsRoot(t)
	helper := &recordingHelper{}
	m := newTestManager(t.TempDir(), 1, helper)
	s := openTestSession(t, m, "run")
	executor := Executor{waitDelay: 100 * time.Millisecond}
	ctx, cancel := context.WithTimeout(s.Context(context.Background()), 200*time.Millisecond)
	defer cancel()

	err := executor.Run(ctx, "", nil, io.Discard, io.Discard, os.Args[0], helperArgs(sleepCommand)...)
	if err == nil {
		t.Fatal("Run returned nil for a cancelled command")
	}
	// Once by Cancel, once after the command ended.
	if killed := helper.killedUsers(); len(killed) == 0 || killed[0] != s.user {
		t.Fatalf("killed = %v, want %v", killed, s.user)
	}
}
