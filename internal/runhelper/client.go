package runhelper

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/vishu42/openplan/internal/runuser"
)

// Client starts the helper as a session's pool user.
type Client struct {
	// Command is the helper's executable and leading arguments: the
	// executor's own binary and Subcommand, or a test binary in tests.
	Command []string
}

// NewClient returns a client that starts the helper from the running
// executor's own binary.
func NewClient() (Client, error) {
	executable, err := os.Executable()
	if err != nil {
		return Client{}, fmt.Errorf("find the executor binary: %w", err)
	}
	return Client{Command: []string{executable, Subcommand}}, nil
}

// Kill kills every process of user.
func (c Client) Kill(ctx context.Context, user runuser.User) error {
	return c.run(ctx, user, nil, nil, "kill")
}

// Reclaim kills every process of user, clears its scratch files, and empties
// workspace when it is not "". Then it checks /proc itself: the helper runs as
// user, so any process of user can tamper with it (ptrace, where the host
// allows it) and have it exit 0 having killed nothing. A survivor fails the
// reclaim, which keeps user out of the pool.
func (c Client) Reclaim(ctx context.Context, user runuser.User, workspace string) error {
	args := []string{}
	if workspace != "" {
		args = append(args, workspace)
	}
	if err := c.run(ctx, user, nil, nil, "reclaim", args...); err != nil {
		return err
	}
	return waitForNoSurvivors(ctx, "/proc", user.UID)
}

// Pack returns the plan bundle packed from dir.
func (c Client) Pack(ctx context.Context, user runuser.User, dir string) ([]byte, error) {
	var bundle bytes.Buffer
	if err := c.run(ctx, user, nil, &bundle, "pack", dir); err != nil {
		return nil, err
	}
	return bundle.Bytes(), nil
}

// Unpack writes bundle's files into dir.
func (c Client) Unpack(ctx context.Context, user runuser.User, dir string, bundle []byte) error {
	return c.run(ctx, user, bytes.NewReader(bundle), nil, "unpack", dir)
}

// CheckRoot reports whether dir is a directory, as user sees it.
func (c Client) CheckRoot(ctx context.Context, user runuser.User, dir string) error {
	return c.run(ctx, user, nil, nil, "check-root", dir)
}

func (c Client) run(ctx context.Context, user runuser.User, stdin io.Reader, stdout io.Writer, op string, args ...string) error {
	credential, err := user.Credential()
	if err != nil {
		return err
	}
	argv := append(append(append([]string{}, c.Command[1:]...), op), args...)
	cmd := exec.CommandContext(ctx, c.Command[0], argv...) //nolint:gosec // the executor's own binary
	if credential != nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: credential}
	}
	// Not nil: a nil Env hands the helper the executor's own environment,
	// readable by every process of the pool user.
	cmd.Env = []string{}
	cmd.Dir = "/"
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("helper %s as %s: %w: %s", op, user.Name, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
