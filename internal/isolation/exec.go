package isolation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

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
	// and write scratch files to. In a session HOME and TMPDIR are the
	// session's own instead, inside its workspace (#331); these are for
	// subprocesses outside a session, such as template sync on the api.
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

// defaultWaitDelay bounds how long a cancelled command, and the kill after a
// command, may take.
const defaultWaitDelay = 10 * time.Second

// Executor starts subprocesses. It satisfies runner.CommandExecutor, which is
// how the tofu and git runners start every command. The zero Executor is the
// session executor, so one built without a constructor refuses to start
// anything outside a session rather than start it as root.
type Executor struct {
	// host starts subprocesses as this process's own user, outside any
	// session: on the executor, as root with root's three capabilities.
	host      bool
	waitDelay time.Duration
}

// HostExecutor starts subprocesses as this process's own user, with only the
// allowlisted environment. Template sync on the api uses it: it runs only git
// clone, which runs no code from the repository.
func HostExecutor() Executor {
	return Executor{host: true}
}

// SessionExecutor starts every subprocess as the pool user of the session its
// ctx is tagged with (Session.Context), and makes sure nothing it started
// outlives it. It refuses a ctx with no open session.
func SessionExecutor() Executor {
	return Executor{}
}

// Run starts one subprocess in dir and connects its output streams.
//
// The context is passed to exec.CommandContext so cancellation or deadline
// expiry can terminate the process. The command name and args are
// intentionally supplied by the caller so this adapter stays generic. env is
// layered over the allowlist in subprocessEnvironment, never over the
// executor's own environment. In a session the subprocess starts as the
// session's pool user; switching from root makes the kernel clear every
// capability.
func (e Executor) Run(ctx context.Context, dir string, env []string, stdout io.Writer, stderr io.Writer, name string, args ...string) error {
	if e.host {
		cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // generic by design; see above
		cmd.Dir = dir
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		// Always set: os/exec hands a nil Env the executor's whole environment.
		cmd.Env = subprocessEnvironment(env)
		return cmd.Run()
	}

	s, err := sessionFrom(ctx)
	if err != nil {
		return err
	}
	credential, err := s.user.credential()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // generic by design; see above
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if credential != nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: credential}
	}
	if cmd.Dir == "" {
		cmd.Dir = s.home()
	}
	// The session's own HOME and TMPDIR, before env so a caller's value wins.
	// tofu's provider plugins put their sockets in TMPDIR.
	cmd.Env = subprocessEnvironment(append([]string{"HOME=" + s.home(), "TMPDIR=" + s.tempDir()}, env...))
	cmd.WaitDelay = e.delay()
	cmd.Cancel = func() error { return e.kill(ctx, s) }
	runErr := cmd.Run()
	// Whatever the command left running (nohup … &) dies with it.
	if err := e.kill(ctx, s); err != nil {
		return errors.Join(runErr, fmt.Errorf("kill what the command left running: %w", err))
	}
	return runErr
}

// kill ends every process of s's user. Root without CAP_KILL cannot signal
// another uid, so the kill runs from inside the session, through the helper.
// It gets a context of its own: the command's may be the very one that was
// cancelled.
func (e Executor) kill(ctx context.Context, s *Session) error {
	// A session that has ended had every process of its user killed by its
	// reclaim, and its user may be another session's by now: a kill could
	// only reach that session.
	if s.open() != nil {
		return nil
	}
	killCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), e.delay())
	defer cancel()
	return s.m.helper.kill(killCtx, s.user)
}

func (e Executor) delay() time.Duration {
	if e.waitDelay > 0 {
		return e.waitDelay
	}
	return defaultWaitDelay
}
