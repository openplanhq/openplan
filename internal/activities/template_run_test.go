package activities

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/githubapp"
	"github.com/vishu42/openplan/internal/logsink"
	gitrunner "github.com/vishu42/openplan/internal/runner"
	"github.com/vishu42/openplan/internal/runseal"
)

func TestRecordTemplateRunStatusDelegatesToRecorder(t *testing.T) {
	t.Parallel()

	recorder := &recordingStatusRecorder{}
	activities := NewControlActivities(&controlStoreStub{status: recorder}, nil, nil)
	input := domain.TemplateRunStatusActivityInput{
		RunID:           domain.TemplateRunID("run_123"),
		TenantID:        domain.TenantID("tenant_123"),
		StackTemplateID: domain.StackTemplateID("stack_template_123"),
		Operation:       domain.OperationPlan,
		Status:          domain.TemplateRunRunning,
	}

	if err := activities.RecordTemplateRunStatus(context.Background(), input); err != nil {
		t.Fatalf("RecordTemplateRunStatus returned error: %v", err)
	}

	if !reflect.DeepEqual(recorder.input, input) {
		t.Fatalf("recorded input = %#v, want %#v", recorder.input, input)
	}
}

func TestRecordTemplateRunStatusWrapsRecorderError(t *testing.T) {
	t.Parallel()

	recorderErr := errors.New("database unavailable")
	activities := NewControlActivities(&controlStoreStub{status: &recordingStatusRecorder{err: recorderErr}}, nil, nil)

	err := activities.RecordTemplateRunStatus(context.Background(), domain.TemplateRunStatusActivityInput{
		RunID:    domain.TemplateRunID("run_123"),
		TenantID: domain.TenantID("tenant_123"),
		Status:   domain.TemplateRunFailed,
	})
	if !errors.Is(err, recorderErr) {
		t.Fatalf("error = %v, want recorderErr", err)
	}
	if !strings.Contains(err.Error(), "record template run status") {
		t.Fatalf("error = %q, want status context", err.Error())
	}
}

func TestPrepareWorkspaceCreatesRunDirectory(t *testing.T) {
	t.Parallel()

	runRoot := t.TempDir()
	activities, _ := newTestTemplateRunActivities(t, runRoot, nil)
	input := domain.PrepareWorkspaceActivityInput{
		RunID:    domain.TemplateRunID("run_123"),
		TenantID: domain.TenantID("tenant_123"),
	}

	output, err := activities.PrepareWorkspace(context.Background(), input)
	if err != nil {
		t.Fatalf("PrepareWorkspace returned error: %v", err)
	}

	wantPath := filepath.Join(runRoot, "tenant_123", "run_123", "workspace")
	if output.WorkspacePath != wantPath {
		t.Fatalf("WorkspacePath = %q, want %q", output.WorkspacePath, wantPath)
	}
	info, err := os.Stat(wantPath)
	if err != nil {
		t.Fatalf("stat workspace path: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("workspace path %q is not a directory", wantPath)
	}
}

func TestPrepareWorkspaceRejectsEmptyRoot(t *testing.T) {
	t.Parallel()

	activities, _ := newTestTemplateRunActivities(t, "", nil)

	_, err := activities.PrepareWorkspace(context.Background(), domain.PrepareWorkspaceActivityInput{
		RunID:    domain.TemplateRunID("run_123"),
		TenantID: domain.TenantID("tenant_123"),
	})
	if err == nil {
		t.Fatal("PrepareWorkspace returned nil error, want error")
	}
	if !strings.Contains(err.Error(), "run root") {
		t.Fatalf("error = %q, want run root context", err.Error())
	}
}

func TestPrepareWorkspaceRejectsUnsafePathComponents(t *testing.T) {
	t.Parallel()

	activities, _ := newTestTemplateRunActivities(t, t.TempDir(), nil)

	_, err := activities.PrepareWorkspace(context.Background(), domain.PrepareWorkspaceActivityInput{
		RunID:    domain.TemplateRunID("run_123"),
		TenantID: domain.TenantID("../tenant_123"),
	})
	if err == nil {
		t.Fatal("PrepareWorkspace returned nil error, want error")
	}
	if !strings.Contains(err.Error(), "single path component") {
		t.Fatalf("error = %q, want the unsafe component refused", err.Error())
	}
}

// The revision is a commit, so the run checks out that commit. Following the
// ref instead would let the source move between a plan and the apply approved
// against it, and would ignore the revision entirely after an upgrade, since
// the installed ref is never updated to match the new revision.
func TestFetchSourceChecksOutTheResolvedCommitRatherThanTheRef(t *testing.T) {
	t.Parallel()

	git := &fakeGit{}
	activities, bodies := newTestTemplateRunActivities(t, t.TempDir(), nil)
	bodies.git = gitrunner.NewLocalGitRunnerWithExecutor(git)
	workspace, err := activities.PrepareWorkspace(context.Background(), domain.PrepareWorkspaceActivityInput{TenantID: "tenant_123", RunID: "run_123"})
	if err != nil {
		t.Fatalf("PrepareWorkspace returned error: %v", err)
	}
	workspacePath := workspace.WorkspacePath

	output, err := activities.FetchSource(context.Background(), domain.FetchSourceActivityInput{
		RunID:         domain.TemplateRunID("run_123"),
		TenantID:      domain.TenantID("tenant_123"),
		WorkspacePath: workspacePath,
		RepoOwner:     "acme",
		RepoName:      "infra-templates",
		// The ref the component was installed from is stale relative to the
		// revision this run is for; it must not decide what gets checked out.
		SourceRef:         "main",
		ResolvedCommitSHA: "a1b2c3d",
		RootPath:          "modules/vpc",
	})
	if err != nil {
		t.Fatalf("FetchSource returned error: %v", err)
	}

	if git.commitSHA != "a1b2c3d" {
		t.Fatalf("commitSHA = %q, want a1b2c3d", git.commitSHA)
	}
	if git.ref != "" {
		t.Fatalf("ref = %q, want the ref to be unused", git.ref)
	}
	if git.repoURL != "https://github.com/acme/infra-templates.git" {
		t.Fatalf("repoURL = %q", git.repoURL)
	}
	wantCloneDest := filepath.Join(workspacePath, "source")
	if git.dest != wantCloneDest {
		t.Fatalf("dest = %q, want %q", git.dest, wantCloneDest)
	}
	wantTerraformPath := filepath.Join(workspacePath, "source", "modules", "vpc")
	if output.TerraformPath != wantTerraformPath {
		t.Fatalf("TerraformPath = %q, want %q", output.TerraformPath, wantTerraformPath)
	}
}

// Runs queued before the commit was threaded through carry no commit, so they
// keep the old ref-based behaviour rather than failing. This path dies with the
// last such run.
func TestFetchSourceFallsBackToTheRefWhenNoCommitWasResolved(t *testing.T) {
	t.Parallel()

	git := &fakeGit{}
	activities, bodies := newTestTemplateRunActivities(t, t.TempDir(), nil)
	bodies.git = gitrunner.NewLocalGitRunnerWithExecutor(git)
	workspace, err := activities.PrepareWorkspace(context.Background(), domain.PrepareWorkspaceActivityInput{TenantID: "tenant_123", RunID: "run_123"})
	if err != nil {
		t.Fatalf("PrepareWorkspace returned error: %v", err)
	}
	workspacePath := workspace.WorkspacePath

	output, err := activities.FetchSource(context.Background(), domain.FetchSourceActivityInput{
		RunID:         domain.TemplateRunID("run_123"),
		TenantID:      domain.TenantID("tenant_123"),
		WorkspacePath: workspacePath,
		RepoOwner:     "acme",
		RepoName:      "infra-templates",
		SourceRef:     "main",
		RootPath:      "modules/vpc",
	})
	if err != nil {
		t.Fatalf("FetchSource returned error: %v", err)
	}

	if git.ref != "main" {
		t.Fatalf("ref = %q, want main", git.ref)
	}
	if git.commitSHA != "" {
		t.Fatalf("commitSHA = %q, want no commit checkout", git.commitSHA)
	}
	wantTerraformPath := filepath.Join(workspacePath, "source", "modules", "vpc")
	if output.TerraformPath != wantTerraformPath {
		t.Fatalf("TerraformPath = %q, want %q", output.TerraformPath, wantTerraformPath)
	}
}

func TestFetchSourceRejectsUnsafeRootPath(t *testing.T) {
	t.Parallel()

	git := &fakeGit{}
	activities, bodies := newTestTemplateRunActivities(t, t.TempDir(), nil)
	bodies.git = gitrunner.NewLocalGitRunnerWithExecutor(git)

	_, err := activities.FetchSource(context.Background(), domain.FetchSourceActivityInput{
		RunID:         domain.TemplateRunID("run_123"),
		TenantID:      domain.TenantID("tenant_123"),
		WorkspacePath: t.TempDir(),
		RepoOwner:     "acme",
		RepoName:      "infra-templates",
		SourceRef:     "main",
		RootPath:      "../secret",
	})
	if err == nil {
		t.Fatal("FetchSource returned nil error, want unsafe root path error")
	}
	if git.calls != 0 {
		t.Fatalf("git ran %d commands, want none", git.calls)
	}
}

// newTestTerraform returns activities that run tofu on executor, with
// tenant_123's run_123 prepared, and the run's workspace.
func newTestTerraform(t *testing.T, executor gitrunner.CommandExecutor) (*TemplateRunActivities, string) {
	t.Helper()
	activities, bodies := newTestTemplateRunActivities(t, t.TempDir(), nil)
	bodies.terraform = gitrunner.NewLocalTerraformRunnerWithExecutor(executor)
	return activities, prepareTestWorkspace(t, activities)
}

// planInput is a plan for tenant_123's run_123 in workspace.
func planInput(workspace string) domain.RunTerraformActivityInput {
	return domain.RunTerraformActivityInput{
		RunID:         domain.TemplateRunID("run_123"),
		TenantID:      domain.TenantID("tenant_123"),
		WorkspacePath: workspace,
		WorkspaceName: "mtp_acme_prod_vpc_a13f9c",
		Command:       domain.TerraformCommandPlan,
		LogName:       "plan",
	}
}

// runLog reads one of the run's logs, which sit beside its workspace.
func runLog(t *testing.T, workspace string, name string) string {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(filepath.Dir(workspace), "logs", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(got)
}

func TestRecordTemplateRunLogDelegatesToRecorder(t *testing.T) {
	t.Parallel()

	recorder := &recordingLogMetadataRecorder{}
	log := domain.TemplateRunLog{
		TenantID: "tenant_123", RunID: "run_123", Name: "plan-3", RunPhase: domain.RunPhasePlan, Seq: 3, Command: domain.TerraformCommandPlan,
		State:     domain.TemplateRunLogComplete,
		ObjectKey: "tenants/tenant_123/runs/run_123/logs/plan-3.log", ContentType: "text/plain; charset=utf-8",
		UploadedAt: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC),
	}

	if err := NewControlActivities(&controlStoreStub{logs: recorder}, nil, nil).RecordTemplateRunLog(context.Background(), log); err != nil {
		t.Fatalf("RecordTemplateRunLog returned error: %v", err)
	}
	if recorder.log != log {
		t.Fatalf("recorded log = %#v, want %#v", recorder.log, log)
	}

	// The executor reports the object key, but the API serves whatever the row
	// points at, so the key recorded is the one derived from the run and name.
	forged := log
	forged.ObjectKey = "tenants/tenant_victim/runs/run_victim/logs/plan-3.log"
	if err := NewControlActivities(&controlStoreStub{logs: recorder}, nil, nil).RecordTemplateRunLog(context.Background(), forged); err != nil {
		t.Fatalf("RecordTemplateRunLog returned error: %v", err)
	}
	if recorder.log.ObjectKey != log.ObjectKey {
		t.Fatalf("recorded object key = %q, want the derived %q", recorder.log.ObjectKey, log.ObjectKey)
	}

	// The name and key follow the phase and number, whatever name came in.
	renamed := log
	renamed.Name = "../plan"
	if err := NewControlActivities(&controlStoreStub{logs: recorder}, nil, nil).RecordTemplateRunLog(context.Background(), renamed); err != nil {
		t.Fatalf("RecordTemplateRunLog returned error: %v", err)
	}
	if recorder.log.Name != "plan-3" || recorder.log.ObjectKey != log.ObjectKey {
		t.Fatalf("recorded %q at %q, want plan-3 at %q", recorder.log.Name, recorder.log.ObjectKey, log.ObjectKey)
	}

	unnumbered := log
	unnumbered.Seq = 0
	if err := NewControlActivities(&controlStoreStub{logs: recorder}, nil, nil).RecordTemplateRunLog(context.Background(), unnumbered); err == nil {
		t.Fatal("RecordTemplateRunLog accepted a log with no number")
	}

	unknown := log
	unknown.Command = "refresh"
	if err := NewControlActivities(&controlStoreStub{logs: recorder}, nil, nil).RecordTemplateRunLog(context.Background(), unknown); err == nil {
		t.Fatal("RecordTemplateRunLog accepted an unknown command")
	}

	recorder.err = errors.New("database unavailable")
	err := NewControlActivities(&controlStoreStub{logs: recorder}, nil, nil).RecordTemplateRunLog(context.Background(), log)
	if !errors.Is(err, recorder.err) || !strings.Contains(err.Error(), "record template run log metadata") {
		t.Fatalf("error = %v, want wrapped recorder error", err)
	}
}

func TestRunTerraformWritesCommandLogFile(t *testing.T) {
	t.Parallel()

	executor := &recordingCommandExecutor{
		stdout: "plan stdout\n",
		stderr: "plan stderr\n",
	}
	activities, workspace := newTestTerraform(t, executor)
	input := planInput(workspace)
	input.ConfigJSON = []byte(`{"region":"us-east-1"}`)

	if _, err := activities.RunTerraform(context.Background(), input); err != nil {
		t.Fatalf("RunTerraform returned error: %v", err)
	}

	if got := runLog(t, workspace, "plan.log"); got != "plan stdout\nplan stderr\n" {
		t.Fatalf("plan log = %q", got)
	}
	if len(executor.env) != 0 {
		t.Fatalf("env = %#v, want none: variables travel in the var file", executor.env)
	}
	if executor.varFile != `{"region":"us-east-1"}` {
		t.Fatalf("var file = %q, want the run's config", executor.varFile)
	}
}

// A failed command is published too: its last lines are usually the ones
// that say why it failed.
func TestRunTerraformPublishesTheSpoolLive(t *testing.T) {
	t.Parallel()

	for name, commandErr := range map[string]error{
		"succeeded": nil,
		"failed":    errors.New("exit status 1"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { _ = client.Close() })
			activities, workspace := newTestTerraform(t, &recordingCommandExecutor{stdout: "hello\n", err: commandErr})
			activities.WithLiveLogs(client)
			input := planInput(workspace)
			input.LogName = "plan-1"

			if _, err := activities.RunTerraform(context.Background(), input); (err != nil) != (commandErr != nil) {
				t.Fatalf("RunTerraform error = %v, want one only if the command failed", err)
			}

			// The command ends well inside a flush interval: what is in the
			// stream is the final flush's, made before RunTerraform returned.
			messages, err := client.XRange(context.Background(), "runlog:{run_123}:plan-1", "-", "+").Result()
			if err != nil {
				t.Fatalf("XRANGE: %v", err)
			}
			if len(messages) != 1 {
				t.Fatalf("stream holds %d entries, want 1", len(messages))
			}
			entry, err := logsink.ParseEntry(messages[0])
			if err != nil {
				t.Fatalf("ParseEntry: %v", err)
			}
			if entry.Kind != logsink.KindPiece || entry.Offset != 0 || string(entry.Data) != "hello\n" {
				t.Fatalf("entry = %+v, want a piece at 0 of %q", entry, "hello\n")
			}
		})
	}
}

func TestRunTerraformWithoutLiveLogsStillSpools(t *testing.T) {
	t.Parallel()

	activities, workspace := newTestTerraform(t, &recordingCommandExecutor{stdout: "hello\n"})
	activities.WithLiveLogs(nil)
	input := planInput(workspace)
	input.LogName = "plan-1"

	if _, err := activities.RunTerraform(context.Background(), input); err != nil {
		t.Fatalf("RunTerraform returned error: %v", err)
	}
	if got := runLog(t, workspace, "plan-1.log"); got != "hello\n" {
		t.Fatalf("plan-1 log = %q, want %q", got, "hello\n")
	}
}

// RunTerraform spools to, and UploadLog uploads, the log the workflow named,
// so the apply phase's init cannot replace the plan phase's init log.
func TestRunTerraformLogsUnderTheWorkflowsName(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		runPhase domain.RunPhase
		want     string
	}{
		{runPhase: domain.RunPhasePlan, want: "plan-1"},
		{runPhase: domain.RunPhaseApply, want: "apply-1"},
	} {
		activities, bodies := newTestTemplateRunActivities(t, t.TempDir(), nil)
		bodies.terraform = gitrunner.NewLocalTerraformRunnerWithExecutor(&recordingCommandExecutor{stdout: "init stdout\n"})
		logStore := &recordingTemplateRunLogStore{}
		bodies.logs = logStore
		input := planInput(prepareTestWorkspace(t, activities))
		input.Command, input.LogName = domain.TerraformCommandInit, tt.want

		if _, err := activities.RunTerraform(context.Background(), input); err != nil {
			t.Fatalf("RunTerraform(%s) returned error: %v", tt.runPhase, err)
		}
		if _, err := activities.UploadLog(context.Background(), domain.UploadLogActivityInput{
			TenantID: input.TenantID, RunID: input.RunID, LogName: input.LogName,
		}); err != nil {
			t.Fatalf("UploadLog(%s) returned error: %v", tt.runPhase, err)
		}
		if logStore.name != tt.want {
			t.Fatalf("%s init uploaded log %q, want %q", tt.runPhase, logStore.name, tt.want)
		}
		if logStore.content != "init stdout\n" {
			t.Fatalf("uploaded content = %q", logStore.content)
		}
	}
}

// TestRunTerraformHeartbeatsWhileTheCommandRuns covers what a heartbeat is
// for: an apply that legitimately runs for half an hour has to keep telling
// Temporal it is alive, or Temporal fails the activity at the heartbeat
// timeout long before the command's real budget is spent. The silent case is
// the important one -- a command producing no output at all still has to
// report, because output is not what keeps it alive.
func TestRunTerraformHeartbeatsWhileTheCommandRuns(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		stdout string
	}{
		{name: "with output", stdout: "plan stdout\n"},
		{name: "silent", stdout: ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			executor := &blockingCommandExecutor{
				stdout:  testCase.stdout,
				release: make(chan struct{}),
			}
			heartbeats := newHeartbeatRecorder()
			activities, workspace := newTestTerraform(t, executor)
			activities.heartbeat = heartbeats.record
			activities.heartbeatInterval = time.Millisecond

			done := make(chan error, 1)
			go func() {
				_, err := activities.RunTerraform(context.Background(), planInput(workspace))
				done <- err
			}()

			select {
			case <-heartbeats.first:
			case <-time.After(5 * time.Second):
				t.Fatal("no heartbeat was recorded while the command was running")
			}
			close(executor.release)
			if err := <-done; err != nil {
				t.Fatalf("RunTerraform returned error: %v", err)
			}
		})
	}
}

// TestRunTerraformStopsHeartbeatingWhenTheCommandEnds guards the lifetime of
// the heartbeat goroutine. A heartbeat recorded after the activity returned is
// reported against a finished activity, and a goroutine per Terraform command
// that never exits is a leak on a long-lived executor.
func TestRunTerraformStopsHeartbeatingWhenTheCommandEnds(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	close(release)
	heartbeats := newHeartbeatRecorder()
	activities, workspace := newTestTerraform(t, &blockingCommandExecutor{stdout: "plan stdout\n", release: release})
	activities.heartbeat = heartbeats.record
	activities.heartbeatInterval = time.Millisecond

	if _, err := activities.RunTerraform(context.Background(), planInput(workspace)); err != nil {
		t.Fatalf("RunTerraform returned error: %v", err)
	}

	settled := heartbeats.count()
	time.Sleep(50 * time.Millisecond)
	if got := heartbeats.count(); got != settled {
		t.Fatalf("heartbeats after the command ended = %d, want none beyond the %d recorded during it", got-settled, settled)
	}
}

// TestRecordActivityHeartbeatIsSafeOffAnActivity pins the guard that lets
// RunTerraform be driven directly by tests: the Temporal SDK panics when asked
// to heartbeat on a context that is not an activity's.
func TestRecordActivityHeartbeatIsSafeOffAnActivity(t *testing.T) {
	t.Parallel()

	recordActivityHeartbeat(context.Background())
}

func TestRunTerraformWrapsRunnerError(t *testing.T) {
	t.Parallel()

	runnerErr := errors.New("terraform failed")
	activities, workspace := newTestTerraform(t, &recordingCommandExecutor{err: runnerErr})
	input := planInput(workspace)
	input.Command, input.LogName = domain.TerraformCommandApply, "apply"

	_, err := activities.RunTerraform(context.Background(), input)
	// tofu ran in a sandbox, so its error arrives as text.
	if err == nil || !strings.Contains(err.Error(), runnerErr.Error()) {
		t.Fatalf("error = %v, want runnerErr", err)
	}
	if !strings.Contains(err.Error(), "run terraform") {
		t.Fatalf("error = %q, want run terraform context", err.Error())
	}
}

type recordingStatusRecorder struct {
	input domain.TemplateRunStatusActivityInput
	err   error
}

func (recorder *recordingStatusRecorder) RecordTemplateRunStatus(_ context.Context, input domain.TemplateRunStatusActivityInput) error {
	recorder.input = input
	return recorder.err
}

type recordingLogMetadataRecorder struct {
	log domain.TemplateRunLog
	err error
}

func (recorder *recordingLogMetadataRecorder) RecordTemplateRunLog(_ context.Context, log domain.TemplateRunLog) error {
	recorder.log = log
	return recorder.err
}

// fakeGit stands in for the git executable behind runner.LocalGitRunner. It
// reads back from each command what the activity asked git for, and lays out a
// finished clone where git would leave one.
type fakeGit struct {
	repoURL   string
	ref       string
	commitSHA string
	dest      string
	// token is the installation token the networked commands authenticated
	// with, empty when they authenticated nothing.
	token string
	// head is what rev-parse HEAD prints.
	head string
	// populate fills a finished clone; nil lays out an empty modules/vpc, the
	// template root most tests name.
	populate func(dest string) error
	// err fails the networked commands, clone and fetch, as a remote would.
	err   error
	calls int
}

// Run answers the commands LocalGitRunner issues: clone for a ref; init, fetch
// and checkout for a commit; rev-parse for the head.
func (git *fakeGit) Run(_ context.Context, _ string, env []string, stdout io.Writer, _ io.Writer, _ string, args ...string) error {
	git.calls++
	// The commands that work inside a clone name it first, with -C.
	command := args
	if len(command) > 2 && command[0] == "-C" {
		command = command[2:]
	}
	last := func(i int) string { return args[len(args)-i] }
	switch command[0] {
	case "clone": // clone --depth 1 --branch <ref> <url> <dest>
		git.ref, git.repoURL, git.dest, git.token = last(3), last(2), last(1), gitToken(env)
		if git.err != nil {
			return git.err
		}
		return git.finish()
	case "init": // init --quiet <dest>
		git.dest = last(1)
	case "fetch": // -C <dest> fetch --depth 1 <url> <commit>
		git.repoURL, git.commitSHA, git.token = last(2), last(1), gitToken(env)
		return git.err
	case "checkout":
		return git.finish()
	case "rev-parse":
		_, err := io.WriteString(stdout, git.head+"\n")
		return err
	}
	return nil
}

func (git *fakeGit) finish() error {
	if git.populate != nil {
		return git.populate(git.dest)
	}
	return os.MkdirAll(filepath.Join(git.dest, "modules", "vpc"), 0o700)
}

// gitToken reads the installation token back out of the Authorization header
// gitEnvironment hands git, so a test asserts on the token the remote would
// receive rather than on how the runner encodes it.
func gitToken(env []string) string {
	for _, entry := range env {
		_, header, ok := strings.Cut(entry, "Authorization: Basic ")
		if !ok {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(header)
		if err != nil {
			// Still non-empty, so a test expecting no token fails on it.
			return header
		}
		return strings.TrimPrefix(string(decoded), "x-access-token:")
	}
	return ""
}

type recordingTemplateRunLogStore struct {
	tenantID domain.TenantID
	runID    domain.TemplateRunID
	name     string
	content  string
	returned domain.TemplateRunLog
	err      error
}

func (store *recordingTemplateRunLogStore) PutTemplateRunLog(_ context.Context, tenantID domain.TenantID, runID domain.TemplateRunID, name string, body io.Reader) (domain.TemplateRunLog, error) {
	store.tenantID = tenantID
	store.runID = runID
	store.name = name
	content, err := io.ReadAll(body)
	if err != nil {
		return domain.TemplateRunLog{}, err
	}
	store.content = string(content)
	store.returned = domain.TemplateRunLog{TenantID: tenantID, RunID: runID, Name: name, ObjectKey: "logs/" + name + ".log", SizeBytes: int64(len(content))}
	return store.returned, store.err
}

// blockingCommandExecutor writes its output and then waits, standing in for an
// OpenTofu process that is still working when the heartbeat ticks.
type blockingCommandExecutor struct {
	stdout  string
	release chan struct{}
}

func (executor *blockingCommandExecutor) Run(ctx context.Context, _ string, _ []string, stdout io.Writer, _ io.Writer, _ string, _ ...string) error {
	if _, err := io.WriteString(stdout, executor.stdout); err != nil {
		return err
	}
	select {
	case <-executor.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// heartbeatRecorder counts what a running command reported and closes first as
// soon as anything is reported. Heartbeats arrive on the runner's own
// goroutine, so the recorder is guarded and never blocks it: a recorder that
// blocked would deadlock the stop it is observing.
type heartbeatRecorder struct {
	mutex sync.Mutex
	beats int
	first chan struct{}
	once  sync.Once
}

func newHeartbeatRecorder() *heartbeatRecorder {
	return &heartbeatRecorder{first: make(chan struct{})}
}

func (recorder *heartbeatRecorder) record(_ context.Context) {
	recorder.mutex.Lock()
	recorder.beats++
	recorder.mutex.Unlock()
	recorder.once.Do(func() { close(recorder.first) })
}

func (recorder *heartbeatRecorder) count() int {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	return recorder.beats
}

type recordingCommandExecutor struct {
	stdout string
	stderr string
	env    []string
	// varFile is what the command's -var-file held, read while it ran: the
	// runner removes the file once the command returns.
	varFile string
	err     error
}

func (executor *recordingCommandExecutor) Run(_ context.Context, _ string, env []string, stdout io.Writer, stderr io.Writer, _ string, args ...string) error {
	executor.env = append([]string(nil), env...)
	for _, arg := range args {
		if path, ok := strings.CutPrefix(arg, "-var-file="); ok {
			contents, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			executor.varFile = string(contents)
		}
	}
	if _, err := io.WriteString(stdout, executor.stdout); err != nil {
		return err
	}
	if _, err := io.WriteString(stderr, executor.stderr); err != nil {
		return err
	}
	return executor.err
}

// fetchWithSealedToken drives a run's source fetch across the plane split: the
// control plane resolves and seals the token to a key the executor generated,
// then the executor opens it and checks out.
func fetchWithSealedToken(t *testing.T, tokens GitHubTokenSource, git *fakeGit, owner string, repo string) error {
	t.Helper()

	executor, bodies := newTestTemplateRunActivities(t, t.TempDir(), nil)
	bodies.git = gitrunner.NewLocalGitRunnerWithExecutor(git)
	workspace, err := executor.PrepareWorkspace(context.Background(), domain.PrepareWorkspaceActivityInput{TenantID: "tenant_123", RunID: "run_123"})
	if err != nil {
		t.Fatalf("PrepareWorkspace returned error: %v", err)
	}
	sealed, err := NewControlActivities(nil, tokens, nil).SealSourceToken(context.Background(), domain.SealSourceTokenActivityInput{
		RepoOwner: owner,
		RepoName:  repo,
		PublicKey: workspace.PublicKey,
	})
	if err != nil {
		t.Fatalf("SealSourceToken returned error: %v", err)
	}
	if strings.Contains(string(sealed.SealedToken), "ghs_") {
		t.Fatal("sealed token contains the plaintext token")
	}

	_, err = executor.FetchSource(context.Background(), domain.FetchSourceActivityInput{
		TenantID:          "tenant_123",
		RunID:             "run_123",
		WorkspacePath:     workspace.WorkspacePath,
		RepoOwner:         owner,
		RepoName:          repo,
		ResolvedCommitSHA: "a1b2c3d",
		RootPath:          "modules/vpc",
		SealedToken:       sealed.SealedToken,
		FetchHint:         sealed.FetchHint,
	})
	return err
}

// A run's checkout needs the credential just as a sync's clone does;
// this is the path that feeds Terraform.
func TestFetchSourcePassesInstallationTokenToCheckout(t *testing.T) {
	t.Parallel()

	git := &fakeGit{}
	if err := fetchWithSealedToken(t, stubTokenSource{token: "ghs_minted"}, git, "acme", "private-infra"); err != nil {
		t.Fatalf("FetchSource returned error: %v", err)
	}
	if git.token != "ghs_minted" {
		t.Fatal("checkout did not receive the installation credential")
	}
}

// A missing installation is not itself a failure. The checkout still goes
// ahead unauthenticated, and only once that checkout fails for its own reason
// does the missing installation become the user's problem.
func TestFetchSourceReportsUninstalledApp(t *testing.T) {
	t.Parallel()

	checkoutErr := errors.New("authentication required")
	err := fetchWithSealedToken(t, stubTokenSource{err: githubapp.ErrAppNotInstalled}, &fakeGit{err: checkoutErr}, "acme", "private-infra")
	// The checkout ran in a sandbox, so its error arrives as text.
	if err == nil || !strings.Contains(err.Error(), checkoutErr.Error()) {
		t.Fatalf("error = %v, want the underlying checkout failure", err)
	}
	if !strings.Contains(err.Error(), "acme/private-infra") {
		t.Fatalf("error = %v, want it to name the repository", err)
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("error = %v, want it to name the missing installation", err)
	}
}

// The regression this guards: configuring a GitHub App must not break
// fetching source from a public repository it was never installed on. A 404
// from the installation lookup cannot be told apart from that case, so the
// fetch must fall back to an unauthenticated checkout and succeed exactly as it
// would with no token source configured at all.
func TestFetchSourceSucceedsWithZeroCredentialWhenAppNotInstalled(t *testing.T) {
	t.Parallel()

	git := &fakeGit{}
	if err := fetchWithSealedToken(t, stubTokenSource{err: githubapp.ErrAppNotInstalled}, git, "hashicorp", "terraform-aws-modules"); err != nil {
		t.Fatalf("FetchSource returned error: %v", err)
	}
	if git.token != "" {
		t.Fatal("checkout received a non-zero credential for a repo the App is not installed on")
	}
}

// The same for a GitHub API failure that is not a missing installation: it must
// not fail a run against a public repository, which never needed the token.
func TestFetchSourceSucceedsWithZeroCredentialWhenTokenLookupFails(t *testing.T) {
	t.Parallel()

	git := &fakeGit{}
	tokens := stubTokenSource{err: errors.New("resolve installation: unexpected status 503 Service Unavailable")}
	if err := fetchWithSealedToken(t, tokens, git, "hashicorp", "terraform-aws-modules"); err != nil {
		t.Fatalf("FetchSource returned error: %v", err)
	}
	if git.token != "" {
		t.Fatal("checkout received a non-zero credential after the token lookup failed")
	}
}

// And when the unauthenticated checkout fails, the run's error must name the
// token lookup that failed rather than an installation that is fine.
func TestFetchSourceReportsTokenLookupFailure(t *testing.T) {
	t.Parallel()

	checkoutErr := errors.New("authentication required")
	lookupErr := errors.New("resolve installation: unexpected status 503 Service Unavailable")
	err := fetchWithSealedToken(t, stubTokenSource{err: lookupErr}, &fakeGit{err: checkoutErr}, "acme", "private-infra")
	// The checkout ran in a sandbox, so its error arrives as text.
	if err == nil || !strings.Contains(err.Error(), checkoutErr.Error()) {
		t.Fatalf("error = %v, want the underlying checkout failure", err)
	}
	if !strings.Contains(err.Error(), "503") {
		t.Fatalf("error = %v, want it to name the token lookup failure", err)
	}
	if strings.Contains(err.Error(), "not installed") {
		t.Fatalf("error = %v, must not blame a missing installation for an API failure", err)
	}
}

// With no App configured the fetch is unauthenticated and needs no hint.
func TestFetchSourceWithoutTokenSourceIsUnauthenticated(t *testing.T) {
	t.Parallel()

	git := &fakeGit{}
	if err := fetchWithSealedToken(t, nil, git, "hashicorp", "terraform-aws-modules"); err != nil {
		t.Fatalf("FetchSource returned error: %v", err)
	}
	if git.token != "" {
		t.Fatal("checkout received a credential with no token source configured")
	}
}

// Credentials reach Terraform only through the run's key: sealed on the control
// plane, opened on the executor, never present in plaintext in between.
func TestRunCredentialsRoundTripThroughTheRunKey(t *testing.T) {
	t.Parallel()

	tofu := &recordingCommandExecutor{}
	executor, bodies := newTestTemplateRunActivities(t, t.TempDir(), nil)
	bodies.terraform = gitrunner.NewLocalTerraformRunnerWithExecutor(tofu)
	workspace, err := executor.PrepareWorkspace(context.Background(), domain.PrepareWorkspaceActivityInput{TenantID: "tenant_123", RunID: "run_123"})
	if err != nil {
		t.Fatalf("PrepareWorkspace returned error: %v", err)
	}
	publicKey := workspace.PublicKey
	store := &controlStoreStub{credentials: []domain.Credential{
		{StackID: "stack_123", Name: "AWS_SECRET_ACCESS_KEY", Ciphertext: "canary-7f3a", Sensitive: true},
	}}
	sealed, err := NewControlActivities(store, nil, nil).SealRunCredentials(context.Background(), domain.SealRunCredentialsActivityInput{
		TenantID:        "tenant_123",
		StackTemplateID: "stack_template_123",
		PublicKey:       publicKey,
	})
	if err != nil {
		t.Fatalf("SealRunCredentials returned error: %v", err)
	}
	if strings.Contains(string(sealed.SealedEnvironment), "canary-7f3a") {
		t.Fatal("sealed environment contains the plaintext credential")
	}

	input := planInput(workspace.WorkspacePath)
	input.SealedEnvironment = sealed.SealedEnvironment
	if _, err := executor.RunTerraform(context.Background(), input); err != nil {
		t.Fatalf("RunTerraform returned error: %v", err)
	}
	if !slices.Contains(tofu.env, "AWS_SECRET_ACCESS_KEY=decrypted:canary-7f3a") {
		t.Fatalf("terraform environment = %v, want the opened credential", tofu.env)
	}

	// After release, the same sealed value is unreadable.
	if err := executor.ReleaseRunKey(context.Background(), domain.ReleaseRunKeyActivityInput{TenantID: "tenant_123", RunID: "run_123"}); err != nil {
		t.Fatalf("ReleaseRunKey returned error: %v", err)
	}
	_, err = executor.RunTerraform(context.Background(), input)
	if !errors.Is(err, runseal.ErrNoKey) {
		t.Fatalf("error after release = %v, want runseal.ErrNoKey", err)
	}
}

// PrepareWorkspace hands back the key the run's secrets get sealed to.
func TestPrepareWorkspaceReturnsRunPublicKey(t *testing.T) {
	t.Parallel()

	activities, _ := newTestTemplateRunActivities(t, t.TempDir(), nil)
	output, err := activities.PrepareWorkspace(context.Background(), domain.PrepareWorkspaceActivityInput{
		TenantID: "tenant_123",
		RunID:    "run_123",
	})
	if err != nil {
		t.Fatalf("PrepareWorkspace returned error: %v", err)
	}
	sealed, err := runseal.Seal(output.PublicKey, "value")
	if err != nil {
		t.Fatalf("Seal returned error: %v", err)
	}
	var opened string
	if err := activities.keys.Open(domain.RunKeyID("tenant_123", "run_123"), sealed, &opened); err != nil || opened != "value" {
		t.Fatalf("Open = %q, %v; want the key PrepareWorkspace returned to open", opened, err)
	}
}

// controlStoreStub satisfies ControlStore, delegating the writes to recorders a
// test cares about.
type controlStoreStub struct {
	credentials []domain.Credential
	status      *recordingStatusRecorder
	logs        *recordingLogMetadataRecorder
	planKey     []byte
	finished    domain.FinishPlanActivityInput
	planChanges *domain.PlanChanges
	outcome     domain.PlanOutcome
	claimed     bool
	step        domain.TemplateRunStepActivityInput
	event       domain.TemplateRunEventActivityInput
	eventErr    error
}

func (store *controlStoreStub) RecordTemplateRunStatus(ctx context.Context, input domain.TemplateRunStatusActivityInput) error {
	if store.status == nil {
		return nil
	}
	return store.status.RecordTemplateRunStatus(ctx, input)
}

func (store *controlStoreStub) RecordTemplateRunLog(ctx context.Context, log domain.TemplateRunLog) error {
	if store.logs == nil {
		return nil
	}
	return store.logs.RecordTemplateRunLog(ctx, log)
}

func (store *controlStoreStub) ListCredentialsForStackTemplate(context.Context, domain.TenantID, domain.StackTemplateID) ([]domain.Credential, error) {
	return store.credentials, nil
}

func (*controlStoreStub) Decrypt(value string) (string, error) {
	return "decrypted:" + value, nil
}

func (store *controlStoreStub) CreatePlanKey(context.Context, domain.TenantID, domain.TemplateRunID) ([]byte, error) {
	return store.planKey, nil
}

func (store *controlStoreStub) PlanKey(context.Context, domain.TenantID, domain.TemplateRunID) ([]byte, error) {
	if store.planKey == nil {
		return nil, errors.New("run has no plan key")
	}
	return store.planKey, nil
}

func (store *controlStoreStub) FinishTemplatePlan(_ context.Context, input domain.FinishPlanActivityInput, changes *domain.PlanChanges) (domain.PlanOutcome, error) {
	store.planChanges = changes
	store.finished = input
	return store.outcome, nil
}

func (store *controlStoreStub) BeginTemplateApply(context.Context, domain.TenantID, domain.TemplateRunID, bool, string) (bool, error) {
	return store.claimed, nil
}

func (store *controlStoreStub) RecordTemplateRunStep(ctx context.Context, input domain.TemplateRunStepActivityInput) error {
	store.step = input
	return nil
}

func (store *controlStoreStub) RecordTemplateRunEvent(ctx context.Context, input domain.TemplateRunEventActivityInput) error {
	store.event = input
	return store.eventErr
}

func TestRecordTemplateRunStepDelegatesToTheStore(t *testing.T) {
	t.Parallel()

	store := &controlStoreStub{}
	input := domain.TemplateRunStepActivityInput{TenantID: "tenant_123", RunID: "run_123", Step: domain.TemplateRunStepFetchingSource}

	if err := NewControlActivities(store, nil, nil).RecordTemplateRunStep(context.Background(), input); err != nil {
		t.Fatalf("RecordTemplateRunStep returned error: %v", err)
	}
	if store.step != input {
		t.Fatalf("recorded step = %#v, want %#v", store.step, input)
	}
}

func TestRecordTemplateRunEventDelegatesToTheStore(t *testing.T) {
	t.Parallel()

	storeErr := errors.New("database unavailable")
	store := &controlStoreStub{eventErr: storeErr}
	input := domain.TemplateRunEventActivityInput{
		TenantID: "tenant_123", RunID: "run_123", StackTemplateID: "stack_template_123",
		Operation: domain.OperationApply, Event: domain.TemplateRunApplied,
	}

	err := NewControlActivities(store, nil, nil).RecordTemplateRunEvent(context.Background(), input)
	if !errors.Is(err, storeErr) || !strings.Contains(err.Error(), "record template run event") {
		t.Fatalf("error = %v, want the store's error with event context", err)
	}
	if !reflect.DeepEqual(store.event, input) {
		t.Fatalf("recorded event = %#v, want %#v", store.event, input)
	}
}

// prepareTestWorkspace lays out tenant_123/run_123's run directory and key, as
// every later step needs, and returns its workspace.
func prepareTestWorkspace(t *testing.T, activities *TemplateRunActivities) string {
	t.Helper()
	workspace, err := activities.PrepareWorkspace(context.Background(), domain.PrepareWorkspaceActivityInput{TenantID: "tenant_123", RunID: "run_123"})
	if err != nil {
		t.Fatalf("PrepareWorkspace returned error: %v", err)
	}
	return workspace.WorkspacePath
}
