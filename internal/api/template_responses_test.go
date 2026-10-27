package api

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vishu42/openplan/internal/domain"
)

// jsonKeys returns the top-level keys value marshals to, so a test can pin a
// response's field set exactly: a field added to the domain struct must not
// reach the wire unless a response type chooses to send it.
func jsonKeys(t *testing.T, value any) []string {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &fields))
	return slices.Sorted(maps.Keys(fields))
}

func fullTemplateRun() domain.TemplateRun {
	at := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	summary := &domain.PlanSummary{Add: 1, Change: 2, Destroy: 3}
	return domain.TemplateRun{
		ID: "run-1", TenantID: "tenant-1", StackTemplateID: "st-1", TemplateRevisionID: "rev-1",
		SourceTemplateID: "src-1", Operation: domain.OperationApply, SelectedRef: "main",
		ResolvedCommitSHA: "abc", WorkspaceName: "ws", ConfigJSON: json.RawMessage(`{"secret":"x"}`),
		BackendType: "s3", BackendConfigHash: "hash", Status: domain.TemplateRunCompleted,
		Step: domain.TemplateRunStepApplying, TriggerActor: "alice", TriggerActorDisplayName: "Alice",
		CreatedAt: at, CompletedAt: at, ErrorSummary: "none", RunNumber: 3, AutoApprove: true,
		PlanSummary: summary, PlanChangesState: domain.PlanChangesComplete, PlanChangesTotal: new(6),
		Executions: []domain.TemplateRunWorkflowExecution{{
			Phase: domain.RunPhasePlan, WorkflowID: "temporal-workflow-1", Actor: "alice",
			ActorDisplayName: "Alice", Status: domain.TemplateRunExecutionSucceeded,
			StartedAt: at, FinishedAt: at, ErrorSummary: "none", Summary: summary,
			Steps: []domain.TemplateRunStepExecution{{
				Step: domain.TemplateRunStepPlanning, Status: domain.TemplateRunExecutionSucceeded,
				StartedAt: at, FinishedAt: at,
			}},
		}},
	}
}

func TestTemplateRunResponseFields(t *testing.T) {
	t.Parallel()

	response := newTemplateRunResponse(fullTemplateRun())

	assert.Equal(t, []string{
		"auto_approve", "command_error", "completed_at", "created_at", "error_summary", "executions", "id",
		"operation", "plan_changes", "plan_summary", "resolved_commit_sha", "run_number", "selected_ref",
		"stack_template_id", "status", "step", "template_revision_id", "tenant_id",
		"trigger_actor", "trigger_actor_display_name",
	}, jsonKeys(t, response))
	// The run's config snapshot can hold sensitive values, and the backend
	// and workspace are the executor's business.
	for _, internal := range []string{"config_json", "backend_type", "backend_config_hash", "workspace_name", "source_template_id"} {
		assert.NotContains(t, jsonKeys(t, response), internal)
	}

	require.Len(t, response.Executions, 1)
	assert.Equal(t, []string{
		"actor", "actor_display_name", "command_error", "error_summary", "finished_at", "phase", "started_at",
		"status", "steps", "summary",
	}, jsonKeys(t, response.Executions[0]))
	assert.Equal(t, []string{"finished_at", "started_at", "status", "step"}, jsonKeys(t, response.Executions[0].Steps[0]))
	assert.Equal(t, []string{"add", "change", "destroy"}, jsonKeys(t, response.PlanSummary))
}

// Issue 522: what Terraform said reaches the run and its execution as it was
// stored, and an empty one is still sent, so a client reads "" rather than a
// missing field.
func TestTemplateRunResponseCarriesTheCommandError(t *testing.T) {
	t.Parallel()

	run := fullTemplateRun()
	run.CommandError = "Error: bad creds"
	run.Executions[0].CommandError = "Error: bad creds"
	response := newTemplateRunResponse(run)
	assert.Equal(t, "Error: bad creds", response.CommandError)
	assert.Equal(t, "Error: bad creds", response.Executions[0].CommandError)

	encoded, err := json.Marshal(newTemplateRunResponse(fullTemplateRun()))
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"command_error":""`)
}

// A run still in flight has no completion time, and a listed run carries no
// executions; both are left out rather than sent as zero values.
func TestTemplateRunResponseOmitsUnsetFields(t *testing.T) {
	t.Parallel()

	run := fullTemplateRun()
	run.CompletedAt = time.Time{}
	run.Executions = nil
	run.PlanSummary = nil

	keys := jsonKeys(t, newTemplateRunResponse(run))
	assert.NotContains(t, keys, "completed_at")
	assert.NotContains(t, keys, "executions")
	encoded, err := json.Marshal(newTemplateRunResponse(run))
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"plan_summary":null`)
}

func TestTemplateRunLogResponseFields(t *testing.T) {
	t.Parallel()

	response := newTemplateRunLogResponse(domain.TemplateRunLog{
		TenantID: "tenant-1", RunID: "run-1", Name: "plan-1", RunPhase: domain.RunPhasePlan,
		Seq: 1, Command: domain.TerraformCommandPlan, CommandLine: "tofu plan",
		State:     domain.TemplateRunLogComplete,
		ObjectKey: "tenants/tenant-1/runs/run-1/plan-1.log", ContentType: "text/plain",
		SizeBytes: 12, UploadedAt: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC),
	})

	assert.Equal(t, []string{
		"command", "command_line", "content_type", "name", "run_id", "run_phase", "seq",
		"size_bytes", "state", "tenant_id", "uploaded_at",
	}, jsonKeys(t, response))
	// name is what GET .../logs/{name} reads the log under.
	assert.Equal(t, "plan-1", response.Name)
}

// A complete log of zero bytes is an empty upload, which a client must be
// able to tell from a log with no upload at all.
func TestTemplateRunLogResponseSizeBytes(t *testing.T) {
	t.Parallel()

	complete := domain.TemplateRunLog{
		Name: "plan-1", State: domain.TemplateRunLogComplete, ContentType: "text/plain",
		SizeBytes: 0, UploadedAt: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC),
	}
	encoded, err := json.Marshal(newTemplateRunLogResponse(complete))
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"size_bytes":0`)

	live := domain.TemplateRunLog{Name: "plan-1", State: domain.TemplateRunLogLive, SizeBytes: 12}
	assert.NotContains(t, jsonKeys(t, newTemplateRunLogResponse(live)), "size_bytes")
}

func TestTemplateRevisionResponseFields(t *testing.T) {
	t.Parallel()

	response := newTemplateRevisionResponse(domain.TemplateRevision{
		ID: "rev-1", TenantID: "tenant-1", SourceTemplateID: "src-1", RepoOwner: "o", RepoName: "r",
		SourceRef: "main", ResolvedCommitSHA: "abc", RootPath: ".", Name: "n", Description: "d",
		Tags: []string{"t"}, Status: domain.TemplateRevisionActive, CreatedAt: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC),
	})

	assert.Equal(t, []string{
		"created_at", "description", "id", "name", "repo_name", "repo_owner", "resolved_commit_sha",
		"root_path", "source_ref", "source_template_id", "status", "tags", "tenant_id",
	}, jsonKeys(t, response))
}

// A revision registered without tags lists them as [], never null: every
// screen reads them as a list.
func TestTemplateRevisionResponseTagsNeverNull(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(newTemplateRevisionResponse(domain.TemplateRevision{ID: "rev-1"}))
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"tags":[]`)
}

func TestTemplateResponseFields(t *testing.T) {
	t.Parallel()

	response := newTemplateResponse(domain.SourceTemplate{
		ID: "source_template_1", TenantID: "tenant-1", RepoOwner: "o", RepoName: "r", SourceRef: "main",
		RootPath: ".", Name: "n", Description: "d", Tags: []string{"t"}, LatestTemplateRevisionID: "rev-1",
		CreatedAt: time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC),
	}, time.Date(2026, 10, 21, 11, 0, 0, 0, time.UTC))

	assert.Equal(t, []string{
		"created_at", "description", "id", "latest_sync", "latest_template_revision_id", "name", "repo_name",
		"repo_owner", "root_path", "source_ref", "tags", "tenant_id",
	}, jsonKeys(t, response))
}

// A template no sync has filled in yet still sends every field: tags as [],
// the revision id as "", and latest_sync as null.
func TestTemplateResponseBeforeTheFirstRevision(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(newTemplateResponse(domain.SourceTemplate{ID: "source_template_1"}, time.Now()))
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"tags":[]`)
	assert.Contains(t, string(encoded), `"latest_template_revision_id":""`)
	assert.Contains(t, string(encoded), `"latest_sync":null`)
}
