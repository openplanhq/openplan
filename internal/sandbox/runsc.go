package sandbox

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Runtime runs each job in a fresh gVisor sandbox with runsc.
type Runtime struct {
	// Runsc is the runsc binary; its gvisor-bin/ sidecars sit beside it.
	Runsc string
	// State holds runsc's own state and each sandbox's bundle, on ephemeral
	// storage.
	State string
	// GuestRoot is the read-only root every sandbox boots from.
	GuestRoot string
	// CgroupFS is the cgroup2 mount, and Cgroups the parent DelegateCgroups
	// returned.
	CgroupFS string
	Cgroups  string
	Limits   Limits
	// Env is every sandbox's environment: baseEnvironment, never this
	// process's own.
	Env []string
	// Network gives each sandbox its way out. Without one, a sandbox has no
	// network at all.
	Network *Network
}

// NewRuntime returns a Runtime whose sandboxes get baseEnvironment and
// network.
func NewRuntime(runsc, state, guestRoot, cgroupFS, cgroups string, limits Limits, network *Network) *Runtime {
	return &Runtime{
		Runsc: runsc, State: state, GuestRoot: guestRoot,
		CgroupFS: cgroupFS, Cgroups: cgroups, Limits: limits,
		Env: baseEnvironment(os.LookupEnv), Network: network,
	}
}

// runscTimeout bounds every runsc command but the wait for a sandbox's body.
const runscTimeout = 30 * time.Second

func (r *Runtime) args(subcommand string, args ...string) []string {
	return append([]string{
		"--root", filepath.Join(r.State, "runsc"),
		// No KVM: systrap runs wherever Linux 5.6 or later does.
		"--platform", "systrap",
		// Writes outside the mounts go to memory private to the sandbox.
		"--overlay2", "root:memory",
		// gVisor's own network stack, in the namespace the bundle names.
		"--network", "sandbox",
		subcommand,
	}, args...)
}

func (r *Runtime) runsc(ctx context.Context, args ...string) error {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, r.Runsc, args...) //nolint:gosec // our own runsc, our own arguments
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", r.Runsc, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// runscWithTimeout runs a runsc command that must finish even once ctx is
// cancelled, as the kill and delete after a cancellation must, but not hang.
func (r *Runtime) runscWithTimeout(ctx context.Context, args ...string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runscTimeout)
	defer cancel()
	return r.runsc(ctx, args...)
}

// Run starts a fresh sandbox for job and relays until it exits. The sandbox
// is created on pipes it keeps until it exits, then started, waited for and
// deleted as separate steps: runsc run would delete it on exit, and with it
// the cgroup whose memory.events says whether it was killed for memory.
func (r *Runtime) Run(ctx context.Context, job Job, output io.Writer) (json.RawMessage, error) {
	id, err := newSandboxID()
	if err != nil {
		return nil, err
	}
	bundle := filepath.Join(r.State, "bundles", id)
	if err := os.MkdirAll(bundle, 0o700); err != nil {
		return nil, err
	}
	defer os.RemoveAll(bundle)
	var network sandboxNetwork
	if r.Network != nil {
		att, err := r.Network.attach(id)
		if err != nil {
			return nil, err
		}
		// Deferred before the delete below, so it runs after it: the
		// namespace is freed once the sandbox has left it.
		defer r.Network.release(att)
		network = sandboxNetwork{Namespace: att.namespace, ResolvConf: r.Network.resolvConf}
	}
	// Registered before create: a create that fails halfway may leave runsc
	// state for this id. Deleting an id runsc never made fails harmlessly.
	defer func() { _ = r.runscWithTimeout(ctx, r.args("delete", "--force", id)...) }()
	config, err := json.Marshal(ociSpec(job, r.GuestRoot, r.Env, path.Join(r.Cgroups, id), r.Limits, network))
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(bundle, "config.json"), config, 0o600); err != nil {
		return nil, err
	}
	request, err := json.Marshal(job.Request)
	if err != nil {
		return nil, fmt.Errorf("encode the request: %w", err)
	}

	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer stdinWrite.Close()
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		_ = stdinRead.Close()
		return nil, err
	}
	defer stdoutRead.Close()

	// The pipes are *os.File, so runsc hands them straight to the sandbox,
	// which holds them from here on; ours close once create returns.
	//
	// create's stderr is a file, not a pipe: the sandbox and gofer processes
	// create starts inherit it and hold it for the sandbox's whole life, so a
	// pipe would not close until the sandbox exits, and Run would wait on it
	// before ever starting it.
	createLog, err := os.Create(filepath.Join(bundle, "create.log"))
	if err != nil {
		_ = stdinRead.Close()
		_ = stdoutWrite.Close()
		return nil, err
	}
	// Not cancelled with ctx: a create cut short can leave a sandbox half made.
	createCtx, cancelCreate := context.WithTimeout(context.WithoutCancel(ctx), runscTimeout)
	create := exec.CommandContext(createCtx, r.Runsc, r.args("create", "--bundle", bundle, id)...) //nolint:gosec // our own runsc
	create.Stdin, create.Stdout, create.Stderr = stdinRead, stdoutWrite, createLog
	err = create.Run()
	cancelCreate()
	_ = createLog.Close()
	_ = stdinRead.Close()
	_ = stdoutWrite.Close()
	if err != nil {
		message, _ := os.ReadFile(createLog.Name())
		return nil, fmt.Errorf("create the activity's sandbox: %w: %s", err, strings.TrimSpace(string(message)))
	}

	type framesResult struct {
		message resultMessage
		err     error
	}
	frames := make(chan framesResult, 1)
	go func() {
		message, err := readFrames(stdoutRead, output)
		if err != nil && !errors.Is(err, errNoResult) {
			// A sandbox writing garbage, or past its result, or output this
			// side can't store, is stopped rather than left to block.
			_ = r.runscWithTimeout(ctx, r.args("kill", "--all", id, "KILL")...)
		}
		frames <- framesResult{message, err}
	}()
	stop := context.AfterFunc(ctx, func() { _ = r.runscWithTimeout(ctx, r.args("kill", "--all", id, "KILL")...) })
	defer stop()

	if err := r.runscWithTimeout(ctx, r.args("start", id)...); err != nil {
		return nil, err
	}
	go func() {
		_, _ = stdinWrite.Write(request)
		_ = stdinWrite.Close()
	}()
	// No timeout: the activity's own timeout cancels ctx, which kills it.
	waitErr := r.runsc(context.WithoutCancel(ctx), r.args("wait", id)...)
	oom := r.oomKilled(id)
	// Closes the sandbox's ends of the pipes, should it still hold them, so
	// the reader reaches the end of the stream.
	_ = r.runscWithTimeout(ctx, r.args("delete", "--force", id)...)
	result := <-frames

	switch {
	case ctx.Err() != nil:
		return nil, fmt.Errorf("activity sandbox: %w", ctx.Err())
	case oom:
		return nil, fmt.Errorf("the activity's sandbox ran out of memory (limit %d MiB)", r.Limits.MemoryMiB)
	case result.err != nil && waitErr != nil:
		return nil, fmt.Errorf("%w (%w)", result.err, waitErr)
	case result.err != nil:
		return nil, result.err
	}
	return decodeResult(result.message)
}

// oomKilled reports whether the kernel killed anything in the sandbox's
// cgroup for memory.
func (r *Runtime) oomKilled(id string) bool {
	events, err := os.ReadFile(filepath.Join(r.CgroupFS, r.Cgroups, id, "memory.events"))
	return err == nil && oomKilled(string(events))
}

func oomKilled(events string) bool {
	for line := range strings.SplitSeq(events, "\n") {
		name, value, ok := strings.Cut(line, " ")
		if ok && name == "oom_kill" && strings.TrimSpace(value) != "0" {
			return true
		}
	}
	return false
}

// newSandboxID names one sandbox, unique among every run on this executor.
func newSandboxID() (string, error) {
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return "openplan-" + hex.EncodeToString(random[:]), nil
}
