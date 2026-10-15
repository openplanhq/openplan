package sandbox

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// A frame is one byte of kind, four bytes of big-endian length, then the
// payload. Output frames carry what the body's commands write; the result
// frame, last, carries its outcome.
const (
	frameOutput byte = 'o'
	frameResult byte = 'r'

	// maxOutputFrame bounds one output frame; longer writes are split.
	maxOutputFrame = 32 << 10
	// MaxResultSize bounds the result frame, which the worker parses: the
	// body that wrote it may have shared its sandbox with tofu.
	MaxResultSize = 1 << 20
)

// resultMessage is the result frame's payload.
type resultMessage struct {
	Data  json.RawMessage `json:"data,omitempty"`
	Error string          `json:"error,omitempty"`
}

func writeFrame(w io.Writer, kind byte, payload []byte) error {
	var header [5]byte
	header[0] = kind
	binary.BigEndian.PutUint32(header[1:], uint32(len(payload))) //nolint:gosec // payloads are bounded far below 4 GiB
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// errFrameTooLarge rejects a frame longer than its kind allows, before its
// payload is read.
var errFrameTooLarge = errors.New("frame too large")

func readFrame(r io.Reader) (byte, []byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, err
	}
	kind := header[0]
	size := binary.BigEndian.Uint32(header[1:])
	limit := uint32(maxOutputFrame)
	if kind == frameResult {
		limit = MaxResultSize
	}
	if size > limit {
		return 0, nil, fmt.Errorf("%w: %d bytes of kind %q", errFrameTooLarge, size, kind)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	return kind, payload, nil
}

// frameWriter frames everything written to it as output. A command's stdout
// and stderr are copied by goroutines of their own, so writes are serialised.
type frameWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (f *frameWriter) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for written := 0; written < len(p); {
		end := min(written+maxOutputFrame, len(p))
		if err := writeFrame(f.w, frameOutput, p[written:end]); err != nil {
			return written, err
		}
		written = end
	}
	return len(p), nil
}

// errNoResult is a stream that ended without a result frame: the body died,
// or its sandbox did.
var errNoResult = errors.New("the activity's sandbox exited without a result")

// readFrames copies output frames to output until the stream ends, and
// returns the result frame, which must be the last.
func readFrames(r io.Reader, output io.Writer) (resultMessage, error) {
	var result *resultMessage
	for {
		kind, payload, err := readFrame(r)
		if errors.Is(err, io.EOF) {
			if result == nil {
				return resultMessage{}, errNoResult
			}
			return *result, nil
		}
		if err != nil {
			return resultMessage{}, fmt.Errorf("read the sandbox's output: %w", err)
		}
		if result != nil {
			return resultMessage{}, errors.New("the sandbox wrote past its result")
		}
		switch kind {
		case frameOutput:
			if _, err := output.Write(payload); err != nil {
				return resultMessage{}, fmt.Errorf("write the activity's output: %w", err)
			}
		case frameResult:
			var message resultMessage
			if err := json.Unmarshal(payload, &message); err != nil {
				return resultMessage{}, fmt.Errorf("decode the activity's result: %w", err)
			}
			result = &message
		default:
			return resultMessage{}, fmt.Errorf("unknown frame kind %q", kind)
		}
	}
}

// decodeResult turns a result frame into what Runner.Run returns. The error
// crossed a process boundary as text, so callers match it by message.
func decodeResult(message resultMessage) (json.RawMessage, error) {
	if message.Error != "" {
		return nil, errors.New(message.Error)
	}
	return message.Data, nil
}
