//go:build isolation

// Package isolationtest checks subprocess isolation (#331) in the executor
// image, as root with only SETUID, SETGID and CHOWN. make isolation-test runs
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
	"github.com/vishu42/openplan/internal/runhelper"
	"github.com/vishu42/openplan/internal/runner"
	"github.com/vishu42/openplan/internal/runseal"
	"github.com/vishu42/openplan/internal/runuser"
)

// TestMain doubles as the helper: the client below starts this binary with
// runhelper.Subcommand, as the executor starts its own.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == runhelper.Subcommand {
		os.Exit(runhelper.Main(os.Args[2:], os.Stdin, os.Stdout, os.Stderr))
	}
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "isolation tests run as root in the executor image: make isolation-test")
		os.Exit(1)
	}
	os.Exit(m.Run())
}

var helper = runhelper.Client{Command: []string{os.Args[0], runhelper.Subcommand}}

type session struct {
	activities *activities.TemplateRunActivities
	tenant     domain.TenantID
	run        domain.TemplateRunID
	workspace  string
	user       runuser.User
}

func newActivities(t *testing.T) (*activities.TemplateRunActivities, string) {
	t.Helper()
	// Under the image's 0711 run root, so pool users can walk to their workspace.
	runRoot, err := os.MkdirTemp("/var/lib/openplan/runs", "isolation-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(runRoot, 0o711); err != nil {
		t.Fatal(err)
	}
	isolation := activities.Isolation{Users: runuser.PoolUsers(), Helper: helper}
	return activities.NewTemplateRunActivities(runRoot, nil, nil, runseal.NewKeyRing(), isolation), runRoot
}

func prepare(t *testing.T, a *activities.TemplateRunActivities, run string) session {
	t.Helper()
	output, err := a.PrepareWorkspace(context.Background(), domain.PrepareWorkspaceActivityInput{TenantID: "tenant", RunID: domain.TemplateRunID(run)})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(output.WorkspacePath)
	if err != nil {
		t.Fatal(err)
	}
	uid := info.Sys().(*syscall.Stat_t).Uid
	user := runuser.PoolUsers()[uid-runuser.FirstUID]
	return session{activities: a, tenant: "tenant", run: domain.TemplateRunID(run), workspace: output.WorkspacePath, user: user}
}

func (s session) ctx(ctx context.Context) context.Context {
	return runuser.WithBranch(ctx, runuser.Branch{User: s.user, Home: filepath.Join(s.workspace, "home"), TempDir: filepath.Join(s.workspace, "tmp")})
}

// shell runs script as the session's pool user, in its workspace.
func (s session) shell(t *testing.T, script string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := runner.NewIsolatedExecutor(helper.Kill).Run(s.ctx(context.Background()), s.workspace, nil, &out, &out, "/bin/sh", "-c", script)
	return out.String(), err
}

func (s session) writeTemplate(t *testing.T, hcl string) {
	t.Helper()
	if out, err := s.shell(t, "cat > main.tf <<'EOF'\n"+hcl+"\nEOF"); err != nil {
		t.Fatalf("write template: %v: %s", err, out)
	}
}

func (s session) tofu(ctx context.Context, command domain.TerraformCommandType, env map[string]string) error {
	_, err := runner.NewIsolatedProcessRunner(helper.Kill).Run(s.ctx(ctx), runner.TerraformCommand{
		WorkspacePath: s.workspace, WorkspaceName: "default", Command: command, Environment: env,
		Stdout: os.Stderr, Stderr: os.Stderr,
	})
	return err
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
	a, _ := newActivities(t)
	runA, runB := prepare(t, a, "run_a"), prepare(t, a, "run_b")
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
	eventually(t, "run B's sleep", func() bool { return len(processesOf(runB.user.UID)) >= 2 })

	if out, _ := runA.shell(t, "grep -a -r -l "+canary+" /proc/[0-9]*/environ 2>/dev/null; true"); strings.TrimSpace(out) != "" {
		t.Fatalf("run A read run B's environment: %s", out)
	}
	pidB := processesOf(runB.user.UID)[0]
	for name, script := range map[string]string{
		"the executor's environment": fmt.Sprintf("cat /proc/%d/environ", os.Getpid()),
		"run B's workspace":          "ls " + runB.workspace,
		"a signal to run B":          fmt.Sprintf("kill -9 %d", pidB),
	} {
		if out, err := runA.shell(t, script); err == nil {
			t.Errorf("run A reached %s: %s", name, out)
		}
	}

	// A plan file planted as a symlink to the executor's environment packs
	// nothing: the helper reads it as run A's user.
	if out, err := runA.shell(t, fmt.Sprintf("ln -s /proc/%d/environ tfplan", os.Getpid())); err != nil {
		t.Fatal(out)
	}
	if _, err := helper.Pack(context.Background(), runA.user, runA.workspace); err == nil {
		t.Fatal("UploadPlan packed the executor's environment through a symlink")
	}

	// Cancelling run B (a timeout) kills its whole branch through the helper.
	cancelB()
	<-doneB
	eventually(t, "run B's branch to die", func() bool { return len(processesOf(runB.user.UID)) == 0 })
}

// Scenario 3: what a command leaves running dies with it.
func TestNohupDoesNotOutliveItsCommand(t *testing.T) {
	a, _ := newActivities(t)
	run := prepare(t, a, "run_nohup")
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
	eventually(t, "the nohup'd sleep to die", func() bool { return len(processesOf(run.user.UID)) == 0 })
}

// Scenario 4: a restarted executor sweeps what the previous process left.
func TestSweepClearsWhatARestartLeft(t *testing.T) {
	before, runRoot := newActivities(t)
	left := prepare(t, before, "run_left")

	// Started directly, not through the isolated executor, which would kill
	// it when the command ends.
	sleep := exec.Command("sleep", "3600")
	sleep.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: left.user.UID, Gid: left.user.GID, Groups: []uint32{}}}
	if err := sleep.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = sleep.Wait() }()

	after := activities.NewTemplateRunActivities(runRoot, nil, nil, runseal.NewKeyRing(), activities.Isolation{Users: runuser.PoolUsers(), Helper: helper})
	if err := after.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the left process to die", func() bool { return len(processesOf(left.user.UID)) == 0 })
	if _, err := os.Stat(filepath.Dir(left.workspace)); !os.IsNotExist(err) {
		t.Fatalf("run directory survived the sweep: %v", err)
	}
}

// Review focus 1: a process of the pool user can tamper with the helper, which
// runs as that user too, and have it exit 0 having killed nothing. Reclaim
// must look for itself, or the user returns to the pool still running the
// last tenant's process.
func TestReclaimFailsWhenAProcessSurvivesTheHelper(t *testing.T) {
	user := runuser.PoolUsers()[runuser.PoolSize-1]
	sleep := exec.Command("sleep", "3600")
	sleep.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: user.UID, Gid: user.GID, Groups: []uint32{}}}
	if err := sleep.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = sleep.Wait() }()
	defer func() { _ = helper.Kill(context.Background(), user) }()

	// Stands in for a subverted helper: it exits 0 and does nothing.
	subverted := runhelper.Client{Command: []string{"/bin/true"}}
	if err := subverted.Reclaim(context.Background(), user, ""); err == nil {
		t.Fatal("Reclaim succeeded with the user's process still running")
	}
}
