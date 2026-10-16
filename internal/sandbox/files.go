package sandbox

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

// ReadFiles reads the named files in dir, skipping any that don't exist. Tenant
// code wrote them, so each must be a regular file inside dir: os.Root refuses
// a symlink out of it, and a FIFO or device is refused rather than read.
// Otherwise plan.tfplan -> /proc/self/environ would hand the reading
// sandbox's own credentials to whatever reads its result. No size limit: the
// sandbox's memory limit bounds a body that reads too much.
func ReadFiles(dir string, names []string) (map[string][]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	files := map[string][]byte{}
	for _, name := range names {
		if err := checkName(name); err != nil {
			return nil, err
		}
		content, err := readFile(root, name)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		files[name] = content
	}
	return files, nil
}

func readFile(root *os.Root, name string) ([]byte, error) {
	// O_NONBLOCK: opening a FIFO would otherwise wait for a writer that never
	// comes. Regular files ignore it.
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	return io.ReadAll(file)
}

// WriteFiles writes files into dir, replacing any already there, as regular
// files inside dir only: a name tenant code pointed elsewhere, or made a
// FIFO, fails instead of being followed or blocking.
func WriteFiles(dir string, files map[string][]byte) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, name := range slices.Sorted(maps.Keys(files)) {
		if err := checkName(name); err != nil {
			return err
		}
		if err := writeFile(root, name, files[name]); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	return nil
}

func writeFile(root *os.Root, name string, content []byte) error {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = errors.New("not a regular file")
	}
	if err == nil {
		_, err = file.Write(content)
	}
	return errors.Join(err, file.Close())
}

// CheckDir reports whether path is a directory.
func CheckDir(path string) error {
	info, err := os.Stat(path) //nolint:gosec // inside a sandbox, any path reaches only what the sandbox already can
	if errors.Is(err, fs.ErrNotExist) {
		return errors.New("directory does not exist")
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("not a directory")
	}
	return nil
}

// checkName accepts only a single path component, so a name never reaches
// outside the directory it is read from or written to.
func checkName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsRune(name, filepath.Separator) {
		return fmt.Errorf("name %q is not a single path component", name)
	}
	return nil
}
