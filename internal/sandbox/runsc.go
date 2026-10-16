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
	"slices"
	"strings"
	"time"

	specs "github.com/opencontainers/runtime-spec/specs-go"
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
	// Mounts are bound into every sandbox beside its job's own: the CA
	// paths Env forwards (trustMounts).
	Mounts []Mount
}

// NewRuntime returns a Runtime whose sandboxes get baseEnvironment, the CA
// paths it names, and network. It fails when one of those paths can't be
// mounted.
func NewRuntime(runsc, state, guestRoot, cgroupFS, cgroups string, limits Limits, network *Network) (*Runtime, error) {
	mounts, err := trustMounts(os.LookupEnv)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		Runsc: runsc, State: state, GuestRoot: guestRoot,
		CgroupFS: cgroupFS, Cgroups: cgroups, Limits: limits,
		Env: baseEnvironment(os.LookupEnv), Network: network, Mounts: mounts,
	}, nil
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
	_, err := r.runscOutput(ctx, args...)
	return err
}

// runscOutput runs a runsc command and returns what it printed.
func (r *Runtime) runscOutput(ctx context.Context, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, r.Runsc, args...) //nolint:gosec // our own runsc, our own arguments
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s %s: %w: %s", r.Runsc, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
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
	config, err := json.Marshal(r.spec(job, id, network))
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
	waited, waitErr := r.runscOutput(context.WithoutCancel(ctx), r.args("wait", id)...)
	oom := r.oomKilled(id)
	// Closes the sandbox's ends of the pipes, should it still hold them, so
	// the reader reaches the end of the stream.
	_ = r.runscWithTimeout(ctx, r.args("delete", "--force", id)...)
	result := <-frames
	if errors.Is(result.err, errNoResult) {
		// The body's stderr is the one create handed the sandbox, so a body
		// that died left its reason (a panic's trace, say) in create's log,
		// which goes with the bundle.
		result.err = fmt.Errorf("%w (%s)%s", errNoResult, waitStatus(waited), logHead(createLog.Name()))
	}

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

// waitStatus reads how the sandbox's process ended from runsc wait's report,
// which it prints rather than exits with: the exit code, or 128 plus the
// signal that killed it.
func waitStatus(waited []byte) string {
	var report struct {
		ExitStatus *int `json:"exitStatus"`
	}
	if json.Unmarshal(waited, &report) != nil || report.ExitStatus == nil {
		return "exit status unknown"
	}
	return fmt.Sprintf("exit status %d", *report.ExitStatus)
}

// stderrHeadSize bounds what of a sandbox's stderr reaches an error. A Go
// panic says what it was first, before its goroutines' traces.
const stderrHeadSize = 4 << 10

// logHead is the start of the log at name, as ": " and the text, or nothing
// when it is empty. Bounded: the sandbox wrote it.
func logHead(name string) string {
	file, err := os.Open(name) //nolint:gosec // a log in the sandbox's own bundle
	if err != nil {
		return ""
	}
	defer file.Close()
	head, _ := io.ReadAll(io.LimitReader(file, stderrHeadSize))
	if text := strings.TrimSpace(string(head)); text != "" {
		return ": " + text
	}
	return ""
}

// spec is the bundle configuration for job in the sandbox id: the job's mounts
// after every sandbox's.
func (r *Runtime) spec(job Job, id string, network sandboxNetwork) *specs.Spec {
	job.Mounts = append(slices.Clone(r.Mounts), job.Mounts...)
	return ociSpec(job, r.GuestRoot, r.Env, path.Join(r.Cgroups, id), r.Limits, network)
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
