package api

import (
	"encoding/json"
	"time"

	"github.com/vishu42/openplan/internal/domain"
)

// The response types below are the wire shape of runs, registrations,
// revisions and logs. The domain structs they are built from also carry what
// the executor and the store need, so they are never serialised as they are:
// a field reaches a client only when one of these types names it.

// templateRunResponse leaves out what only the executor reads: the config
// snapshot (it can hold sensitive values), the state backend and its hash,
// the Terraform workspace, and the source template, which the stack template
// already names.
type templateRunResponse struct {
	ID                      string                         `json:"id"`
	TenantID                string                         `json:"tenant_id"`
	StackTemplateID         string                         `json:"stack_template_id"`
	TemplateRevisionID      string                         `json:"template_revision_id"`
	Operation               string                         `json:"operation"`
	SelectedRef             string                         `json:"selected_ref"`
	ResolvedCommitSHA       string                         `json:"resolved_commit_sha"`
	Status                  string                         `json:"status"`
	Step                    string                         `json:"step"`
	TriggerActor            string                         `json:"trigger_actor"`
	TriggerActorDisplayName string                         `json:"trigger_actor_display_name"`
	CreatedAt               time.Time                      `json:"created_at"`
	CompletedAt             time.Time                      `json:"completed_at,omitzero"`
	ErrorSummary            string                         `json:"error_summary"`
	RunNumber               int                            `json:"run_number"`
	AutoApprove             bool                           `json:"auto_approve"`
	PlanSummary             *planSummaryResponse           `json:"plan_summary"`
	PlanChanges             *planChangesResponse           `json:"plan_changes"`
	Executions              []templateRunExecutionResponse `json:"executions,omitzero"`
}

// planChangesResponse says what GET .../changes holds for the run: every
// change (complete), the first of more (truncated, with total counting them
// all), or none although the plan had changes (unavailable). Null until a
// plan with changes finishes.
type planChangesResponse struct {
	State string `json:"state"`
	Total *int   `json:"total"`
}

// planResourceChangeResponse is one resource a run's plan changes. before and
// after are the resource's values with sensitive ones already replaced by
// "(sensitive)" and unknown ones by "(known after apply)".
type planResourceChangeResponse struct {
	Address             string          `json:"address"`
	PreviousAddress     string          `json:"previous_address"`
	ModuleAddress       string          `json:"module_address"`
	Mode                string          `json:"mode"`
	Type                string          `json:"type"`
	Name                string          `json:"name"`
	ProviderName        string          `json:"provider_name"`
	Deposed             string          `json:"deposed"`
	Action              string          `json:"action"`
	CreateBeforeDestroy bool            `json:"create_before_destroy"`
	ActionReason        string          `json:"action_reason"`
	ImportingID         string          `json:"importing_id"`
	ReplacePaths        json.RawMessage `json:"replace_paths"`
	Before              json.RawMessage `json:"before"`
	After               json.RawMessage `json:"after"`
}

func newPlanChangesResponse(run domain.TemplateRun) *planChangesResponse {
	if run.PlanChangesState == "" {
		return nil
	}
	return &planChangesResponse{State: string(run.PlanChangesState), Total: run.PlanChangesTotal}
}

func newPlanResourceChangeResponse(change domain.PlanResourceChange) planResourceChangeResponse {
	return planResourceChangeResponse{
		Address:             change.Address,
		PreviousAddress:     change.PreviousAddress,
		ModuleAddress:       change.ModuleAddress,
		Mode:                change.Mode,
		Type:                change.Type,
		Name:                change.Name,
		ProviderName:        change.ProviderName,
		Deposed:             change.Deposed,
		Action:              string(change.Action),
		CreateBeforeDestroy: change.CreateBeforeDestroy,
		ActionReason:        change.ActionReason,
		ImportingID:         change.ImportingID,
		ReplacePaths:        jsonOrNull(change.ReplacePaths),
		Before:              jsonOrNull(change.Before),
		After:               jsonOrNull(change.After),
	}
}

// jsonOrNull answers a missing value as null rather than leaving it out, so
// every change has the same fields.
func jsonOrNull(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("null")
	}
	return raw
}

// templateRunExecutionResponse leaves out the Temporal workflow ID, which
// names an internal system and is no use to a client.
type templateRunExecutionResponse struct {
	Phase            string                    `json:"phase"`
	Actor            string                    `json:"actor"`
	ActorDisplayName string                    `json:"actor_display_name"`
	Status           string                    `json:"status"`
	StartedAt        time.Time                 `json:"started_at"`
	FinishedAt       time.Time                 `json:"finished_at,omitzero"`
	ErrorSummary     string                    `json:"error_summary"`
	Summary          *planSummaryResponse      `json:"summary"`
	Steps            []templateRunStepResponse `json:"steps"`
}

type templateRunStepResponse struct {
	Step       string    `json:"step"`
	Status     string    `json:"status"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitzero"`
}

type planSummaryResponse struct {
	Add     int `json:"add"`
	Change  int `json:"change"`
	Destroy int `json:"destroy"`
}

// templateRunLogResponse leaves out the object-store key the log is kept
// under. Name is the domain's Phase, sent under the name the log is read by
// (GET .../logs/{name}): it is a key, not a run phase.
type templateRunLogResponse struct {
	TenantID    string `json:"tenant_id"`
	RunID       string `json:"run_id"`
	Name        string `json:"name"`
	RunPhase    string `json:"run_phase"`
	Seq         int    `json:"seq"`
	Command     string `json:"command"`
	CommandLine string `json:"command_line"`
	State       string `json:"state"`
	LostReason  string `json:"lost_reason,omitempty"`
	// The upload fields describe the stored body, so a log that isn't
	// complete leaves all three out rather than sending zeros a client could
	// read as an empty upload. SizeBytes is a pointer so that a complete log
	// of zero bytes still says so.
	ContentType string    `json:"content_type,omitempty"`
	SizeBytes   *int64    `json:"size_bytes,omitempty"`
	UploadedAt  time.Time `json:"uploaded_at,omitzero"`
}

// templateResponse is a registered source. Name, description and tags are
// its latest revision's; before the first sync records one, the name is the
// fallback the sync would use and the revision id is "". LatestSync is null
// until a sync is requested.
type templateResponse struct {
	ID                       string              `json:"id"`
	TenantID                 string              `json:"tenant_id"`
	RepoOwner                string              `json:"repo_owner"`
	RepoName                 string              `json:"repo_name"`
	SourceRef                string              `json:"source_ref"`
	RootPath                 string              `json:"root_path"`
	Name                     string              `json:"name"`
	Description              string              `json:"description"`
	Tags                     []string            `json:"tags"`
	LatestTemplateRevisionID string              `json:"latest_template_revision_id"`
	Installed                bool                `json:"installed"`
	LatestSync               *latestSyncResponse `json:"latest_sync"`
	CreatedAt                time.Time           `json:"created_at"`
}

// latestSyncResponse is a template's newest sync. Status is the shown one,
// derived when the response is built (stalled is never stored), so it is not
// the registration's status.
type latestSyncResponse struct {
	Status            string    `json:"status"`
	ErrorSummary      string    `json:"error_summary"`
	ResolvedCommitSHA string    `json:"resolved_commit_sha"`
	RequestedAt       time.Time `json:"requested_at"`
	CompletedAt       time.Time `json:"completed_at,omitzero"`
}

type templateRevisionResponse struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	// SourceTemplateID names the template the revision belongs to.
	SourceTemplateID  string    `json:"source_template_id"`
	RepoOwner         string    `json:"repo_owner"`
	RepoName          string    `json:"repo_name"`
	SourceRef         string    `json:"source_ref"`
	ResolvedCommitSHA string    `json:"resolved_commit_sha"`
	RootPath          string    `json:"root_path"`
	Name              string    `json:"name"`
	Description       string    `json:"description"`
	Tags              []string  `json:"tags"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
}

func newTemplateRunResponse(run domain.TemplateRun) templateRunResponse {
	response := templateRunResponse{
		ID:                      string(run.ID),
		TenantID:                string(run.TenantID),
		StackTemplateID:         string(run.StackTemplateID),
		TemplateRevisionID:      string(run.TemplateRevisionID),
		Operation:               string(run.Operation),
		SelectedRef:             run.SelectedRef,
		ResolvedCommitSHA:       run.ResolvedCommitSHA,
		Status:                  string(run.Status),
		Step:                    string(run.Step),
		TriggerActor:            string(run.TriggerActor),
		TriggerActorDisplayName: run.TriggerActorDisplayName,
		CreatedAt:               run.CreatedAt,
		CompletedAt:             run.CompletedAt,
		ErrorSummary:            run.ErrorSummary,
		RunNumber:               run.RunNumber,
		AutoApprove:             run.AutoApprove,
		PlanSummary:             newPlanSummaryResponse(run.PlanSummary),
		PlanChanges:             newPlanChangesResponse(run),
	}
	// A list leaves executions nil, which omits the field; a read of one run
	// fills them, an empty slice for a run no workflow has carried yet.
	if run.Executions != nil {
		response.Executions = make([]templateRunExecutionResponse, 0, len(run.Executions))
		for _, execution := range run.Executions {
			response.Executions = append(response.Executions, newTemplateRunExecutionResponse(execution))
		}
	}
	return response
}

func newTemplateRunExecutionResponse(execution domain.TemplateRunWorkflowExecution) templateRunExecutionResponse {
	steps := make([]templateRunStepResponse, 0, len(execution.Steps))
	for _, step := range execution.Steps {
		steps = append(steps, templateRunStepResponse{
			Step:       string(step.Step),
			Status:     string(step.Status),
			StartedAt:  step.StartedAt,
			FinishedAt: step.FinishedAt,
		})
	}
	return templateRunExecutionResponse{
		Phase:            string(execution.Phase),
		Actor:            string(execution.Actor),
		ActorDisplayName: execution.ActorDisplayName,
		Status:           string(execution.Status),
		StartedAt:        execution.StartedAt,
		FinishedAt:       execution.FinishedAt,
		ErrorSummary:     execution.ErrorSummary,
		Summary:          newPlanSummaryResponse(execution.Summary),
		Steps:            steps,
	}
}

func newPlanSummaryResponse(summary *domain.PlanSummary) *planSummaryResponse {
	if summary == nil {
		return nil
	}
	return &planSummaryResponse{Add: summary.Add, Change: summary.Change, Destroy: summary.Destroy}
}

func newTemplateRunLogResponse(log domain.TemplateRunLog) templateRunLogResponse {
	var size *int64
	if log.State == domain.TemplateRunLogComplete {
		size = &log.SizeBytes
	}
	return templateRunLogResponse{
		TenantID:    string(log.TenantID),
		RunID:       string(log.RunID),
		Name:        log.Phase,
		RunPhase:    string(log.RunPhase),
		Seq:         log.Seq,
		Command:     string(log.Command),
		CommandLine: log.CommandLine,
		State:       string(log.State),
		LostReason:  string(log.LostReason),
		ContentType: log.ContentType,
		SizeBytes:   size,
		UploadedAt:  log.UploadedAt,
	}
}

// newTemplateResponse needs now because the latest sync's status depends on
// how long it has been running.
func newTemplateResponse(template domain.SourceTemplate, now time.Time) templateResponse {
	tags := template.Tags
	if tags == nil {
		tags = []string{}
	}
	return templateResponse{
		ID:                       string(template.ID),
		TenantID:                 string(template.TenantID),
		RepoOwner:                template.RepoOwner,
		RepoName:                 template.RepoName,
		SourceRef:                template.SourceRef,
		RootPath:                 template.RootPath,
		Name:                     template.Name,
		Description:              template.Description,
		Tags:                     tags,
		LatestTemplateRevisionID: string(template.LatestTemplateRevisionID),
		Installed:                template.Installed,
		LatestSync:               newLatestSyncResponse(template.LatestSync, now),
		CreatedAt:                template.CreatedAt,
	}
}

func newLatestSyncResponse(sync *domain.LatestTemplateSync, now time.Time) *latestSyncResponse {
	if sync == nil {
		return nil
	}
	return &latestSyncResponse{
		Status:            string(sync.StatusAt(now)),
		ErrorSummary:      sync.ErrorSummary,
		ResolvedCommitSHA: sync.ResolvedCommitSHA,
		RequestedAt:       sync.RequestedAt,
		CompletedAt:       sync.CompletedAt,
	}
}

func newTemplateRevisionResponse(revision domain.TemplateRevision) templateRevisionResponse {
	// A revision stored without tags has a nil slice, which would go out as
	// null; every client reads tags as a list.
	tags := revision.Tags
	if tags == nil {
		tags = []string{}
	}
	return templateRevisionResponse{
		ID:                string(revision.ID),
		TenantID:          string(revision.TenantID),
		SourceTemplateID:  string(revision.SourceTemplateID),
		RepoOwner:         revision.RepoOwner,
		RepoName:          revision.RepoName,
		SourceRef:         revision.SourceRef,
		ResolvedCommitSHA: revision.ResolvedCommitSHA,
		RootPath:          revision.RootPath,
		Name:              revision.Name,
		Description:       revision.Description,
		Tags:              tags,
		Status:            string(revision.Status),
		CreatedAt:         revision.CreatedAt,
	}
}
