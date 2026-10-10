package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/vishu42/openplan/internal/runuser"
)

// CommandExecutor is the subprocess boundary shared by LocalProcessRunner and
// LocalGitRunner.
//
// Implementations receive the fully resolved working directory, output streams,
// executable name, and CLI arguments. The production executor starts a real
// process with os/exec, while tests can provide a recorder or fake executor to
// verify command construction without invoking a subprocess.
type CommandExecutor interface {
	Run(ctx context.Context, dir string, env []string, stdout io.Writer, stderr io.Writer, name string, args ...string) error
}

// executorEnvironmentAllowlist names the only variables a subprocess takes from
// the executor's own environment. That environment holds the artifact store's
// write credentials and the Temporal address, and tofu runs template code,
// which is untrusted (#172). What a run needs arrives explicitly through Run's
// env instead. A sandbox wrapping tofu should take its environment from
// subprocessEnvironment too, so the two cannot drift.
//
// Names, not prefixes: TF_* would forward a TF_VAR_* or TF_CLI_ARGS set on the
// executor into every tenant's run.
var executorEnvironmentAllowlist = []string{
	// Finding executables, and the directories tofu and git read config from
	// and write scratch files to. In a session's branch HOME and TMPDIR are
	// the branch's own instead, inside its workspace (#331); these are for
	// subprocesses outside a branch, such as template sync on the api.
	"PATH",
	"HOME",
	"TMPDIR",
	// Corporate egress: provider and module downloads and git fetches go
	// through the executor's proxy and trust its CA. Both cases, because Go
	// reads either and curl reads only the lowercase http_proxy. A credential
	// embedded in a proxy URL reaches the template too.
	"HTTP_PROXY",
	"HTTPS_PROXY",
	"NO_PROXY",
	"http_proxy",
	"https_proxy",
	"no_proxy",
	"SSL_CERT_FILE",
	"SSL_CERT_DIR",
}

// subprocessEnvironment returns the whole environment for one subprocess: the
// allowlisted executor variables that are set, TF_IN_AUTOMATION, then env.
// env comes last so a caller's value wins; exec.Cmd keeps the last of duplicate
// names.
func subprocessEnvironment(env []string) []string {
	base := make([]string, 0, len(executorEnvironmentAllowlist)+1+len(env))
	for _, name := range executorEnvironmentAllowlist {
		// Unset stays unset rather than becoming empty, which some readers
		// treat differently.
		if value, ok := os.LookupEnv(name); ok {
			base = append(base, name+"="+value)
		}
	}
	// Drops tofu's suggestions of commands to run next, written for a person
	// at a terminal rather than a run log.
	base = append(base, "TF_IN_AUTOMATION=1")
	return append(base, env...)
}

// KillFunc kills every process of user. Root without CAP_KILL cannot signal
// another uid, so the executor kills a branch from inside it, through the
// helper running as the branch's user.
type KillFunc func(ctx context.Context, user runuser.User) error

// ErrNoBranch reports a subprocess an isolated executor was asked to start
// outside every session's branch: it would run as root.
var ErrNoBranch = errors.New("refusing to start a subprocess outside a session's branch")

// defaultWaitDelay bounds how long a cancelled command, and the kill after a
// command, may take.
const defaultWaitDelay = 10 * time.Second

type osExecCommandExecutor struct {
	// isolated refuses subprocesses outside a branch. The executor's runners
	// set it. Template sync on the api does not: it runs only git clone,
	// which runs no code from the repository.
	isolated bool
	// kill ends a branch's leftovers after each command and on cancel.
	kill      KillFunc
	waitDelay time.Duration
}

// NewIsolatedExecutor returns the executor's CommandExecutor: every subprocess
// starts as its session's pool user, and nothing it started outlives it.
func NewIsolatedExecutor(kill KillFunc) CommandExecutor {
	return osExecCommandExecutor{isolated: true, kill: kill}
}

// NewIsolatedProcessRunner returns a tofu runner on NewIsolatedExecutor.
func NewIsolatedProcessRunner(kill KillFunc) *LocalProcessRunner {
	return NewLocalProcessRunnerWithExecutor(NewIsolatedExecutor(kill))
}

// NewIsolatedGitRunner returns a git runner on NewIsolatedExecutor.
func NewIsolatedGitRunner(kill KillFunc) *LocalGitRunner {
	return NewLocalGitRunnerWithExecutor(NewIsolatedExecutor(kill))
}

// Run starts one subprocess in dir and connects its output streams.
//
// The context is passed to exec.CommandContext so cancellation or deadline
// expiry can terminate the process. The command name and args are
// intentionally supplied by the caller so this adapter stays generic. env is
// layered over the allowlist in subprocessEnvironment, never over the
// executor's own environment. In a branch the subprocess starts as the
// branch's pool user; switching from root makes the kernel clear every
// capability.
func (e osExecCommandExecutor) Run(ctx context.Context, dir string, env []string, stdout io.Writer, stderr io.Writer, name string, args ...string) error {
	branch, inBranch := runuser.BranchFrom(ctx)
	if e.isolated && !inBranch {
		return ErrNoBranch
	}
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // generic by design; see above
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if !inBranch {
		// Always set: os/exec hands a nil Env the executor's whole environment.
		cmd.Env = subprocessEnvironment(env)
		return cmd.Run()
	}

	credential, err := branch.User.Credential()
	if err != nil {
		return err
	}
	if credential != nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: credential}
	}
	if cmd.Dir == "" {
		cmd.Dir = branch.Home
	}
	// The branch's own HOME and TMPDIR, before env so a caller's value wins.
	// tofu's provider plugins put their sockets in TMPDIR.
	cmd.Env = subprocessEnvironment(append([]string{"HOME=" + branch.Home, "TMPDIR=" + branch.TempDir}, env...))
	cmd.WaitDelay = e.delay()
	if e.kill != nil {
		cmd.Cancel = func() error { return e.killBranch(ctx, branch.User) }
	}
	runErr := cmd.Run()
	if e.kill == nil {
		return runErr
	}
	// Whatever the command left running (nohup … &) dies with it.
	if err := e.killBranch(ctx, branch.User); err != nil {
		return errors.Join(runErr, fmt.Errorf("kill what the command left running: %w", err))
	}
	return runErr
}

// killBranch runs kill with a context of its own: the command's may be the
// very one that was cancelled.
func (e osExecCommandExecutor) killBranch(ctx context.Context, user runuser.User) error {
	killCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), e.delay())
	defer cancel()
	return e.kill(killCtx, user)
}

func (e osExecCommandExecutor) delay() time.Duration {
	if e.waitDelay > 0 {
		return e.waitDelay
	}
	return defaultWaitDelay
}
