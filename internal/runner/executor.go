package runner

import (
	"context"
	"io"
	"os"
	"os/exec"
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
	// and write scratch files to. They stay the executor's until runs get
	// their own uid (#331).
	"PATH",
	"HOME",
	"TMPDIR",
	// Set by Dockerfile.executor; without it every init re-downloads providers.
	"TF_PLUGIN_CACHE_DIR",
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

type osExecCommandExecutor struct{}

// Run starts one subprocess in dir and connects its output streams.
//
// The context is passed to exec.CommandContext so cancellation or deadline
// expiry can terminate the process. The command name and args are
// intentionally supplied by the caller so this adapter stays generic. env is
// layered over the allowlist in subprocessEnvironment, never over the
// executor's own environment.
func (osExecCommandExecutor) Run(ctx context.Context, dir string, env []string, stdout io.Writer, stderr io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // generic by design; see above
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	// Always set: os/exec hands a nil Env the executor's whole environment.
	cmd.Env = subprocessEnvironment(env)
	return cmd.Run()
}
