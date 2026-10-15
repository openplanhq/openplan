package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// ActivitySubcommand is how the executor binary is run inside a sandbox:
// openplan-executor activity <name>.
const ActivitySubcommand = "activity"

// maxRequestSize bounds the request a body reads from stdin.
const maxRequestSize = 16 << 20

// Handler is one activity's body. It decodes its request, does its work,
// writes what its commands print to output, and returns a result that
// marshals to JSON.
type Handler func(ctx context.Context, request json.RawMessage, output io.Writer) (any, error)

// Serve runs the body args names: the request from stdin, frames to stdout.
// It returns the exit code: 0 for a result, 1 for a body that failed, 2 for a
// call no body answers. It writes a result frame in every case, so the worker
// always learns why.
func Serve(ctx context.Context, handlers map[string]Handler, args []string, stdin io.Reader, stdout io.Writer) int {
	if len(args) != 1 {
		return fail(stdout, 2, fmt.Errorf("usage: %s <name>", ActivitySubcommand))
	}
	handler, ok := handlers[args[0]]
	if !ok {
		return fail(stdout, 2, fmt.Errorf("no activity %q", args[0]))
	}
	request, err := io.ReadAll(io.LimitReader(stdin, maxRequestSize+1))
	if err != nil {
		return fail(stdout, 2, fmt.Errorf("read the request: %w", err))
	}
	if len(request) > maxRequestSize {
		return fail(stdout, 2, fmt.Errorf("request larger than %d bytes", maxRequestSize))
	}
	data, err := handler(ctx, request, &frameWriter{w: stdout})
	if err != nil {
		return fail(stdout, 1, err)
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return fail(stdout, 1, fmt.Errorf("encode the result: %w", err))
	}
	payload, err := json.Marshal(resultMessage{Data: encoded})
	if err != nil || writeFrame(stdout, frameResult, payload) != nil {
		return 1
	}
	return 0
}

func fail(stdout io.Writer, code int, err error) int {
	if payload, marshalErr := json.Marshal(resultMessage{Error: err.Error()}); marshalErr == nil {
		_ = writeFrame(stdout, frameResult, payload)
	}
	return code
}
