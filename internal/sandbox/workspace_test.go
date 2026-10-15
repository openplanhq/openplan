package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareLaysOutTheRunDirectory(t *testing.T) {
	t.Parallel()

	w := Workspaces{Root: t.TempDir()}
	workspace, err := w.Prepare(context.Background(), "tenant_1", "run_1")
	if err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}
	if want := filepath.Join(w.Root, "tenant_1", "run_1", "workspace"); workspace != want {
		t.Fatalf("workspace = %q, want %q", workspace, want)
	}
	info, err := os.Stat(filepath.Join(w.Root, "tenant_1", "run_1", "logs"))
	if err != nil {
		t.Fatalf("logs directory: %v", err)
	}
	// UploadLog's sandbox reads the spool as SandboxUID.
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("logs mode = %v, want 0755", info.Mode().Perm())
	}
	// SandboxUID walks down to its own workspace, but lists no one's.
	for _, dir := range []string{filepath.Join(w.Root, "tenant_1"), filepath.Join(w.Root, "tenant_1", "run_1")} {
		if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o711 {
			t.Fatalf("%s: %v, %v; want mode 0711", dir, info, err)
		}
	}
}

func TestPrepareStartsFresh(t *testing.T) {
	t.Parallel()

	w := Workspaces{Root: t.TempDir()}
	workspace, err := w.Prepare(context.Background(), "tenant_1", "run_1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "left-behind"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Prepare(context.Background(), "tenant_1", "run_1"); err != nil {
		t.Fatalf("second Prepare returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "left-behind")); !os.IsNotExist(err) {
		t.Fatalf("an earlier attempt's file survived: %v", err)
	}
}

func TestPrepareRefusesUnsafeNames(t *testing.T) {
	t.Parallel()

	w := Workspaces{Root: t.TempDir()}
	for _, ids := range [][2]string{{"../x", "run"}, {"tenant", "a/b"}, {"", "run"}, {"tenant", ".."}} {
		if _, err := w.Prepare(context.Background(), ids[0], ids[1]); err == nil {
			t.Fatalf("Prepare(%q, %q) accepted it", ids[0], ids[1])
		}
	}
	entries, _ := os.ReadDir(w.Root)
	if len(entries) != 0 {
		t.Fatalf("run root holds %d entries, want none", len(entries))
	}
}

func TestRemoveDeletesTheRunDirectory(t *testing.T) {
	t.Parallel()

	w := Workspaces{Root: t.TempDir()}
	workspace, err := w.Prepare(context.Background(), "tenant_1", "run_1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "source", ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := w.Remove(context.Background(), "tenant_1", "run_1"); err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(w.Root, "tenant_1", "run_1")); !os.IsNotExist(err) {
		t.Fatalf("run directory survived: %v", err)
	}
}
