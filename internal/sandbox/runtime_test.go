package sandbox

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeRunsc is a runsc whose create leaves a process behind holding its
// stderr, as runsc's own sandbox and gofer do, and whose start fails.
const fakeRunsc = `#!/bin/sh
for arg; do subcommand=$arg; case $arg in create|start|wait|kill|delete) break ;; esac; done
case $subcommand in
create) sleep 30 </dev/null >/dev/null & exit 0 ;;
start) echo "start failed" >&2; exit 1 ;;
esac
exit 0
`

// create returns once runsc create has, not once everything it started has
// let go of its stderr: that is the sandbox's whole life, and it has not been
// started yet.
func TestRunDoesNotWaitForTheSandboxToReleaseCreatesStderr(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	runsc := filepath.Join(dir, "runsc")
	if err := os.WriteFile(runsc, []byte(fakeRunsc), 0o700); err != nil { //nolint:gosec // an executable test double
		t.Fatal(err)
	}
	runtime := &Runtime{Runsc: runsc, State: filepath.Join(dir, "state"), Limits: Limits{MemoryMiB: 1, CPUs: 1, Pids: 1}}

	done := make(chan error, 1)
	go func() {
		_, err := runtime.Run(context.Background(), Job{Activity: "run-terraform"}, io.Discard)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "start failed") {
			t.Fatalf("error = %v, want start's", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run waited on create's stderr")
	}
}

func testJob() Job {
	return Job{
		Activity: "run-terraform",
		Request:  map[string]string{"secret": "do-not-put-me-in-the-spec"},
		Mounts:   []Mount{{Path: "/runs/t/r/workspace"}, {Path: "/runs/t/r/logs", ReadOnly: true}},
		Groups:   []uint32{10001},
	}
}

func TestOCISpecRunsTheBodyUnprivileged(t *testing.T) {
	t.Parallel()

	spec := ociSpec(testJob(), "/opt/openplan/guest", []string{"PATH=/bin"}, "/sandboxes/x", Limits{MemoryMiB: 2048, CPUs: 2, Pids: 1024}, sandboxNetwork{})

	if want := []string{guestBinary, ActivitySubcommand, "run-terraform"}; !slices.Equal(spec.Process.Args, want) {
		t.Fatalf("args = %q, want %q", spec.Process.Args, want)
	}
	if spec.Process.User.UID != SandboxUID || spec.Process.User.GID != SandboxUID {
		t.Fatalf("user = %+v, want %d", spec.Process.User, SandboxUID)
	}
	if !slices.Equal(spec.Process.User.AdditionalGids, []uint32{10001}) {
		t.Fatalf("groups = %v, want [10001]", spec.Process.User.AdditionalGids)
	}
	caps := spec.Process.Capabilities
	if len(caps.Bounding)+len(caps.Effective)+len(caps.Permitted)+len(caps.Inheritable)+len(caps.Ambient) != 0 {
		t.Fatalf("capabilities = %+v, want none", caps)
	}
	if !spec.Process.NoNewPrivileges || !spec.Root.Readonly {
		t.Fatal("want no_new_privs and a read-only root")
	}
}

// Credentials travel over stdin only: the bundle on disk never holds them.
func TestOCISpecHoldsOnlyTheBaseEnvironment(t *testing.T) {
	t.Parallel()

	spec := ociSpec(testJob(), "/guest", []string{"PATH=/bin"}, "/sandboxes/x", Limits{MemoryMiB: 1, CPUs: 1, Pids: 1}, sandboxNetwork{})
	encoded, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "do-not-put-me-in-the-spec") {
		t.Fatal("the request reached the bundle")
	}
	if !slices.Equal(spec.Process.Env, []string{"PATH=/bin"}) {
		t.Fatalf("env = %q, want only the base environment", spec.Process.Env)
	}
}

func TestOCISpecMountsAtTheSamePath(t *testing.T) {
	t.Parallel()

	spec := ociSpec(testJob(), "/guest", nil, "/sandboxes/x", Limits{MemoryMiB: 1, CPUs: 1, Pids: 1}, sandboxNetwork{})
	var found int
	for _, m := range spec.Mounts {
		switch m.Destination {
		case "/runs/t/r/workspace":
			found++
			if m.Source != m.Destination || !slices.Contains(m.Options, "rw") {
				t.Fatalf("workspace mount = %+v, want rw at the same path", m)
			}
		case "/runs/t/r/logs":
			found++
			if !slices.Contains(m.Options, "ro") {
				t.Fatalf("logs mount = %+v, want ro", m)
			}
		}
	}
	if found != 2 {
		t.Fatalf("found %d of the job's mounts, want 2", found)
	}
}

// Every sandbox gets the runtime's own mounts, the CA paths, beside its job's.
func TestRuntimeSpecAddsTheTrustMounts(t *testing.T) {
	t.Parallel()

	r := &Runtime{GuestRoot: "/guest", Cgroups: "/sandboxes", Limits: Limits{MemoryMiB: 1, CPUs: 1, Pids: 1}, Mounts: []Mount{{Path: "/etc/ssl/corp.pem", ReadOnly: true}}}
	spec := r.spec(testJob(), "x", sandboxNetwork{})
	var ca, workspace bool
	for _, m := range spec.Mounts {
		switch m.Destination {
		case "/etc/ssl/corp.pem":
			ca = m.Source == m.Destination && slices.Contains(m.Options, "ro")
		case "/runs/t/r/workspace":
			workspace = true
		}
	}
	if !ca || !workspace {
		t.Fatalf("mounts = %+v, want the CA read-only and the job's workspace", spec.Mounts)
	}
	if spec.Linux.CgroupsPath != "/sandboxes/x" {
		t.Fatalf("cgroups path = %q, want /sandboxes/x", spec.Linux.CgroupsPath)
	}
}

func TestOCISpecSetsTheLimits(t *testing.T) {
	t.Parallel()

	spec := ociSpec(testJob(), "/guest", nil, "/sandboxes/x", Limits{MemoryMiB: 2048, CPUs: 2, Pids: 1024}, sandboxNetwork{})
	resources := spec.Linux.Resources
	if *resources.Memory.Limit != 2048<<20 || resources.Pids.Limit != 1024 || *resources.CPU.Quota != 200_000 {
		t.Fatalf("resources = memory %d, pids %d, quota %d", *resources.Memory.Limit, resources.Pids.Limit, *resources.CPU.Quota)
	}
	// OCI's swap is memory plus swap: equal to the limit means no swap, so a
	// sandbox can't page past its limit onto the host's swap.
	if resources.Memory.Swap == nil || *resources.Memory.Swap != *resources.Memory.Limit {
		t.Fatalf("memory swap = %v, want the memory limit", resources.Memory.Swap)
	}
	if spec.Linux.CgroupsPath != "/sandboxes/x" {
		t.Fatalf("cgroups path = %q", spec.Linux.CgroupsPath)
	}
	for _, ns := range spec.Linux.Namespaces {
		if ns.Type == "network" {
			t.Fatal("a bundle without a network named a network namespace")
		}
	}
}

func TestRuntimeArgsNeverIgnoreCgroups(t *testing.T) {
	t.Parallel()

	args := (&Runtime{State: "/state"}).args("create", "--bundle", "/b", "id")
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "ignore-cgroups") {
		t.Fatalf("args = %q: limits must apply or the sandbox must not start", args)
	}
	for _, want := range []string{"--platform systrap", "--overlay2 root:memory", "--network sandbox", "create --bundle /b id"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args = %q, want %q", args, want)
		}
	}
}

// Review focus 1.
func TestSandboxIDsAreUnique(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	for range 1000 {
		id, err := newSandboxID()
		if err != nil {
			t.Fatal(err)
		}
		if seen[id] {
			t.Fatalf("id %q repeated", id)
		}
		seen[id] = true
	}
}

func TestOOMKilledReadsMemoryEvents(t *testing.T) {
	t.Parallel()

	if !oomKilled("low 0\nhigh 0\nmax 3\noom 1\noom_kill 1\n") {
		t.Fatal("oom_kill 1 not seen")
	}
	if oomKilled("low 0\noom 0\noom_kill 0\n") {
		t.Fatal("oom_kill 0 read as a kill")
	}
}

// With the host's cgroup namespace, the cgroup mount is the host's own
// hierarchy, and delegating would move the host's processes. The executor
// must see itself at its namespace's root, or in the leaf it moved itself to.
func TestDelegationNeedsAPrivateCgroupNamespace(t *testing.T) {
	t.Parallel()

	for content, private := range map[string]bool{
		"0::/\n":         true,
		"0::/executor\n": true,
		"0::/system.slice/docker-0123abcd.scope\n": false,
		"0::/kubepods/burstable/pod1/0123abcd\n":   false,
		// cgroup v1: no unified hierarchy to delegate.
		"4:memory:/docker/0123abcd\n1:name=systemd:/docker/0123abcd\n": false,
	} {
		if err := checkCgroupNamespace(content); (err == nil) != private {
			t.Errorf("%q: error = %v, want accepted %v", content, err, private)
		}
	}
}

func TestDelegateMovesProcessesAndEnablesControllers(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "cgroup.procs"), []byte("1\n7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	parent, err := delegate(root)
	if err != nil {
		t.Fatalf("delegate returned error: %v", err)
	}
	if parent != "/sandboxes" {
		t.Fatalf("parent = %q, want /sandboxes", parent)
	}
	moved, _ := os.ReadFile(filepath.Join(root, "executor", "cgroup.procs"))
	if string(moved) != "1\n7\n" {
		t.Fatalf("moved processes = %q, want both", moved)
	}
	for _, dir := range []string{root, filepath.Join(root, "sandboxes")} {
		control, _ := os.ReadFile(filepath.Join(dir, "cgroup.subtree_control"))
		if string(control) != controllers {
			t.Fatalf("%s subtree_control = %q, want %q", dir, control, controllers)
		}
	}
}

// diesWithoutResult is a runsc whose sandbox writes nothing to stdout, its
// body's stderr landing in create's, and exits 2.
const diesWithoutResult = `#!/bin/sh
for arg; do subcommand=$arg; case $arg in create|start|wait|kill|delete) break ;; esac; done
case $subcommand in
create) echo "panic: the body fell over" >&2 ;;
wait) echo '{"id":"x","exitStatus":2}' ;;
esac
exit 0
`

// A body that dies without a result says why: its stderr and exit status,
// not only that there was no result.
func TestRunReportsWhyTheBodyDied(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	runsc := filepath.Join(dir, "runsc")
	if err := os.WriteFile(runsc, []byte(diesWithoutResult), 0o700); err != nil { //nolint:gosec // an executable test double
		t.Fatal(err)
	}
	runtime := &Runtime{Runsc: runsc, State: filepath.Join(dir, "state"), Limits: Limits{MemoryMiB: 1, CPUs: 1, Pids: 1}}

	_, err := runtime.Run(context.Background(), Job{Activity: "run-terraform"}, io.Discard)
	if err == nil {
		t.Fatal("Run returned no error for a sandbox that wrote no result")
	}
	for _, want := range []string{"without a result", "exit status 2", "panic: the body fell over"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want %q in it", err, want)
		}
	}
}
