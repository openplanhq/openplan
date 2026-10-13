package isolation

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// maxHelperStderr bounds what the executor keeps of the helper's stderr, for
// the error it returns. The helper runs as the session's user, so a process of
// that user can tamper with it and write as much as it likes.
const maxHelperStderr = 64 << 10

// helperClient starts the helper as a session's pool user: this binary again,
// with HelperSubcommand, the usual way for a Go program to run code as
// another user, since Go cannot fork safely.
type helperClient struct {
	// command is the helper's executable and leading arguments: the
	// executor's own binary and HelperSubcommand, or a test binary in tests.
	command []string
	// procRoot is where reclaim looks for survivors: /proc, or a fake in tests.
	procRoot string
}

// newHelperClient returns a client that starts the helper from the running
// executor's own binary.
func newHelperClient() (helperClient, error) {
	executable, err := os.Executable()
	if err != nil {
		return helperClient{}, fmt.Errorf("find the executor binary: %w", err)
	}
	return helperClient{command: []string{executable, HelperSubcommand}, procRoot: "/proc"}, nil
}

func (c helperClient) kill(ctx context.Context, u user) error {
	return c.run(ctx, u, nil, nil, "kill")
}

// reclaim runs the helper's reclaim, then checks /proc itself: the helper runs
// as u, so any process of u can tamper with it (ptrace, where the host allows
// it) and have it exit 0 having killed nothing. A survivor fails the reclaim,
// which keeps u out of the pool.
func (c helperClient) reclaim(ctx context.Context, u user, workspace string) error {
	args := []string{}
	if workspace != "" {
		args = append(args, workspace)
	}
	if err := c.run(ctx, u, nil, nil, "reclaim", args...); err != nil {
		return err
	}
	return waitForNoSurvivors(ctx, c.procRoot, u.uid)
}

// readFiles decodes the helper's answer as it streams, with the same bounds
// the helper puts on what it reads from the executor, so the executor holds
// each file once rather than the whole answer first. It keeps only the files
// it asked for: the helper may have been tampered with, so its answer is the
// session's to choose.
func (c helperClient) readFiles(ctx context.Context, u user, dir string, names []string) (map[string][]byte, error) {
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := c.run(ctx, u, nil, writer, "read-files", append([]string{dir}, names...)...)
		writer.CloseWithError(err)
		done <- err
	}()
	files, decodeErr := decodeFiles(reader)
	// Read to the end whatever the outcome: a helper left blocked on a full
	// pipe would never exit.
	_, _ = io.Copy(io.Discard, reader)
	if err := <-done; err != nil {
		return nil, err
	}
	if decodeErr != nil {
		return nil, fmt.Errorf("helper read-files as %s: %w", u.name, decodeErr)
	}
	for name := range files {
		if !slices.Contains(names, name) {
			return nil, fmt.Errorf("helper read-files as %s: sent %q, which was not asked for", u.name, name)
		}
	}
	return files, nil
}

func (c helperClient) writeFiles(ctx context.Context, u user, dir string, files map[string][]byte) error {
	archive, err := encodeFiles(files)
	if err != nil {
		return err
	}
	return c.run(ctx, u, bytes.NewReader(archive), nil, "write-files", dir)
}

func (c helperClient) checkDir(ctx context.Context, u user, path string) error {
	return c.run(ctx, u, nil, nil, "check-dir", path)
}

func (c helperClient) run(ctx context.Context, u user, stdin io.Reader, stdout io.Writer, op string, args ...string) error {
	credential, err := u.credential()
	if err != nil {
		return err
	}
	argv := append(append(append([]string{}, c.command[1:]...), op), args...)
	cmd := exec.CommandContext(ctx, c.command[0], argv...) //nolint:gosec // the executor's own binary
	if credential != nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: credential}
	}
	// Not nil: a nil Env hands the helper the executor's own environment,
	// readable by every process of the pool user.
	cmd.Env = []string{}
	cmd.Dir = "/"
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	stderr := &cappedBuffer{limit: maxHelperStderr}
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("helper %s as %s: %w: %s", op, u.name, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// cappedBuffer keeps the first limit bytes written to it and drops the rest,
// noting that it did. It never fails a write: a writer that failed would stop
// os/exec draining the pipe, and the helper would block on a full pipe
// until its context ended. The buffer is a field, not embedded: an embedded
// bytes.Buffer would lend it ReadFrom, which io.Copy prefers to Write, and
// the cap would never run.
type cappedBuffer struct {
	buf        bytes.Buffer
	limit      int
	overflowed bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.buf.Len(); len(p) > room {
		b.overflowed = true
		if room > 0 {
			b.buf.Write(p[:room])
		}
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *cappedBuffer) String() string { return b.buf.String() }
func (b *cappedBuffer) Len() int       { return b.buf.Len() }

// survivorPoll is how often waitForNoSurvivors looks again. SIGKILL is
// delivered asynchronously, so a reclaimed user's processes can take a moment
// to go.
const survivorPoll = 50 * time.Millisecond

// survivorTimeout bounds the wait when ctx has no sooner deadline: a killed
// process leaves within milliseconds unless it is stuck in the kernel.
const survivorTimeout = 10 * time.Second

// waitForNoSurvivors returns once uid has no live process under procRoot, and
// fails if one outlasts ctx or survivorTimeout. It runs in the executor, not
// the helper, because only the executor's own look can be trusted: the helper
// runs as uid, so a process of uid can tamper with it.
func waitForNoSurvivors(ctx context.Context, procRoot string, uid uint32) error {
	ctx, cancel := context.WithTimeout(ctx, survivorTimeout)
	defer cancel()
	for {
		pids, err := survivors(procRoot, uid)
		if err != nil {
			return fmt.Errorf("look for processes of uid %d: %w", uid, err)
		}
		if len(pids) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("processes %v of uid %d are still running", pids, uid)
		case <-time.After(survivorPoll):
		}
	}
}

// survivors returns the pids of uid's live processes under procRoot. A zombie
// is dead and holds nothing, so it does not count.
func survivors(procRoot string, uid uint32) ([]int, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, err
	}
	want := strconv.FormatUint(uint64(uid), 10)
	var pids []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		// Gone since the listing: nothing left to count.
		status, err := os.ReadFile(filepath.Join(procRoot, entry.Name(), "status"))
		if err != nil {
			continue
		}
		if ownedLive(string(status), want) {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

// ownedLive reports whether a /proc/<pid>/status describes a live process
// whose real or effective uid is uid.
func ownedLive(status, uid string) bool {
	var owned, dead bool
	for line := range strings.SplitSeq(status, "\n") {
		name, value, _ := strings.Cut(line, ":")
		fields := strings.Fields(value)
		switch {
		case name == "State" && len(fields) > 0:
			dead = fields[0] == "Z" || fields[0] == "X"
		case name == "Uid" && len(fields) > 1:
			owned = fields[0] == uid || fields[1] == uid
		}
	}
	return owned && !dead
}
