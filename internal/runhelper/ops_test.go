package runhelper

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckRoot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := CheckRoot(dir); err != nil {
		t.Fatalf("CheckRoot(dir) = %v", err)
	}
	if err := CheckRoot(file); err == nil || !strings.Contains(err.Error(), "is not a directory") {
		t.Fatalf("CheckRoot(file) = %v", err)
	}
	if err := CheckRoot(filepath.Join(dir, "missing")); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("CheckRoot(missing) = %v", err)
	}
}

// A template can leave directories it made unreadable. Reclaim must still
// empty the workspace, or the next session on that user inherits them.
func TestEmptyDirRemovesUnreadableDirectories(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	locked := filepath.Join(workspace, "a", "locked")
	if err := os.MkdirAll(filepath.Join(locked, "deep"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "deep", "f"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}

	if err := EmptyDir(workspace); err != nil {
		t.Fatalf("EmptyDir = %v", err)
	}
	entries, err := os.ReadDir(workspace)
	if err != nil || len(entries) != 0 {
		t.Fatalf("workspace holds %v, %v; want it empty", entries, err)
	}
}

// A symlink in the workspace is removed, never followed.
func TestEmptyDirDoesNotFollowSymlinks(t *testing.T) {
	t.Parallel()
	workspace, outside := t.TempDir(), t.TempDir()
	keep := filepath.Join(outside, "keep")
	if err := os.WriteFile(keep, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "link")); err != nil {
		t.Fatal(err)
	}

	if err := EmptyDir(workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("file behind the symlink is gone: %v", err)
	}
}

func TestClearScratchRemovesOnlyTheUsersEntries(t *testing.T) {
	t.Parallel()
	scratch := t.TempDir()
	if err := os.WriteFile(filepath.Join(scratch, "mine"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(scratch, "dir"), 0o700); err != nil {
		t.Fatal(err)
	}

	// A uid that owns nothing here leaves everything alone.
	if err := ClearScratch(70040, []string{scratch, filepath.Join(scratch, "missing")}); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(scratch); len(entries) != 2 {
		t.Fatalf("entries = %v, want both kept", entries)
	}

	if err := ClearScratch(uint32(os.Getuid()), []string{scratch}); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(scratch); len(entries) != 0 {
		t.Fatalf("entries = %v, want both removed", entries)
	}
}

// As anyone but a pool user, kill(-1) would take down the developer's whole
// session, or as root the container. The guard must hold before any signal.
func TestKillRefusesOutsideThePool(t *testing.T) {
	t.Parallel()

	for _, op := range []string{"kill", "reclaim"} {
		var stderr bytes.Buffer
		if code := Main([]string{op}, nil, nil, &stderr); code == 0 || !strings.Contains(stderr.String(), "not a pool user") {
			t.Fatalf("Main(%s) = %d, %q; want a refusal", op, code, stderr.String())
		}
	}
}

func TestMainPacksAndUnpacks(t *testing.T) {
	t.Parallel()
	from, to := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(from, "tfplan"), []byte("the plan"), 0o600); err != nil {
		t.Fatal(err)
	}

	var bundle, stderr bytes.Buffer
	if code := Main([]string{"pack", from}, nil, &bundle, &stderr); code != 0 {
		t.Fatalf("pack = %d: %s", code, stderr.String())
	}
	if code := Main([]string{"unpack", to}, &bundle, nil, &stderr); code != 0 {
		t.Fatalf("unpack = %d: %s", code, stderr.String())
	}
	if got, _ := os.ReadFile(filepath.Join(to, "tfplan")); string(got) != "the plan" {
		t.Fatalf("unpacked plan = %q", got)
	}
}

func TestMainRejectsUnknownOperations(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	if code := Main([]string{"pack"}, nil, nil, &stderr); code == 0 {
		t.Fatal("pack without a directory succeeded")
	}
	if code := Main([]string{"rm", "-rf", "/"}, nil, nil, &stderr); code == 0 {
		t.Fatal("an unknown operation succeeded")
	}
}
