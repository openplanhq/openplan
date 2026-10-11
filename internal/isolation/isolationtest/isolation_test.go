//go:build isolation

// Package isolationtest checks the isolation package's promises (#331) in the
// executor image, as root with only SETUID, SETGID and CHOWN, through the
// package's own API and the activities built on it. make isolation-test runs
// it; go test ./... skips it, having no build tag.
package isolationtest

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/vishu42/openplan/internal/activities"
	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/isolation"
	"github.com/vishu42/openplan/internal/runner"
	"github.com/vishu42/openplan/internal/runseal"
)

// subvertedMarker, while it exists, makes this binary's helper exit 0 having
// done nothing: a helper a session's process tampered with. It is root's, in
// /tmp, so pool users can see it but neither make nor remove it.
const subvertedMarker = "/tmp/openplan-isolation-subverted-helper"

// TestMain doubles as the helper: isolation.New starts this binary with
// isolation.HelperSubcommand, as the executor starts its own.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == isolation.HelperSubcommand {
		if _, err := os.Stat(subvertedMarker); err == nil {
			os.Exit(0)
		}
		os.Exit(isolation.HelperMain(os.Args[2:], os.Stdin, os.Stdout, os.Stderr))
	}
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "isolation tests run as root in the executor image: make isolation-test")
		os.Exit(1)
	}
	os.Exit(m.Run())
}

type session struct {
	session   *isolation.Session
	workspace string
	uid       uint32
}

// newSessions returns the executor's own Manager, New's checks and all, over a
// fresh run root.
func newSessions(t *testing.T) (*isolation.Manager, string) {
	t.Helper()
	// Under the image's 0711 run root, so pool users can walk to their workspace.
	runRoot, err := os.MkdirTemp("/var/lib/openplan/runs", "isolation-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(runRoot, 0o711); err != nil {
		t.Fatal(err)
	}
	sessions, err := isolation.New(isolation.Config{RunRoot: runRoot, MaxSessions: 20})
	if err != nil {
		t.Fatal(err)
	}
	return sessions, runRoot
}

func newActivities(t *testing.T) (*activities.TemplateRunActivities, *isolation.Manager, string) {
	t.Helper()
	sessions, runRoot := newSessions(t)
	return activities.NewTemplateRunActivities(runRoot, nil, nil, runseal.NewKeyRing(), sessions), sessions, runRoot
}

func prepare(t *testing.T, a *activities.TemplateRunActivities, sessions *isolation.Manager, run string) session {
	t.Helper()
	output, err := a.PrepareWorkspace(context.Background(), domain.PrepareWorkspaceActivityInput{TenantID: "tenant", RunID: domain.TemplateRunID(run)})
	if err != nil {
		t.Fatal(err)
	}
	s, err := sessions.Session(domain.RunKeyID("tenant", domain.TemplateRunID(run)))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(output.WorkspacePath)
	if err != nil {
		t.Fatal(err)
	}
	return session{session: s, workspace: output.WorkspacePath, uid: info.Sys().(*syscall.Stat_t).Uid}
}

func (s session) ctx(ctx context.Context) context.Context {
	return s.session.Context(ctx)
}

// shell runs script as the session's pool user, in its workspace.
func (s session) shell(t *testing.T, script string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := isolation.SessionExecutor().Run(s.ctx(context.Background()), s.workspace, nil, &out, &out, "/bin/sh", "-c", script)
	return out.String(), err
}

func (s session) writeTemplate(t *testing.T, hcl string) {
	t.Helper()
	if out, err := s.shell(t, "cat > main.tf <<'EOF'\n"+hcl+"\nEOF"); err != nil {
		t.Fatalf("write template: %v: %s", err, out)
	}
}

func (s session) tofu(ctx context.Context, command domain.TerraformCommandType, env map[string]string) error {
	_, err := runner.NewLocalProcessRunnerWithExecutor(isolation.SessionExecutor()).Run(s.ctx(ctx), runner.TerraformCommand{
		WorkspacePath: s.workspace, WorkspaceName: "default", Command: command, Environment: env,
		Stdout: os.Stderr, Stderr: os.Stderr,
	})
	return err
}

// startAs starts a sleep as uid directly, not through the session executor,
// which would kill it when the command ends: a process left behind.
func startAs(t *testing.T, uid uint32) {
	t.Helper()
	sleep := exec.Command("sleep", "3600")
	sleep.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: uid, Groups: []uint32{}}}
	if err := sleep.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = sleep.Wait() }()
}

// processesOf returns the pids of uid's processes, read from /proc as root.
func processesOf(uid uint32) []int {
	var pids []int
	entries, _ := os.ReadDir("/proc")
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		status, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "status"))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(status), "\n") {
			if fields := strings.Fields(line); len(fields) > 1 && fields[0] == "Uid:" && fields[1] == strconv.FormatUint(uint64(uid), 10) {
				pids = append(pids, pid)
			}
		}
	}
	return pids
}

func eventually(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Scenarios 1 and 2: run B holds a canary in tofu's environment, and run A,
// concurrently, can neither read it, nor reach B's workspace or processes,
// nor the executor's environment.
func TestBranchesCannotReachEachOtherOrTheExecutor(t *testing.T) {
	a, sessions, _ := newActivities(t)
	runA, runB := prepare(t, a, sessions, "run_a"), prepare(t, a, sessions, "run_b")
	const canary = "leak-me-7f3a"

	runB.writeTemplate(t, `resource "terraform_data" "hold" {
  provisioner "local-exec" {
    command = "sleep 300"
  }
}`)
	if err := runB.tofu(context.Background(), domain.TerraformCommandInit, nil); err != nil {
		t.Fatal(err)
	}
	ctxB, cancelB := context.WithCancel(context.Background())
	doneB := make(chan error, 1)
	go func() {
		doneB <- runB.tofu(ctxB, domain.TerraformCommandApplyAutoApprove, map[string]string{"OPENPLAN_CANARY": canary})
	}()
	// tofu and the sleep at least; the shell may exec sleep in its own place.
	eventually(t, "run B's sleep", func() bool { return len(processesOf(runB.uid)) >= 2 })

	if out, _ := runA.shell(t, "grep -a -r -l "+canary+" /proc/[0-9]*/environ 2>/dev/null; true"); strings.TrimSpace(out) != "" {
		t.Fatalf("run A read run B's environment: %s", out)
	}
	pidB := processesOf(runB.uid)[0]
	for name, script := range map[string]string{
		"the executor's environment": fmt.Sprintf("cat /proc/%d/environ", os.Getpid()),
		"run B's workspace":          "ls " + runB.workspace,
		"a signal to run B":          fmt.Sprintf("kill -9 %d", pidB),
	} {
		if out, err := runA.shell(t, script); err == nil {
			t.Errorf("run A reached %s: %s", name, out)
		}
	}

	// A plan file planted as a symlink to the executor's environment reads
	// nothing: the helper reads it as run A's user.
	if out, err := runA.shell(t, fmt.Sprintf("ln -s /proc/%d/environ tfplan", os.Getpid())); err != nil {
		t.Fatal(out)
	}
	if files, err := runA.session.ReadFiles(context.Background(), runA.workspace, []string{"tfplan"}); err == nil {
		t.Fatalf("ReadFiles read the executor's environment through a symlink: %q", files)
	}

	// Cancelling run B (a timeout) kills its whole branch through the helper.
	cancelB()
	<-doneB
	eventually(t, "run B's branch to die", func() bool { return len(processesOf(runB.uid)) == 0 })
}

// Scenario 3: what a command leaves running dies with it.
func TestNohupDoesNotOutliveItsCommand(t *testing.T) {
	a, sessions, _ := newActivities(t)
	run := prepare(t, a, sessions, "run_nohup")
	run.writeTemplate(t, `resource "terraform_data" "leave" {
  provisioner "local-exec" {
    command = "nohup sleep 3600 >/dev/null 2>&1 &"
  }
}`)
	if err := run.tofu(context.Background(), domain.TerraformCommandInit, nil); err != nil {
		t.Fatal(err)
	}
	if err := run.tofu(context.Background(), domain.TerraformCommandApplyAutoApprove, nil); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the nohup'd sleep to die", func() bool { return len(processesOf(run.uid)) == 0 })
}

// Scenario 4: a restarted executor sweeps what the previous process left.
func TestSweepClearsWhatARestartLeft(t *testing.T) {
	before, sessions, runRoot := newActivities(t)
	left := prepare(t, before, sessions, "run_left")
	startAs(t, left.uid)

	after, err := isolation.New(isolation.Config{RunRoot: runRoot, MaxSessions: 20})
	if err != nil {
		t.Fatal(err)
	}
	if err := after.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the left process to die", func() bool { return len(processesOf(left.uid)) == 0 })
	if _, err := os.Stat(filepath.Dir(left.workspace)); !os.IsNotExist(err) {
		t.Fatalf("run directory survived the sweep: %v", err)
	}
}

// Review focus 1: a process of the pool user can tamper with the helper, which
// runs as that user too, and have it exit 0 having killed nothing. The
// executor must look for itself, or the user returns to the pool still
// running the last tenant's process.
func TestReclaimFailsWhenAProcessSurvivesTheHelper(t *testing.T) {
	uid := uint32(isolation.FirstUID + isolation.PoolSize - 1)
	startAs(t, uid)
	sessions, _ := newSessions(t)

	if err := os.WriteFile(subvertedMarker, nil, 0o644); err != nil { //nolint:gosec // pool users must be able to see it
		t.Fatal(err)
	}
	err := sessions.Sweep(context.Background())
	if removeErr := os.Remove(subvertedMarker); removeErr != nil {
		t.Fatal(removeErr)
	}
	if err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("Sweep = %v, want the survivor to fail it", err)
	}

	// The real helper does kill it.
	if err := sessions.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the survivor to die", func() bool { return len(processesOf(uid)) == 0 })
}
