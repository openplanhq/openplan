package sandbox

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func echoHandlers() map[string]Handler {
	return map[string]Handler{
		"echo": func(_ context.Context, request json.RawMessage, output io.Writer) (any, error) {
			_, _ = output.Write([]byte("hello\n"))
			return map[string]string{"got": string(request)}, nil
		},
		"fail": func(context.Context, json.RawMessage, io.Writer) (any, error) {
			return nil, errors.New("tofu failed")
		},
	}
}

func TestServeReturnsTheResultAfterTheOutput(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	data, err := InProcess{Handlers: echoHandlers()}.Run(context.Background(), Job{Activity: "echo", Request: "x"}, &output)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if output.String() != "hello\n" {
		t.Fatalf("output = %q, want %q", output.String(), "hello\n")
	}
	if string(data) != `{"got":"\"x\""}` {
		t.Fatalf("result = %s, want {\"got\":\"\\\"x\\\"\"}", data)
	}
}

func TestServeCarriesTheBodysError(t *testing.T) {
	t.Parallel()

	_, err := InProcess{Handlers: echoHandlers()}.Run(context.Background(), Job{Activity: "fail"}, io.Discard)
	if err == nil || err.Error() != "tofu failed" {
		t.Fatalf("error = %v, want tofu failed", err)
	}
}

func TestServeRefusesAnUnknownActivity(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	if code := Serve(context.Background(), echoHandlers(), []string{"nope"}, strings.NewReader("{}"), &stdout); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	message, err := readFrames(&stdout, io.Discard)
	if err != nil || !strings.Contains(message.Error, `no activity "nope"`) {
		t.Fatalf("result = %+v, %v; want the unknown activity named", message, err)
	}
}

func TestFrameWriterSplitsLongWrites(t *testing.T) {
	t.Parallel()

	var stream bytes.Buffer
	long := bytes.Repeat([]byte("a"), 3*maxOutputFrame+5)
	if _, err := (&frameWriter{w: &stream}).Write(long); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	frames := 0
	for r := bytes.NewReader(stream.Bytes()); r.Len() > 0; frames++ {
		kind, _, err := readFrame(r)
		if err != nil || kind != frameOutput {
			t.Fatalf("frame %d: kind %q, err %v", frames, kind, err)
		}
	}
	if frames != 4 {
		t.Fatalf("frames = %d, want 4", frames)
	}
}

// Review focus 3: output is bytes, not text.
func TestFramesCarryBinaryOutput(t *testing.T) {
	t.Parallel()

	binaryOutput := []byte{0, 0xff, '\n', 0, 'x'}
	handlers := map[string]Handler{"bin": func(_ context.Context, _ json.RawMessage, output io.Writer) (any, error) {
		_, _ = output.Write(binaryOutput)
		return nil, nil
	}}
	var output bytes.Buffer
	if _, err := (InProcess{Handlers: handlers}).Run(context.Background(), Job{Activity: "bin"}, &output); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !bytes.Equal(output.Bytes(), binaryOutput) {
		t.Fatalf("output = %v, want %v", output.Bytes(), binaryOutput)
	}
}

// Review focus 2: a sandbox that dies says so in words.
func TestReadFramesWithoutAResult(t *testing.T) {
	t.Parallel()

	var stream bytes.Buffer
	_ = writeFrame(&stream, frameOutput, []byte("partial"))
	_, err := readFrames(&stream, io.Discard)
	if !errors.Is(err, errNoResult) {
		t.Fatalf("error = %v, want errNoResult", err)
	}
}

func TestReadFramesRejectsAnOversizedResult(t *testing.T) {
	t.Parallel()

	var header [5]byte
	header[0] = frameResult
	binary.BigEndian.PutUint32(header[1:], MaxResultSize+1)
	_, err := readFrames(bytes.NewReader(header[:]), io.Discard)
	if !errors.Is(err, errFrameTooLarge) {
		t.Fatalf("error = %v, want errFrameTooLarge", err)
	}
}

func TestReadFramesRejectsOutputAfterTheResult(t *testing.T) {
	t.Parallel()

	var stream bytes.Buffer
	_ = writeFrame(&stream, frameResult, []byte(`{"data":null}`))
	_ = writeFrame(&stream, frameOutput, []byte("late"))
	if _, err := readFrames(&stream, io.Discard); err == nil {
		t.Fatal("readFrames accepted output after the result")
	}
}
