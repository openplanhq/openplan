package activities

import "io"

// spoolLimit caps one command's log, so a command that writes without end
// cannot fill the executor's disk, which every run on it shares.
const spoolLimit = 64 << 20

// truncatedMarker ends a log that hit spoolLimit, once.
const truncatedMarker = "\n[openplan: output truncated at 64 MiB]\n"

// cappedWriter passes writes through until limit bytes, then writes the
// marker and drops the rest. It reports every write as complete, so the
// sandbox's stream keeps draining.
type cappedWriter struct {
	w         io.WriteCloser
	remaining int64
	truncated bool
}

func newCappedWriter(w io.WriteCloser, limit int64) *cappedWriter {
	return &cappedWriter{w: w, remaining: limit}
}

func (c *cappedWriter) Write(p []byte) (int, error) {
	if c.truncated {
		return len(p), nil
	}
	if int64(len(p)) <= c.remaining {
		if _, err := c.w.Write(p); err != nil {
			return 0, err
		}
		c.remaining -= int64(len(p))
		return len(p), nil
	}
	if _, err := c.w.Write(p[:c.remaining]); err != nil {
		return 0, err
	}
	c.remaining = 0
	c.truncated = true
	if _, err := io.WriteString(c.w, truncatedMarker); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *cappedWriter) Close() error { return c.w.Close() }
