package runner

import (
	"context"
	"io"
)

// CommandExecutor is the subprocess boundary shared by LocalTerraformRunner and
// LocalGitRunner.
//
// Implementations receive the fully resolved working directory, output streams,
// executable name, and CLI arguments. The production executor is
// ProcessExecutor: on the api for template sync, and inside each activity's
// sandbox on the executor, where the sandbox decides what a command can reach
// and this package only builds it. Tests can provide a recorder or fake
// executor to verify command construction without invoking a subprocess.
type CommandExecutor interface {
	Run(ctx context.Context, dir string, env []string, stdout io.Writer, stderr io.Writer, name string, args ...string) error
}
