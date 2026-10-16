package logsink

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileSinkWritesAndAppendsPhaseLog(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sink := NewFileSink(dir)

	first, err := sink.Open("plan.log")
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	if _, err := first.Write([]byte("first line\n")); err != nil {
		t.Fatalf("write first log: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first log: %v", err)
	}

	second, err := sink.Open("plan.log")
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	if _, err := second.Write([]byte("second line\n")); err != nil {
		t.Fatalf("write second log: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("close second log: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "plan.log"))
	if err != nil {
		t.Fatalf("read phase log: %v", err)
	}
	if string(got) != "first line\nsecond line\n" {
		t.Fatalf("log content = %q", string(got))
	}
}

func TestFileSinkRejectsUnsafePhase(t *testing.T) {
	t.Parallel()

	sink := NewFileSink(t.TempDir())

	_, err := sink.Open("../plan.log")
	if err == nil {
		t.Fatal("Open returned nil error, want unsafe phase error")
	}
	if !strings.Contains(err.Error(), "safe path") {
		t.Fatalf("error = %q, want safe path context", err.Error())
	}
}
