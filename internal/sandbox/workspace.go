package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// SandboxUID is the unprivileged user every sandbox's process runs as, and so
// the owner of everything a sandbox writes. Nothing outside a sandbox runs as
// it except the cleanup that deletes what it wrote.
const SandboxUID = 70000

// Workspaces lays out each run's directory under Root: workspace/, which the
// run's sandboxes share, and logs/ beside it, where the worker spools output
// that no sandbox running tofu mounts. Root is ephemeral storage, so an
// executor that restarts starts with none.
type Workspaces struct {
	Root string
}

func (w Workspaces) runDir(tenant, run string) (string, error) {
	if strings.TrimSpace(w.Root) == "" {
		return "", errors.New("run root is required")
	}
	if err := checkName(tenant); err != nil {
		return "", fmt.Errorf("tenant: %w", err)
	}
	if err := checkName(run); err != nil {
		return "", fmt.Errorf("run: %w", err)
	}
	return filepath.Join(w.Root, tenant, run), nil
}

// Check makes the run root and refuses one mounted noexec, as Docker mounts
// a tmpfs by default. Providers are programs tofu installs in the workspace,
// and gVisor can't map a program from a noexec mount, so every provider would
// segfault as it starts. The executor calls it once at startup.
func (w Workspaces) Check() error {
	if err := os.MkdirAll(w.Root, 0o755); err != nil { //nolint:gosec // run directories below are 0711
		return err
	}
	var stat unix.Statfs_t
	if err := unix.Statfs(w.Root, &stat); err != nil {
		return err
	}
	if noexec(stat.Flags) {
		return fmt.Errorf("%s is mounted noexec, so providers can't run there: mount it with exec (Compose tmpfs: add exec to its options)", w.Root)
	}
	return nil
}

func noexec(flags int64) bool { return flags&unix.ST_NOEXEC != 0 }

// Workspace is the run's workspace, derived from its identity alone.
func (w Workspaces) Workspace(tenant, run string) (string, error) {
	dir, err := w.runDir(tenant, run)
	return filepath.Join(dir, "workspace"), err
}

// LogsDir is where the worker spools the run's command output.
func (w Workspaces) LogsDir(tenant, run string) (string, error) {
	dir, err := w.runDir(tenant, run)
	return filepath.Join(dir, "logs"), err
}

// Prepare lays out a fresh run directory, removing whatever an earlier
// attempt left, and hands the workspace to SandboxUID.
func (w Workspaces) Prepare(ctx context.Context, tenant, run string) (string, error) {
	if err := w.Remove(ctx, tenant, run); err != nil {
		return "", err
	}
	dir, err := w.runDir(tenant, run)
	if err != nil {
		return "", err
	}
	// 0711: SandboxUID walks down to its own workspace, but lists no one's.
	if err := os.MkdirAll(dir, 0o711); err != nil { //nolint:gosec // see above
		return "", fmt.Errorf("create the run directory: %w", err)
	}
	workspace := filepath.Join(dir, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		return "", fmt.Errorf("create the workspace: %w", err)
	}
	// 0755: UploadLog's sandbox reads the spool as SandboxUID.
	if err := os.Mkdir(filepath.Join(dir, "logs"), 0o755); err != nil { //nolint:gosec // see above
		return "", fmt.Errorf("create the log spool: %w", err)
	}
	// Off root, in tests, the workspace stays the current user's.
	if os.Geteuid() == 0 {
		if err := os.Chown(workspace, SandboxUID, SandboxUID); err != nil {
			return "", fmt.Errorf("hand the workspace to the sandbox user: %w", err)
		}
	}
	return workspace, nil
}

// Remove deletes a run's directory. The workspace's contents are SandboxUID's,
// in a directory root cannot enter without capabilities the executor doesn't
// keep, so they are deleted as SandboxUID first.
func (w Workspaces) Remove(ctx context.Context, tenant, run string) error {
	dir, err := w.runDir(tenant, run)
	if err != nil {
		return err
	}
	workspace := filepath.Join(dir, "workspace")
	if _, err := os.Lstat(workspace); err == nil {
		cmd := exec.CommandContext(ctx, "find", workspace, "-mindepth", "1", "-delete") //nolint:gosec // workspace is derived from checked IDs under the run root
		if os.Geteuid() == 0 {
			cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: SandboxUID, Gid: SandboxUID}}
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("empty the workspace: %w: %s", err, out)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.RemoveAll(dir)
}
