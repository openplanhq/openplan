package activities

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/planbundle"
	"github.com/vishu42/openplan/internal/runner"
	"github.com/vishu42/openplan/internal/sandbox"
)

// scriptedExecutor stands in for git and tofu: it records each command and
// prints stdout.
type scriptedExecutor struct {
	commands [][]string
	env      [][]string
	stdout   string
	err      error
}

func (e *scriptedExecutor) Run(_ context.Context, _ string, env []string, stdout io.Writer, _ io.Writer, name string, args ...string) error {
	e.commands = append(e.commands, append([]string{name}, args...))
	e.env = append(e.env, env)
	_, _ = io.WriteString(stdout, e.stdout)
	return e.err
}

func runBody(t *testing.T, bodies *Bodies, activity string, request any, output io.Writer) ([]byte, error) {
	t.Helper()
	return sandbox.InProcess{Handlers: bodies.Handlers()}.Run(context.Background(), sandbox.Job{Activity: activity, Request: request}, output)
}

func TestRunTerraformBodyStreamsTofusOutput(t *testing.T) {
	t.Parallel()

	tofu := &scriptedExecutor{stdout: "Initializing the backend...\n"}
	bodies := &Bodies{terraform: runner.NewLocalTerraformRunnerWithExecutor(tofu)}
	var output bytes.Buffer
	_, err := runBody(t, bodies, bodyRunTerraform, runTerraformRequest{
		TerraformPath: t.TempDir(), WorkspaceName: "default",
		Command: domain.TerraformCommandInit, Environment: map[string]string{"AWS_ACCESS_KEY_ID": "AKIA"},
	}, &output)
	if err != nil {
		t.Fatalf("run-terraform returned error: %v", err)
	}
	if output.String() != "Initializing the backend...\n" {
		t.Fatalf("output = %q", output.String())
	}
	if !strings.Contains(strings.Join(tofu.env[0], " "), "AWS_ACCESS_KEY_ID=AKIA") {
		t.Fatalf("tofu env = %q, want the run's credentials", tofu.env[0])
	}
}

func TestFetchSourceBodyChecksOutTheCommitWithTheToken(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	source := filepath.Join(workspace, "source")
	git := &scriptedExecutor{}
	bodies := &Bodies{git: runner.NewLocalGitRunnerWithExecutor(git)}
	// git is scripted, so make the directory its checkout would have.
	if err := os.MkdirAll(filepath.Join(source, "infra"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := runBody(t, bodies, bodyFetchSource, fetchSourceRequest{
		RepoURL: "https://github.com/acme/infra.git", CommitSHA: "abc123",
		SourcePath: source, TerraformPath: filepath.Join(source, "infra"), Token: "ghs_token",
	}, io.Discard)
	if err != nil {
		t.Fatalf("fetch-source returned error: %v", err)
	}
	if !strings.Contains(strings.Join(git.env[0], " "), "extraheader") {
		t.Fatalf("git env = %q, want the token's header", git.env[0])
	}
}

func TestFetchSourceBodyFailsWhenTheRootIsMissing(t *testing.T) {
	t.Parallel()

	source := filepath.Join(t.TempDir(), "source")
	bodies := &Bodies{git: runner.NewLocalGitRunnerWithExecutor(&scriptedExecutor{})}
	_, err := runBody(t, bodies, bodyFetchSource, fetchSourceRequest{
		RepoURL: "https://github.com/acme/infra.git", CommitSHA: "abc123",
		SourcePath: source, TerraformPath: filepath.Join(source, "missing"),
	}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("error = %v, want the missing root named", err)
	}
}

func TestUploadLogBodyUploadsTheSpool(t *testing.T) {
	t.Parallel()

	logsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(logsDir, "plan.log"), []byte("Plan: 1 to add\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	logs := &recordingTemplateRunLogStore{}
	bodies := &Bodies{logs: logs}
	data, err := runBody(t, bodies, bodyUploadLog, uploadLogRequest{
		TenantID: "tenant_123", RunID: "run_123", Phase: "plan", LogsDir: logsDir, FileName: "plan.log",
	}, io.Discard)
	if err != nil {
		t.Fatalf("upload-log returned error: %v", err)
	}
	if logs.content != "Plan: 1 to add\n" {
		t.Fatalf("uploaded %q", logs.content)
	}
	if !strings.Contains(string(data), `"phase":"plan"`) {
		t.Fatalf("result = %s, want the log's metadata", data)
	}
}

// The spec's symlink attack: the plan file is a link to the sandbox's own
// environment, which holds its storage key.
func TestUploadPlanBodyRefusesASymlinkedPlan(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.Symlink("/proc/self/environ", filepath.Join(dir, runner.PlanFileName)); err != nil {
		t.Fatal(err)
	}
	plans := newMemoryPlanStore()
	bodies := &Bodies{plans: plans}
	key, _ := planbundle.NewKey()
	_, err := runBody(t, bodies, bodyUploadPlan, planRequest{TenantID: "tenant_123", RunID: "run_123", TerraformPath: dir, PlanKey: key}, io.Discard)
	if err == nil {
		t.Fatal("upload-plan uploaded a symlinked plan")
	}
	if len(plans.plans) != 0 {
		t.Fatal("something was stored")
	}
}

func TestPlanBodiesRoundTripAPlan(t *testing.T) {
	t.Parallel()

	planDir, applyDir := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(planDir, runner.PlanFileName), []byte("the plan"), 0o600); err != nil {
		t.Fatal(err)
	}
	plans := newMemoryPlanStore()
	bodies := &Bodies{plans: plans}
	key, _ := planbundle.NewKey()
	if _, err := runBody(t, bodies, bodyUploadPlan, planRequest{TenantID: "tenant_123", RunID: "run_123", TerraformPath: planDir, PlanKey: key}, io.Discard); err != nil {
		t.Fatalf("upload-plan returned error: %v", err)
	}
	if _, err := runBody(t, bodies, bodyDownloadPlan, planRequest{TenantID: "tenant_123", RunID: "run_123", TerraformPath: applyDir, PlanKey: key}, io.Discard); err != nil {
		t.Fatalf("download-plan returned error: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(applyDir, runner.PlanFileName))
	if err != nil || string(got) != "the plan" {
		t.Fatalf("restored plan = %q, %v", got, err)
	}
}
