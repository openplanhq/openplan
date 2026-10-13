package isolation

// This file is the helper's side: the executor binary re-run as a session's
// pool user, to do the work inside that session's workspace. Everything here
// runs as that user, so any process of the session can tamper with it: it
// must never be handed a secret, and the executor checks what it reports.

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

// HelperSubcommand is the hidden first argument that turns the executor
// binary into the helper. The executor's main hands over to HelperMain when
// it sees it, before any of its own startup.
const HelperSubcommand = "helper"

// scratchDirs are the world-writable directories where a session can leave
// files that outlive its workspace, for the next session on the same user.
var scratchDirs = []string{"/tmp", "/var/tmp", "/dev/shm"}

// HelperMain runs one helper operation and returns the process exit code. It
// acts only as the user its process already is, so calling it grants
// nothing.
func HelperMain(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if err := dispatch(args, stdin, stdout); err != nil {
		fmt.Fprintf(stderr, "helper: %v\n", err)
		return 1
	}
	return 0
}

// dispatch runs the operation args names. Each case takes exactly the
// arguments helperClient sends, so a client and helper that disagree fail as
// an unknown operation instead of running with an argument ignored. File
// contents travel on stdin and stdout, not as arguments: a plan can run to
// maxFileSize, far past what a command line holds.
func dispatch(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("no operation")
	}
	op, rest := args[0], args[1:]
	switch {
	case op == "kill" && len(rest) == 0:
		return killAll()
	case op == "reclaim" && len(rest) <= 1:
		// Kill first: a process left running could write new files after
		// they are cleared.
		if err := killAll(); err != nil {
			return err
		}
		// The helper runs as the session's user, so its own uid picks out
		// that session's files and no one else's.
		if err := clearScratch(uint32(os.Getuid()), scratchDirs); err != nil { //nolint:gosec // the kernel stores uids in 32 bits
			return err
		}
		if len(rest) == 1 {
			return emptyDir(rest[0])
		}
		return nil
	case op == "read-files" && len(rest) >= 1:
		files, err := readFiles(rest[0], rest[1:])
		if err != nil {
			return err
		}
		archive, err := encodeFiles(files)
		if err != nil {
			return err
		}
		_, err = stdout.Write(archive)
		return err
	case op == "write-files" && len(rest) == 1:
		files, err := decodeFiles(stdin)
		if err != nil {
			return err
		}
		return writeFiles(rest[0], files)
	case op == "check-dir" && len(rest) == 1:
		return checkDir(rest[0])
	}
	return fmt.Errorf("unknown operation %q", strings.Join(args, " "))
}

// readFiles reads names from dir. A name that doesn't exist is left out, so
// the caller decides which it needs: a module with no providers has no lock
// file.
//
// Running as the pool user keeps a symlink from reaching anything the session
// couldn't read anyway, but not from making the helper read forever: a name
// pointed at /dev/zero, a FIFO with no writer, or a sparse file the size of
// the disk would. So reads stay inside dir, take only regular files, and stop
// at maxFileSize.
func readFiles(dir string, names []string) (map[string][]byte, error) {
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
	// O_NONBLOCK: opening a FIFO would otherwise wait for a writer that
	// never comes. Regular files ignore it.
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
	// The limit, not the size, bounds the read: the file can grow meanwhile.
	content, err := io.ReadAll(io.LimitReader(file, maxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(content) > maxFileSize {
		return nil, fmt.Errorf("larger than %d bytes", maxFileSize)
	}
	return content, nil
}

// writeFiles writes files into dir, replacing any already there. Like
// readFiles it stays inside dir and writes only regular files, so a name the
// session pointed elsewhere, or made a FIFO, fails instead of being followed
// or blocking.
func writeFiles(dir string, files map[string][]byte) error {
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

// checkDir reports whether path is a directory.
func checkDir(path string) error {
	info, err := os.Stat(path) //nolint:gosec // the helper runs as the pool user: any path reaches only what the session already can
	if errors.Is(err, fs.ErrNotExist) {
		return errors.New("directory does not exist")
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("is not a directory")
	}
	return nil
}

// emptyDir deletes everything inside dir, but not dir itself. A session can
// leave directories it made unreadable, so each directory is made the owner's
// again before it is read. The session's processes are dead by then, so
// nothing can swap a directory for a symlink between the check and the chmod.
func emptyDir(dir string) error {
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error { //nolint:gosec // the helper runs as the pool user: any path reaches only what the session already can
		if err != nil {
			return err
		}
		if entry.IsDir() {
			// WalkDir calls this before it reads the directory.
			return os.Chmod(path, 0o700) //nolint:gosec // a directory needs its search bit
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("open up %s: %w", dir, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var errs []error
	for _, entry := range entries {
		// RemoveAll removes a symlink itself, never what it points to.
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil { //nolint:gosec // the helper runs as the pool user: any path reaches only what the session already can
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// clearScratch removes every entry uid owns in dirs. Directories other users
// own, and dirs that do not exist, are skipped.
func clearScratch(uid uint32, dirs []string) error {
	var errs []error
	for _, root := range dirs {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || path == root {
				// Unreadable: another user's directory, or a missing root.
				return nil //nolint:nilerr // skipped, not failed: other users' directories are none of ours
			}
			info, err := entry.Info()
			if err != nil {
				return nil //nolint:nilerr // the entry is already gone
			}
			if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Uid != uid {
				return nil
			}
			if err := os.RemoveAll(path); err != nil { //nolint:gosec // as the pool user, a swapped path reaches only that user's own files
				errs = append(errs, err)
			}
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		})
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// killAll kills every process this user owns, except the caller.
func killAll() error {
	if uid := os.Getuid(); !isPoolUID(uid) {
		return fmt.Errorf("refusing to kill every process of uid %d: not a pool user", uid)
	}
	if err := syscall.Kill(-1, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("kill: %w", err)
	}
	return nil
}
