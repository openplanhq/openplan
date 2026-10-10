package activities

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/planbundle"
	"github.com/vishu42/openplan/internal/runhelper"
	gitrunner "github.com/vishu42/openplan/internal/runner"
	"github.com/vishu42/openplan/internal/runseal"
	"github.com/vishu42/openplan/internal/runuser"
)

// fakeWorkspaceHelper does the helper's work in this process, as the current
// user, and records what it was asked to do. It never kills anything.
type fakeWorkspaceHelper struct {
	mu        sync.Mutex
	reclaimed []string
	packedBy  []runuser.User
	unpacked  []runuser.User
	checkedBy []runuser.User
}

func (h *fakeWorkspaceHelper) Kill(context.Context, runuser.User) error { return nil }

func (h *fakeWorkspaceHelper) Reclaim(_ context.Context, _ runuser.User, workspace string) error {
	h.mu.Lock()
	h.reclaimed = append(h.reclaimed, workspace)
	h.mu.Unlock()
	if workspace == "" {
		return nil
	}
	return runhelper.EmptyDir(workspace)
}

func (h *fakeWorkspaceHelper) Pack(_ context.Context, user runuser.User, dir string) ([]byte, error) {
	h.mu.Lock()
	h.packedBy = append(h.packedBy, user)
	h.mu.Unlock()
	return planbundle.Pack(dir)
}

func (h *fakeWorkspaceHelper) Unpack(_ context.Context, user runuser.User, dir string, bundle []byte) error {
	h.mu.Lock()
	h.unpacked = append(h.unpacked, user)
	h.mu.Unlock()
	return planbundle.Unpack(bundle, dir)
}

func (h *fakeWorkspaceHelper) CheckRoot(_ context.Context, user runuser.User, dir string) error {
	h.mu.Lock()
	h.checkedBy = append(h.checkedBy, user)
	h.mu.Unlock()
	return runhelper.CheckRoot(dir)
}

// testIsolation runs every branch as the current user, through the fake.
func testIsolation() (Isolation, *fakeWorkspaceHelper) {
	helper := &fakeWorkspaceHelper{}
	return Isolation{Users: runuser.DevelopmentUsers(4), Helper: helper}, helper
}

func newTestTemplateRunActivities(t *testing.T, runRoot string, plans PlanArtifactStore, runners ...TerraformRunner) (*TemplateRunActivities, *fakeWorkspaceHelper) {
	t.Helper()
	isolation, helper := testIsolation()
	return NewTemplateRunActivities(runRoot, nil, plans, runseal.NewKeyRing(), isolation, runners...), helper
}

func TestPrepareWorkspaceLaysOutTheRunDirectory(t *testing.T) {
	t.Parallel()
	runRoot := t.TempDir()
	activities, _ := newTestTemplateRunActivities(t, runRoot, nil)

	output, err := activities.PrepareWorkspace(context.Background(), domain.PrepareWorkspaceActivityInput{TenantID: "tenant_123", RunID: "run_123"})
	if err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(runRoot, "tenant_123", "run_123")
	if output.WorkspacePath != filepath.Join(runDir, "workspace") {
		t.Fatalf("WorkspacePath = %q", output.WorkspacePath)
	}
	for path, want := range map[string]os.FileMode{
		runDir:                                     0o711,
		filepath.Join(runDir, "logs"):              0o700,
		filepath.Join(runDir, "workspace"):         0o700,
		filepath.Join(runDir, "workspace", "home"): 0o700,
		filepath.Join(runDir, "workspace", "tmp"):  0o700,
	} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != want {
			t.Errorf("%s: %v, %v; want mode %o", path, info, err, want)
		}
	}
	if _, err := activities.users.Lookup(domain.RunKeyID("tenant_123", "run_123")); err != nil {
		t.Fatalf("no lease after PrepareWorkspace: %v", err)
	}
}

func TestPrepareWorkspaceTwiceReclaimsTheFirstSession(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	runRoot := t.TempDir()
	activities, helper := newTestTemplateRunActivities(t, runRoot, nil)
	input := domain.PrepareWorkspaceActivityInput{TenantID: "tenant_123", RunID: "run_123"}
	first, err := activities.PrepareWorkspace(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, first.WorkspacePath, "left-by-plan", "x")

	second, err := activities.PrepareWorkspace(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(helper.reclaimed) != 1 || helper.reclaimed[0] != first.WorkspacePath {
		t.Fatalf("reclaimed = %v, want the first workspace", helper.reclaimed)
	}
	if _, err := os.Stat(filepath.Join(second.WorkspacePath, "left-by-plan")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the second session sees the first's file: %v", err)
	}
}

func TestPrepareWorkspaceFailsWhenThePoolIsFull(t *testing.T) {
	t.Parallel()
	activities, _ := newTestTemplateRunActivities(t, t.TempDir(), nil)
	for i := range 4 {
		if _, err := activities.PrepareWorkspace(context.Background(), domain.PrepareWorkspaceActivityInput{TenantID: "t", RunID: domain.TemplateRunID("run_" + string(rune('a'+i)))}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := activities.PrepareWorkspace(context.Background(), domain.PrepareWorkspaceActivityInput{TenantID: "t", RunID: "run_e"})
	if !errors.Is(err, runuser.ErrNoFreeUser) {
		t.Fatalf("PrepareWorkspace = %v, want ErrNoFreeUser", err)
	}
}

func TestCleanupWorkspaceReclaimsTheBranch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	runRoot := t.TempDir()
	activities, helper := newTestTemplateRunActivities(t, runRoot, newMemoryPlanStore())
	output, err := activities.PrepareWorkspace(ctx, domain.PrepareWorkspaceActivityInput{TenantID: "tenant_123", RunID: "run_123"})
	if err != nil {
		t.Fatal(err)
	}

	if err := activities.CleanupWorkspace(ctx, domain.CleanupWorkspaceActivityInput{TenantID: "tenant_123", RunID: "run_123"}); err != nil {
		t.Fatal(err)
	}
	if len(helper.reclaimed) != 1 || helper.reclaimed[0] != output.WorkspacePath {
		t.Fatalf("reclaimed = %v", helper.reclaimed)
	}
	if _, err := os.Stat(filepath.Join(runRoot, "tenant_123", "run_123")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("run directory still exists: %v", err)
	}
	if _, err := activities.users.Lookup(domain.RunKeyID("tenant_123", "run_123")); !errors.Is(err, runuser.ErrNoLease) {
		t.Fatalf("lease survives cleanup: %v", err)
	}
}

// After a restart no lease is valid: the pool died with the process. The
// sweep ends every pool user's processes and removes every run directory.
func TestSweepRemovesWhatAnEarlierProcessLeft(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	runRoot := t.TempDir()
	before, _ := newTestTemplateRunActivities(t, runRoot, nil)
	left, err := before.PrepareWorkspace(ctx, domain.PrepareWorkspaceActivityInput{TenantID: "tenant_123", RunID: "run_123"})
	if err != nil {
		t.Fatal(err)
	}

	after, helper := newTestTemplateRunActivities(t, runRoot, nil)
	if err := after.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	var perUser, workspaces int
	for _, workspace := range helper.reclaimed {
		switch workspace {
		case "":
			perUser++
		case left.WorkspacePath:
			workspaces++
		}
	}
	if perUser != 4 || workspaces != 1 {
		t.Fatalf("reclaimed = %v, want every user once and the left workspace", helper.reclaimed)
	}
	if entries, _ := os.ReadDir(filepath.Join(runRoot, "tenant_123")); len(entries) != 0 {
		t.Fatalf("run directories remain: %v", entries)
	}
}

func TestRunTerraformWritesItsLogOutsideTheWorkspace(t *testing.T) {
	t.Parallel()
	runRoot := t.TempDir()
	executor := &recordingCommandExecutor{stdout: "plan stdout\n"}
	runner := localTerraformRunner{runRoot: runRoot, runner: gitrunner.NewLocalProcessRunnerWithExecutor(executor)}

	if _, err := runner.RunTerraform(context.Background(), domain.RunTerraformActivityInput{
		RunID: "run_123", TenantID: "tenant_123",
		WorkspacePath: filepath.Join(runRoot, "tenant_123", "run_123", "workspace"),
		WorkspaceName: "default", Command: domain.TerraformCommandPlan, RunPhase: domain.RunPhasePlan,
	}); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(runRoot, "tenant_123", "run_123", "logs", "plan.log")); err != nil || string(got) != "plan stdout\n" {
		t.Fatalf("log = %q, %v", got, err)
	}
}

type branchRecordingGit struct {
	branch runuser.Branch
	called bool
}

func (g *branchRecordingGit) Clone(ctx context.Context, _, _, dest string, _ gitrunner.GitCredential) error {
	return g.record(ctx, dest)
}

func (g *branchRecordingGit) CheckoutCommit(ctx context.Context, _, _, dest string, _ gitrunner.GitCredential) error {
	return g.record(ctx, dest)
}

func (g *branchRecordingGit) ResolveHead(context.Context, string) (string, error) { return "", nil }

func (g *branchRecordingGit) record(ctx context.Context, dest string) error {
	g.called = true
	g.branch, _ = runuser.BranchFrom(ctx)
	return os.MkdirAll(dest, 0o700)
}

func TestFetchSourceRunsGitInTheSessionsBranch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	activities, helper := newTestTemplateRunActivities(t, t.TempDir(), nil)
	git := &branchRecordingGit{}
	activities.git = git
	workspace, err := activities.PrepareWorkspace(ctx, domain.PrepareWorkspaceActivityInput{TenantID: "tenant_123", RunID: "run_123"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = activities.FetchSource(ctx, domain.FetchSourceActivityInput{
		TenantID: "tenant_123", RunID: "run_123", WorkspacePath: workspace.WorkspacePath,
		RepoOwner: "acme", RepoName: "infra", ResolvedCommitSHA: "0123456789abcdef0123456789abcdef01234567", RootPath: ".",
	})
	if err != nil {
		t.Fatal(err)
	}
	user, _ := activities.users.Lookup(domain.RunKeyID("tenant_123", "run_123"))
	want := runuser.Branch{User: user, Home: filepath.Join(workspace.WorkspacePath, "home"), TempDir: filepath.Join(workspace.WorkspacePath, "tmp")}
	if git.branch != want {
		t.Fatalf("git ran in %+v, want %+v", git.branch, want)
	}
	if len(helper.checkedBy) != 1 || helper.checkedBy[0] != user {
		t.Fatalf("template root checked by %v, want the helper as %v", helper.checkedBy, user)
	}
}

// After a restart the session's lease is gone. The next step must fail, not
// run as whichever user the new pool would hand out.
func TestFetchSourceFailsWithoutALease(t *testing.T) {
	t.Parallel()
	activities, _ := newTestTemplateRunActivities(t, t.TempDir(), nil)
	git := &branchRecordingGit{}
	activities.git = git

	_, err := activities.FetchSource(context.Background(), domain.FetchSourceActivityInput{
		TenantID: "tenant_123", RunID: "run_123", WorkspacePath: "/somewhere",
		RepoOwner: "acme", RepoName: "infra", ResolvedCommitSHA: "0123456789abcdef0123456789abcdef01234567", RootPath: ".",
	})
	if !errors.Is(err, runuser.ErrNoLease) || git.called {
		t.Fatalf("FetchSource = %v, git called = %v; want ErrNoLease and no git", err, git.called)
	}
}

type branchRecordingTerraformRunner struct{ branch runuser.Branch }

func (r *branchRecordingTerraformRunner) RunTerraform(ctx context.Context, _ domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
	r.branch, _ = runuser.BranchFrom(ctx)
	return domain.RunTerraformActivityOutput{}, nil
}

func TestRunTerraformRunsInTheSessionsBranch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	recorder := &branchRecordingTerraformRunner{}
	activities, _ := newTestTemplateRunActivities(t, t.TempDir(), nil, recorder)
	workspace, err := activities.PrepareWorkspace(ctx, domain.PrepareWorkspaceActivityInput{TenantID: "tenant_123", RunID: "run_123"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := activities.RunTerraform(ctx, domain.RunTerraformActivityInput{TenantID: "tenant_123", RunID: "run_123", WorkspacePath: workspace.WorkspacePath}); err != nil {
		t.Fatal(err)
	}
	if recorder.branch.Home != filepath.Join(workspace.WorkspacePath, "home") {
		t.Fatalf("tofu ran in %+v", recorder.branch)
	}
}
