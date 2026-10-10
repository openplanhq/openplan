// Package runhelper is the executor's helper: the executor binary re-run as a
// session's pool user, to do the work inside that session's workspace. The
// executor itself never reads or writes there. Root without CAP_DAC_OVERRIDE
// cannot, and a branch controls every name in it, so root following a planted
// symlink would read or write root's own files (#331).
package runhelper

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/vishu42/openplan/internal/runuser"
)

// ScratchDirs are the world-writable directories where a branch can leave
// files that outlive its workspace, for the next session on the same user.
var ScratchDirs = []string{"/tmp", "/var/tmp", "/dev/shm"}

// CheckRoot reports whether dir, a template's root, is a directory.
func CheckRoot(dir string) error {
	info, err := os.Stat(dir) //nolint:gosec // the helper runs as the pool user: any path reaches only what the branch already can
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

// EmptyDir deletes everything inside dir, but not dir itself. A branch can
// leave directories it made unreadable, so each directory is made the owner's
// again before it is read. The branch's processes are dead by then, so nothing
// can swap a directory for a symlink between the check and the chmod.
func EmptyDir(dir string) error {
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error { //nolint:gosec // the helper runs as the pool user: any path reaches only what the branch already can
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
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil { //nolint:gosec // the helper runs as the pool user: any path reaches only what the branch already can
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ClearScratch removes every entry uid owns in dirs. Directories other users
// own, and dirs that do not exist, are skipped.
func ClearScratch(uid uint32, dirs []string) error {
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
	if uid := os.Getuid(); !runuser.IsPoolUID(uid) {
		return fmt.Errorf("refusing to kill every process of uid %d: not a pool user", uid)
	}
	if err := syscall.Kill(-1, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("kill: %w", err)
	}
	return nil
}
