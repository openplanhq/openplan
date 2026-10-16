package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The attack the spec names: a plan file swapped for a link to the
// sandbox's own environment, which holds its storage key.
func TestReadFilesRefusesASymlinkOutOfTheDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.Symlink("/proc/self/environ", filepath.Join(dir, "tfplan")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../outside", filepath.Join(dir, "rel")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tfplan", "rel"} {
		if _, err := ReadFiles(dir, []string{name}); err == nil {
			t.Fatalf("ReadFiles followed %s out of the directory", name)
		}
	}
}

func TestReadFilesRefusesAFIFOWithoutBlocking(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := unix.Mkfifo(filepath.Join(dir, "tfplan"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFiles(dir, []string{"tfplan"}); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("error = %v, want not a regular file", err)
	}
}

func TestReadFilesSkipsMissingFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tfplan"), []byte("plan"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := ReadFiles(dir, []string{"tfplan", ".terraform.lock.hcl"})
	if err != nil {
		t.Fatalf("ReadFiles returned error: %v", err)
	}
	if len(files) != 1 || string(files["tfplan"]) != "plan" {
		t.Fatalf("files = %v, want only tfplan", files)
	}
}

func TestWriteFilesRefusesToWriteThroughASymlink(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "target")
	if err := os.Symlink(outside, filepath.Join(dir, "tfplan")); err != nil {
		t.Fatal(err)
	}
	if err := WriteFiles(dir, map[string][]byte{"tfplan": []byte("x")}); err == nil {
		t.Fatal("WriteFiles wrote through a symlink out of the directory")
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("target outside the directory exists: %v", err)
	}
}

func TestCheckNameRefusesPaths(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"", ".", "..", "a/b", "/etc"} {
		if checkName(name) == nil {
			t.Fatalf("checkName(%q) accepted it", name)
		}
	}
}
