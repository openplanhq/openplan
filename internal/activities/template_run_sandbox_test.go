package activities

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.temporal.io/sdk/temporal"

	"github.com/vishu42/openplan/internal/config"
	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/runner"
	"github.com/vishu42/openplan/internal/runseal"
	"github.com/vishu42/openplan/internal/sandbox"
)

// recordingRunner records each job before running it in this process.
type recordingRunner struct {
	next sandbox.Runner
	jobs []sandbox.Job
}

func (r *recordingRunner) Run(ctx context.Context, job sandbox.Job, output io.Writer) (json.RawMessage, error) {
	r.jobs = append(r.jobs, job)
	return r.next.Run(ctx, job, output)
}

// newSandboxedActivities returns activities whose bodies run in this
// process on tofu and git, with tenant_123's run_123 prepared.
func newSandboxedActivities(t *testing.T, tofu runner.CommandExecutor) (*TemplateRunActivities, *recordingRunner, string) {
	t.Helper()
	bodies := &Bodies{
		terraform: runner.NewLocalTerraformRunnerWithExecutor(tofu),
		git:       runner.NewLocalGitRunnerWithExecutor(&scriptedExecutor{}),
		logs:      &recordingTemplateRunLogStore{},
		plans:     newMemoryPlanStore(),
	}
	recorder := &recordingRunner{next: sandbox.InProcess{Handlers: bodies.Handlers()}}
	activities := NewTemplateRunActivities(recorder, sandbox.Workspaces{Root: t.TempDir()}, config.ArtifactStoreConfig{Kind: config.ArtifactStoreS3}, bodies.plans, runseal.NewKeyRing())
	return activities, recorder, prepareTestWorkspace(t, activities)
}

func sealedEnvironment(t *testing.T, activities *TemplateRunActivities, credentials domain.RunCredentials) []byte {
	t.Helper()
	publicKey, err := activities.keys.Generate(domain.RunKeyID("tenant_123", "run_123"))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := runseal.Seal(publicKey, credentials)
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}

func TestRunTerraformSpoolsItsLogOutsideTheWorkspace(t *testing.T) {
	t.Parallel()

	activities, _, workspace := newSandboxedActivities(t, &scriptedExecutor{stdout: "Plan: 1 to add\n"})
	if _, err := activities.RunTerraform(context.Background(), planInput(workspace)); err != nil {
		t.Fatalf("RunTerraform returned error: %v", err)
	}
	if got := runLog(t, workspace, "plan.log"); !strings.Contains(got, "Plan: 1 to add") {
		t.Fatalf("plan.log = %q", got)
	}
}

func TestRunTerraformRedactsCredentialsInItsLog(t *testing.T) {
	t.Parallel()

	activities, _, workspace := newSandboxedActivities(t, &scriptedExecutor{stdout: "key is s3cr3t-value\n"})
	input := planInput(workspace)
	input.SealedEnvironment = sealedEnvironment(t, activities, domain.RunCredentials{
		Environment: map[string]string{"AWS_SECRET_ACCESS_KEY": "s3cr3t-value"},
	})
	if _, err := activities.RunTerraform(context.Background(), input); err != nil {
		t.Fatalf("RunTerraform returned error: %v", err)
	}
	if got := runLog(t, workspace, "plan.log"); strings.Contains(got, "s3cr3t-value") {
		t.Fatalf("plan.log leaks the credential: %q", got)
	}
}

// #497: a value marked not sensitive reads as itself in the log, and a
// sensitive one beside it is still hidden.
func TestRunTerraformLeavesPlainValuesInItsLog(t *testing.T) {
	t.Parallel()

	activities, _, workspace := newSandboxedActivities(t, &scriptedExecutor{stdout: "region us-east-1, key s3cr3t-value\n"})
	input := planInput(workspace)
	input.SealedEnvironment = sealedEnvironment(t, activities, domain.RunCredentials{
		Environment: map[string]string{"AWS_REGION": "us-east-1", "AWS_SECRET_ACCESS_KEY": "s3cr3t-value"},
		Plain:       []string{"AWS_REGION"},
	})
	if _, err := activities.RunTerraform(context.Background(), input); err != nil {
		t.Fatalf("RunTerraform returned error: %v", err)
	}
	if got := runLog(t, workspace, "plan.log"); !strings.Contains(got, "region us-east-1, key ******") {
		t.Fatalf("plan.log = %q", got)
	}
}

// The credential rule: tofu's sandbox gets the run's credentials, its
// workspace, and nothing of storage.
func TestRunTerraformJobCarriesOnlyTheRunsCredentials(t *testing.T) {
	t.Parallel()

	activities, recorder, workspace := newSandboxedActivities(t, &scriptedExecutor{})
	input := planInput(workspace)
	input.SealedEnvironment = sealedEnvironment(t, activities, domain.RunCredentials{
		Environment: map[string]string{"AWS_ACCESS_KEY_ID": "AKIA"},
	})
	if _, err := activities.RunTerraform(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	job := recorder.jobs[len(recorder.jobs)-1]
	encoded, err := json.Marshal(job.Request)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "Store") || strings.Contains(string(encoded), "PlanKey") {
		t.Fatalf("tofu's request carries storage or the plan key: %s", encoded)
	}
	if len(job.Mounts) != 1 || job.Mounts[0].Path != workspace || job.Mounts[0].ReadOnly || len(job.Groups) != 0 {
		t.Fatalf("mounts = %+v, groups = %v; want only the workspace, read-write", job.Mounts, job.Groups)
	}
}

// Review focus 4.
func TestRunTerraformRefusesAPathOutsideTheWorkspace(t *testing.T) {
	t.Parallel()

	activities, recorder, workspace := newSandboxedActivities(t, &scriptedExecutor{})
	input := planInput(workspace)
	input.TerraformPath = "/etc"
	before := len(recorder.jobs)
	if _, err := activities.RunTerraform(context.Background(), input); err == nil || !strings.Contains(err.Error(), "outside the run's workspace") {
		t.Fatalf("error = %v, want the path refused", err)
	}
	if len(recorder.jobs) != before {
		t.Fatal("a sandbox started for a path outside the workspace")
	}
}

// commandFailure is the Terraform error a RunTerraform error carries, and
// whether it carries one.
func commandFailure(t *testing.T, err error) (domain.TerraformCommandFailure, bool) {
	t.Helper()
	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) || appErr.Type() != domain.TerraformCommandFailedErrorType {
		return domain.TerraformCommandFailure{}, false
	}
	var failure domain.TerraformCommandFailure
	if err := appErr.Details(&failure); err != nil {
		t.Fatalf("decode command failure: %v", err)
	}
	return failure, true
}

// Issue 522: a failed command's error leads with what Terraform said.
func TestRunTerraformReturnsTheCommandError(t *testing.T) {
	t.Parallel()

	activities, _, workspace := newSandboxedActivities(t, &scriptedExecutor{stdout: "Error: bad creds\n", err: errors.New("exit status 1")})
	_, err := activities.RunTerraform(context.Background(), planInput(workspace))
	failure, ok := commandFailure(t, err)
	if !ok || failure.Message != "Error: bad creds" {
		t.Fatalf("RunTerraform error = %v, failure = %#v, want the command's error", err, failure)
	}
	if !strings.Contains(err.Error(), "run terraform: ") {
		t.Fatalf("error = %q, want today's wrapper text", err)
	}
}

// Review focus 2: the message is read from the redacted spool.
func TestRunTerraformCommandErrorIsRedacted(t *testing.T) {
	t.Parallel()

	activities, _, workspace := newSandboxedActivities(t, &scriptedExecutor{stdout: "Error: key s3cr3t-value rejected\n", err: errors.New("exit status 1")})
	input := planInput(workspace)
	input.SealedEnvironment = sealedEnvironment(t, activities, domain.RunCredentials{
		Environment: map[string]string{"AWS_SECRET_ACCESS_KEY": "s3cr3t-value"},
	})
	_, err := activities.RunTerraform(context.Background(), input)
	failure, ok := commandFailure(t, err)
	if !ok || strings.Contains(failure.Message, "s3cr3t-value") {
		t.Fatalf("failure = %#v (carried %v), want the redacted error", failure, ok)
	}
}

// Review focus 1: a command that printed no error fails as it does today.
func TestRunTerraformReturnsNoCommandErrorWithoutAnErrorLine(t *testing.T) {
	t.Parallel()

	activities, _, workspace := newSandboxedActivities(t, &scriptedExecutor{stdout: "Killed\n", err: errors.New("exit status 1")})
	_, err := activities.RunTerraform(context.Background(), planInput(workspace))
	if err == nil {
		t.Fatal("RunTerraform returned no error")
	}
	if _, ok := commandFailure(t, err); ok {
		t.Fatalf("error = %v, want no command failure", err)
	}
	if !strings.HasPrefix(err.Error(), "run terraform: ") {
		t.Fatalf("error = %q, want today's text", err)
	}
}

// Review focus 3.
func TestRunTerraformCapsItsLog(t *testing.T) {
	t.Parallel()

	var capped bytes.Buffer
	writer := newCappedWriter(nopWriteCloser{Buffer: &capped}, 10)
	for range 3 {
		if _, err := writer.Write([]byte("0123456789")); err != nil {
			t.Fatal(err)
		}
	}
	if want := "0123456789" + truncatedMarker; capped.String() != want {
		t.Fatalf("log = %q, want %q", capped.String(), want)
	}
}

func TestUploadLogUploadsTheSpooledLog(t *testing.T) {
	t.Parallel()

	activities, recorder, workspace := newSandboxedActivities(t, &scriptedExecutor{stdout: "Plan: 1 to add\n"})
	if _, err := activities.RunTerraform(context.Background(), planInput(workspace)); err != nil {
		t.Fatal(err)
	}
	log, err := activities.UploadLog(context.Background(), domain.UploadLogActivityInput{
		TenantID: "tenant_123", RunID: "run_123", LogName: "plan",
	})
	if err != nil {
		t.Fatalf("UploadLog returned error: %v", err)
	}
	if log.Name != "plan" {
		t.Fatalf("log = %+v, want phase plan", log)
	}
	job := recorder.jobs[len(recorder.jobs)-1]
	if len(job.Mounts) != 1 || job.Mounts[0].Path != filepath.Join(filepath.Dir(workspace), "logs") || !job.Mounts[0].ReadOnly {
		t.Fatalf("mounts = %+v, want only the spool, read-only", job.Mounts)
	}
}

func TestPrepareWorkspaceLaysOutTheRunDirectory(t *testing.T) {
	t.Parallel()

	_, _, workspace := newSandboxedActivities(t, &scriptedExecutor{})
	for _, dir := range []string{workspace, filepath.Join(filepath.Dir(workspace), "logs")} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("%s: %v", dir, err)
		}
	}
}

func TestCleanupWorkspaceRemovesTheRunDirectory(t *testing.T) {
	t.Parallel()

	activities, _, workspace := newSandboxedActivities(t, &scriptedExecutor{})
	if err := activities.CleanupWorkspace(context.Background(), domain.CleanupWorkspaceActivityInput{TenantID: "tenant_123", RunID: "run_123"}); err != nil {
		t.Fatalf("CleanupWorkspace returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(workspace)); !os.IsNotExist(err) {
		t.Fatalf("run directory survived: %v", err)
	}
}

// newTestTemplateRunActivities returns activities over runRoot whose bodies
// run in this process, and the bodies, so a test can swap their runners.
func newTestTemplateRunActivities(t *testing.T, runRoot string, plans PlanArtifactStore) (*TemplateRunActivities, *Bodies) {
	t.Helper()
	bodies := &Bodies{
		terraform: runner.NewLocalTerraformRunnerWithExecutor(&scriptedExecutor{}),
		git:       runner.NewLocalGitRunnerWithExecutor(&scriptedExecutor{}),
		logs:      &recordingTemplateRunLogStore{},
		plans:     plans,
	}
	if plans == nil {
		bodies.plans = newMemoryPlanStore()
	}
	activities := NewTemplateRunActivities(sandbox.InProcess{Handlers: bodies.Handlers()}, sandbox.Workspaces{Root: runRoot}, config.ArtifactStoreConfig{Kind: config.ArtifactStoreS3}, bodies.plans, runseal.NewKeyRing())
	return activities, bodies
}
