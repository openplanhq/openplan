package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// InProcess runs each job's body in this process, through the same request
// and frame encoding a sandbox uses, and isolates nothing. Tests use it to
// run activities end to end without runsc.
type InProcess struct {
	Handlers map[string]Handler
}

// Run implements Runner.
func (p InProcess) Run(ctx context.Context, job Job, output io.Writer) (json.RawMessage, error) {
	request, err := json.Marshal(job.Request)
	if err != nil {
		return nil, fmt.Errorf("encode the request: %w", err)
	}
	reader, writer := io.Pipe()
	go func() {
		Serve(ctx, p.Handlers, []string{job.Activity}, bytes.NewReader(request), writer)
		_ = writer.Close()
	}()
	message, err := readFrames(reader, output)
	// Unblocks Serve should readFrames have stopped early.
	_ = reader.Close()
	if err != nil {
		return nil, err
	}
	return decodeResult(message)
}
