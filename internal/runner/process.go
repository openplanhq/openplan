package runner

import (
	"context"
	"io"
	"os"
	"os/exec"
	"time"
)

// processEnvironmentAllowlist names the only variables a subprocess takes from
// this process's environment. On the api that environment holds the
// database URL and keys; inside a sandbox it is the sandbox's own scratch
// environment. What a command needs beyond these arrives through Run's env.
//
// Names, not prefixes: TF_* would forward a TF_VAR_* or TF_CLI_ARGS set on
// the host into every tenant's run.
var processEnvironmentAllowlist = []string{
	// Finding executables, and where tofu and git keep config and scratch
	// files.
	"PATH",
	"HOME",
	"TMPDIR",
	// Corporate egress: provider and module downloads and git fetches go
	// through the proxy and trust its CA. Both cases, because Go reads either
	// and curl reads only the lowercase http_proxy.
	"HTTP_PROXY",
	"HTTPS_PROXY",
	"NO_PROXY",
	"http_proxy",
	"https_proxy",
	"no_proxy",
	"SSL_CERT_FILE",
	"SSL_CERT_DIR",
}

// processEnvironment returns a subprocess's whole environment: the
// allowlisted variables that are set, TF_IN_AUTOMATION, then env. env comes
// last so a caller's value wins; exec.Cmd keeps the last of duplicate names.
func processEnvironment(lookup func(string) (string, bool), env []string) []string {
	base := make([]string, 0, len(processEnvironmentAllowlist)+1+len(env))
	for _, name := range processEnvironmentAllowlist {
		// Unset stays unset rather than becoming empty, which some readers
		// treat differently.
		if value, ok := lookup(name); ok {
			base = append(base, name+"="+value)
		}
	}
	// Drops tofu's suggestions of commands to run next, written for a person
	// at a terminal rather than a run log.
	base = append(base, "TF_IN_AUTOMATION=1")
	return append(base, env...)
}

// processWaitDelay bounds how long Run waits, once a command has exited or
// been cancelled, for anything it left holding its output.
const processWaitDelay = 10 * time.Second

// ProcessExecutor starts subprocesses as this process's own user with only the
// allowlisted environment. The api's template sync uses it for git, and every
// activity body uses it inside its sandbox: the sandbox, not this type, is
// what keeps tenant code apart.
type ProcessExecutor struct{}

// Run starts one subprocess in dir and connects its output streams; ctx
// cancels it.
func (ProcessExecutor) Run(ctx context.Context, dir string, env []string, stdout io.Writer, stderr io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // generic by design: callers build the command
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	// Always set: os/exec hands a nil Env the whole environment.
	cmd.Env = processEnvironment(os.LookupEnv, env)
	cmd.WaitDelay = processWaitDelay
	return cmd.Run()
}
