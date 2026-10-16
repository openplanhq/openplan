package sandbox

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// sandboxCgroup is the cgroup, under the executor container's own, that
// every sandbox's cgroup is created in.
const sandboxCgroup = "sandboxes"

// controllers are the ones Limits sets.
const controllers = "+memory +pids +cpu"

// DelegateCgroups readies root, the container's own cgroup2 mount, for runsc
// to create one cgroup per sandbox in, and returns their parent. A
// container's cgroupfs is mounted read-only, and cgroup v2 lets a cgroup give
// controllers to children only once it holds no process itself, so it
// remounts root read-write, moves the container's processes into a leaf, and
// enables the controllers for sandboxCgroup. The executor calls it once at
// startup and refuses to start if it fails.
func DelegateCgroups(root string) (string, error) {
	if err := unix.Mount("", root, "", unix.MS_REMOUNT|unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC|unix.MS_RELATIME, ""); err != nil {
		return "", fmt.Errorf("remount %s read-write: %w", root, err)
	}
	return delegate(root)
}

func delegate(root string) (string, error) {
	leaf := filepath.Join(root, "executor")
	if err := os.Mkdir(leaf, 0o755); err != nil && !errors.Is(err, fs.ErrExist) { //nolint:gosec // a cgroup directory
		return "", fmt.Errorf("create the executor's leaf cgroup: %w", err)
	}
	pids, err := os.ReadFile(filepath.Join(root, "cgroup.procs"))
	if err != nil {
		return "", err
	}
	// O_CREATE: cgroupfs makes the file itself; a test's plain directory does
	// not.
	procs, err := os.OpenFile(filepath.Join(leaf, "cgroup.procs"), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644) //nolint:gosec // a cgroup file
	if err != nil {
		return "", err
	}
	for pid := range strings.FieldsSeq(string(pids)) {
		// One pid per write: that is how cgroupfs moves a process. One that
		// exited meanwhile is gone already.
		if _, err := procs.WriteString(pid + "\n"); err != nil && !errors.Is(err, syscall.ESRCH) {
			_ = procs.Close()
			return "", fmt.Errorf("move process %s out of the root cgroup: %w", pid, err)
		}
	}
	if err := procs.Close(); err != nil {
		return "", err
	}
	parent := filepath.Join(root, sandboxCgroup)
	if err := os.WriteFile(filepath.Join(root, "cgroup.subtree_control"), []byte(controllers), 0o644); err != nil { //nolint:gosec // a cgroup file
		return "", fmt.Errorf("enable %s: %w", controllers, err)
	}
	if err := os.Mkdir(parent, 0o755); err != nil && !errors.Is(err, fs.ErrExist) { //nolint:gosec // a cgroup directory
		return "", err
	}
	if err := os.WriteFile(filepath.Join(parent, "cgroup.subtree_control"), []byte(controllers), 0o644); err != nil { //nolint:gosec // a cgroup file
		return "", fmt.Errorf("enable %s for sandboxes: %w", controllers, err)
	}
	return "/" + sandboxCgroup, nil
}
