package sandbox

import (
	"context"
	"encoding/json"
	"io"
)

// Runner runs one activity's body in a sandbox of its own.
type Runner interface {
	// Run starts a fresh sandbox for job, copies what the body's commands
	// print to output as it arrives, and returns the body's result, or its
	// error.
	Run(ctx context.Context, job Job, output io.Writer) (json.RawMessage, error)
}

// Job is one activity body to run.
type Job struct {
	// Activity names the body: openplan-executor activity <Activity>.
	Activity string
	// Request is marshalled to JSON and written to the sandbox's stdin. It is
	// the only way a credential reaches a sandbox.
	Request any
	// Mounts are host paths bound into the sandbox at the same path, so paths
	// in activity inputs need no translation.
	Mounts []Mount
	// Groups are supplementary gids for the sandbox's process, such as the
	// artifacts directory's group on the filesystem store.
	Groups []uint32
}

// Mount is one host directory a sandbox sees.
type Mount struct {
	Path     string
	ReadOnly bool
}
