package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vishu42/openplan/internal/runuser"
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

// TestHelperProcess is not a test. It is the child the environment tests start
// through osExecCommandExecutor, so they observe what a real subprocess
// receives rather than what cmd.Env was set to: the leak in #172 included Go
// filling in a nil cmd.Env, which no inspection of cmd.Env would catch.
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

type recordingKill struct {
	mu    sync.Mutex
	users []runuser.User
}

func (k *recordingKill) kill(_ context.Context, user runuser.User) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.users = append(k.users, user)
	return nil
}

func testBranch(t *testing.T) runuser.Branch {
	home := t.TempDir()
	return runuser.Branch{User: runuser.DevelopmentUsers(1)[0], Home: home, TempDir: t.TempDir()}
}

// On the executor every subprocess belongs to a session. One outside a branch
// would run as root, with root's three capabilities.
func TestIsolatedExecutorRefusesSubprocessesOutsideABranch(t *testing.T) {
	t.Parallel()
	err := NewIsolatedExecutor((&recordingKill{}).kill).Run(context.Background(), "", nil, io.Discard, io.Discard,
		os.Args[0], helperArgs(printEnvironmentCommand)...)
	if !errors.Is(err, ErrNoBranch) {
		t.Fatalf("Run = %v, want ErrNoBranch", err)
	}
}

func TestIsolatedExecutorGivesTheBranchItsHomeAndTempDir(t *testing.T) {
	t.Parallel()
	branch := testBranch(t)
	ctx := runuser.WithBranch(context.Background(), branch)
	executor := NewIsolatedExecutor((&recordingKill{}).kill)

	var env, dir bytes.Buffer
	if err := executor.Run(ctx, "", nil, &env, io.Discard, os.Args[0], helperArgs(printEnvironmentCommand)...); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(env.String(), "HOME="+branch.Home+"\x00") || !strings.Contains(env.String(), "TMPDIR="+branch.TempDir+"\x00") {
		t.Fatalf("environment = %q, want the branch's HOME and TMPDIR", env.String())
	}
	if err := executor.Run(ctx, "", nil, &dir, io.Discard, os.Args[0], helperArgs(printWorkingDirectoryCommand)...); err != nil {
		t.Fatal(err)
	}
	got, _ := filepath.EvalSymlinks(dir.String())
	want, _ := filepath.EvalSymlinks(branch.Home)
	if got != want {
		t.Fatalf("working directory = %q, want the branch's home %q", got, want)
	}
}

// A command's leftovers (nohup … &) die with it, success or failure.
func TestIsolatedExecutorKillsTheBranchAfterEachCommand(t *testing.T) {
	t.Parallel()
	branch := testBranch(t)
	kill := &recordingKill{}

	err := NewIsolatedExecutor(kill.kill).Run(runuser.WithBranch(context.Background(), branch), "", nil, io.Discard, io.Discard,
		os.Args[0], helperArgs(printEnvironmentCommand)...)
	if err != nil {
		t.Fatal(err)
	}
	if len(kill.users) != 1 || kill.users[0] != branch.User {
		t.Fatalf("killed = %v, want %v once", kill.users, branch.User)
	}
}

func TestIsolatedExecutorKillsTheBranchOnCancel(t *testing.T) {
	t.Parallel()
	branch := testBranch(t)
	kill := &recordingKill{}
	executor := osExecCommandExecutor{isolated: true, kill: kill.kill, waitDelay: 100 * time.Millisecond}
	ctx, cancel := context.WithTimeout(runuser.WithBranch(context.Background(), branch), 200*time.Millisecond)
	defer cancel()

	err := executor.Run(ctx, "", nil, io.Discard, io.Discard, os.Args[0], helperArgs(sleepCommand)...)
	if err == nil {
		t.Fatal("Run returned nil for a cancelled command")
	}
	// Once by Cancel, once after the command ended.
	if len(kill.users) == 0 || kill.users[0] != branch.User {
		t.Fatalf("killed = %v, want %v", kill.users, branch.User)
	}
}
