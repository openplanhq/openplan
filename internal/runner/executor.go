package runner

import (
	"context"
	"io"
)

// CommandExecutor is the subprocess boundary shared by LocalProcessRunner and
// LocalGitRunner.
//
// Implementations receive the fully resolved working directory, output streams,
// executable name, and CLI arguments. The production executors are
// isolation's: how a subprocess starts, as which user and with which
// environment, is the isolation package's to decide, while this package only
// builds the command. Tests can provide a recorder or fake executor to verify
// command construction without invoking a subprocess.
type CommandExecutor interface {
	Run(ctx context.Context, dir string, env []string, stdout io.Writer, stderr io.Writer, name string, args ...string) error
}
