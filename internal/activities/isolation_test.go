package activities

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/isolation"
	"github.com/vishu42/openplan/internal/planbundle"
	gitrunner "github.com/vishu42/openplan/internal/runner"
	"github.com/vishu42/openplan/internal/runseal"
)

// testSessions runs every session as the current user, with the helper's
// work done in this process.
func testSessions(t *testing.T, runRoot string) *isolation.Manager {
	t.Helper()
	return isolation.NewForTest(t, isolation.Config{RunRoot: runRoot}, 4)
}

// sessionsWithRun returns sessions under runRoot with tenant_123's run_123
// open, as PrepareWorkspace leaves it for the activities after it.
func sessionsWithRun(t *testing.T, runRoot string) *isolation.Manager {
	t.Helper()
	sessions := testSessions(t, runRoot)
	if _, err := sessions.Open(context.Background(), "tenant_123", "run_123"); err != nil {
		t.Fatal(err)
	}
	return sessions
}

func newTestTemplateRunActivities(t *testing.T, runRoot string, plans PlanArtifactStore, runners ...TerraformRunner) *TemplateRunActivities {
	t.Helper()
	return NewTemplateRunActivities(nil, plans, runseal.NewKeyRing(), testSessions(t, runRoot), runners...)
}

// sessionHome runs a command the way the executor's runners do and returns
// the HOME it got: the session's own when ctx is tagged with an open session.
// Sessions here run as the current user, which as root is a uid no session
// may have, so the tests that call it skip as root.
func sessionHome(ctx context.Context) (string, error) {
	var home bytes.Buffer
	err := isolation.SessionExecutor().Run(ctx, "", nil, &home, io.Discard, "/bin/sh", "-c", `printf %s "$HOME"`)
	return home.String(), err
}

func skipAsRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("sessions run as the current user here, and root is not a pool user")
	}
}

func TestPrepareWorkspaceLaysOutTheRunDirectory(t *testing.T) {
	t.Parallel()
	runRoot := t.TempDir()
	activities := newTestTemplateRunActivities(t, runRoot, nil)

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
	if _, err := activities.sessions.Session("tenant_123", "run_123"); err != nil {
		t.Fatalf("no session after PrepareWorkspace: %v", err)
	}
}

func TestPrepareWorkspaceTwiceEndsTheFirstSession(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	runRoot := t.TempDir()
	activities := newTestTemplateRunActivities(t, runRoot, nil)
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
	if _, err := os.Stat(filepath.Join(second.WorkspacePath, "left-by-plan")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the second session sees the first's file: %v", err)
	}
}

func TestPrepareWorkspaceFailsWhenThePoolIsFull(t *testing.T) {
	t.Parallel()
	activities := newTestTemplateRunActivities(t, t.TempDir(), nil)
	for i := range 4 {
		if _, err := activities.PrepareWorkspace(context.Background(), domain.PrepareWorkspaceActivityInput{TenantID: "t", RunID: domain.TemplateRunID("run_" + string(rune('a'+i)))}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := activities.PrepareWorkspace(context.Background(), domain.PrepareWorkspaceActivityInput{TenantID: "t", RunID: "run_e"})
	if !errors.Is(err, isolation.ErrNoFreeUser) {
		t.Fatalf("PrepareWorkspace = %v, want ErrNoFreeUser", err)
	}
}

func TestCleanupWorkspaceEndsTheSession(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	runRoot := t.TempDir()
	activities := newTestTemplateRunActivities(t, runRoot, newMemoryPlanStore())
	if _, err := activities.PrepareWorkspace(ctx, domain.PrepareWorkspaceActivityInput{TenantID: "tenant_123", RunID: "run_123"}); err != nil {
		t.Fatal(err)
	}

	if err := activities.CleanupWorkspace(ctx, domain.CleanupWorkspaceActivityInput{TenantID: "tenant_123", RunID: "run_123"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runRoot, "tenant_123", "run_123")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("run directory still exists: %v", err)
	}
	if _, err := activities.sessions.Session("tenant_123", "run_123"); !errors.Is(err, isolation.ErrNoLease) {
		t.Fatalf("session survives cleanup: %v", err)
	}
}

// The saved plan is read through the session, which stays inside the plan's
// folder: a plan file the session pointed elsewhere is refused. Read
// directly, as the executor's root, the symlink would be followed.
func TestUploadPlanReadsThePlanThroughTheSession(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	plans := newMemoryPlanStore()
	activities := newTestTemplateRunActivities(t, t.TempDir(), plans)
	workspace, err := activities.PrepareWorkspace(ctx, domain.PrepareWorkspaceActivityInput{TenantID: "tenant_123", RunID: "run_123"})
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	writeTestFile(t, outside, "secret", "the executor's own")
	planRoot := filepath.Join(workspace.WorkspacePath, "source")
	writeTestFile(t, planRoot, ".terraform.lock.hcl", "lock")
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(planRoot, "tfplan")); err != nil {
		t.Fatal(err)
	}
	sealedKey, err := runseal.Seal(workspace.PublicKey, mustPlanKey(t))
	if err != nil {
		t.Fatal(err)
	}

	err = activities.UploadPlan(ctx, domain.PlanArtifactActivityInput{TenantID: "tenant_123", RunID: "run_123", TerraformPath: planRoot, SealedPlanKey: sealedKey})
	if err == nil || len(plans.plans) != 0 {
		t.Fatalf("UploadPlan = %v, stored %d; want the symlinked plan refused", err, len(plans.plans))
	}
}

// The saved plan is written back through the session too: a plan file the
// session pointed elsewhere is refused, not followed.
func TestDownloadPlanWritesThePlanThroughTheSession(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	plans := newMemoryPlanStore()
	activities := newTestTemplateRunActivities(t, t.TempDir(), plans)
	workspace, err := activities.PrepareWorkspace(ctx, domain.PrepareWorkspaceActivityInput{TenantID: "tenant_123", RunID: "run_123"})
	if err != nil {
		t.Fatal(err)
	}
	key := mustPlanKey(t)
	sealed, err := planbundle.Seal(key, mustBundle(t), "tenant_123/run_123")
	if err != nil {
		t.Fatal(err)
	}
	plans.plans["tenant_123/run_123"] = sealed
	outside := t.TempDir()
	writeTestFile(t, outside, "target", "keep")
	applyRoot := filepath.Join(workspace.WorkspacePath, "source")
	writeTestFile(t, applyRoot, "main.tf", "")
	if err := os.Symlink(filepath.Join(outside, "target"), filepath.Join(applyRoot, "tfplan")); err != nil {
		t.Fatal(err)
	}
	sealedKey, err := runseal.Seal(workspace.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	if err := activities.DownloadPlan(ctx, domain.PlanArtifactActivityInput{TenantID: "tenant_123", RunID: "run_123", TerraformPath: applyRoot, SealedPlanKey: sealedKey}); err == nil {
		t.Fatal("DownloadPlan wrote through a symlink out of the plan's folder")
	}
	assertTestFile(t, outside, "target", "keep")
}

func TestRunTerraformWritesItsLogOutsideTheWorkspace(t *testing.T) {
	t.Parallel()
	runRoot := t.TempDir()
	executor := &recordingCommandExecutor{stdout: "plan stdout\n"}
	runner := localTerraformRunner{sessions: sessionsWithRun(t, runRoot), runner: gitrunner.NewLocalProcessRunnerWithExecutor(executor)}

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

// sessionRecordingGit records the HOME a command run with git's ctx would
// get, and makes the clone's directory.
type sessionRecordingGit struct {
	home   string
	err    error
	called bool
}

func (g *sessionRecordingGit) Clone(ctx context.Context, _, _, dest string, _ gitrunner.GitCredential) error {
	return g.record(ctx, dest)
}

func (g *sessionRecordingGit) CheckoutCommit(ctx context.Context, _, _, dest string, _ gitrunner.GitCredential) error {
	return g.record(ctx, dest)
}

func (g *sessionRecordingGit) ResolveHead(context.Context, string) (string, error) { return "", nil }

func (g *sessionRecordingGit) record(ctx context.Context, dest string) error {
	g.called = true
	g.home, g.err = sessionHome(ctx)
	return os.MkdirAll(dest, 0o700)
}

func TestFetchSourceRunsGitInTheRunsSession(t *testing.T) {
	t.Parallel()
	skipAsRoot(t)
	ctx := context.Background()
	activities := newTestTemplateRunActivities(t, t.TempDir(), nil)
	git := &sessionRecordingGit{}
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
	if want := filepath.Join(workspace.WorkspacePath, "home"); git.err != nil || git.home != want {
		t.Fatalf("git ran with HOME %q, %v; want the session's %q", git.home, git.err, want)
	}
}

// After a restart the run's session is gone. The next step must fail, not
// run as whichever user the new pool would hand out.
func TestFetchSourceFailsWithoutASession(t *testing.T) {
	t.Parallel()
	activities := newTestTemplateRunActivities(t, t.TempDir(), nil)
	git := &sessionRecordingGit{}
	activities.git = git

	_, err := activities.FetchSource(context.Background(), domain.FetchSourceActivityInput{
		TenantID: "tenant_123", RunID: "run_123", WorkspacePath: "/somewhere",
		RepoOwner: "acme", RepoName: "infra", ResolvedCommitSHA: "0123456789abcdef0123456789abcdef01234567", RootPath: ".",
	})
	if !errors.Is(err, isolation.ErrNoLease) || git.called {
		t.Fatalf("FetchSource = %v, git called = %v; want ErrNoLease and no git", err, git.called)
	}
}

type sessionRecordingTerraformRunner struct {
	home string
	err  error
}

func (r *sessionRecordingTerraformRunner) RunTerraform(ctx context.Context, _ domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
	r.home, r.err = sessionHome(ctx)
	return domain.RunTerraformActivityOutput{}, nil
}

func TestRunTerraformRunsInTheRunsSession(t *testing.T) {
	t.Parallel()
	skipAsRoot(t)
	ctx := context.Background()
	recorder := &sessionRecordingTerraformRunner{}
	activities := newTestTemplateRunActivities(t, t.TempDir(), nil, recorder)
	workspace, err := activities.PrepareWorkspace(ctx, domain.PrepareWorkspaceActivityInput{TenantID: "tenant_123", RunID: "run_123"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := activities.RunTerraform(ctx, domain.RunTerraformActivityInput{TenantID: "tenant_123", RunID: "run_123", WorkspacePath: workspace.WorkspacePath}); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(workspace.WorkspacePath, "home"); recorder.err != nil || recorder.home != want {
		t.Fatalf("tofu ran with HOME %q, %v; want the session's %q", recorder.home, recorder.err, want)
	}
}
