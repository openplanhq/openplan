package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/vishu42/openplan/internal/app"
	"github.com/vishu42/openplan/internal/domain"
)

var _ app.StackOverviewRepository = (*Store)(nil)

func TestListTenantStackTemplatesSkipsDestroyedAndOtherTenants(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	seedStackWithTemplate(t, ctx, store)

	destroyed := domain.StackTemplate{
		ID:                        "stack_template_gone",
		TenantID:                  "tenant_123",
		StackID:                   "stack_123",
		SourceTemplateID:          "source_template_vpc",
		DesiredTemplateRevisionID: "template_rev_2",
		WorkspaceName:             "mtp_acme_prod_vpc_gone",
		InstalledConfigJSON:       json.RawMessage(`{}`),
		CreatedBy:                 "installer_123",
		Lifecycle:                 domain.StackTemplateActive,
	}
	failed := destroyed
	failed.ID = "stack_template_failed"
	failed.WorkspaceName = "mtp_acme_prod_vpc_failed"
	for _, stackTemplate := range []domain.StackTemplate{destroyed, failed} {
		if err := store.CreateStackTemplate(ctx, stackTemplate); err != nil {
			t.Fatalf("CreateStackTemplate(%s) returned error: %v", stackTemplate.ID, err)
		}
	}
	if _, err := pool.Exec(ctx, `update stack_templates set lifecycle = $1 where id = $2`, domain.StackTemplateDestroyed, destroyed.ID); err != nil {
		t.Fatalf("mark destroyed: %v", err)
	}
	if _, err := pool.Exec(ctx, `update stack_templates set lifecycle = $1 where id = $2`, domain.StackTemplateFailed, failed.ID); err != nil {
		t.Fatalf("mark failed: %v", err)
	}

	if err := store.CreateStack(ctx, domain.Stack{ID: "stack_other", TenantID: "tenant_456", Name: "Other", Slug: "other", CreatedBy: "user_456"}); err != nil {
		t.Fatalf("CreateStack(other tenant) returned error: %v", err)
	}
	other := destroyed
	other.ID = "stack_template_other"
	other.TenantID = "tenant_456"
	other.StackID = "stack_other"
	other.WorkspaceName = "mtp_other_vpc"
	if err := store.CreateStackTemplate(ctx, other); err != nil {
		t.Fatalf("CreateStackTemplate(other tenant) returned error: %v", err)
	}

	templates, err := store.ListTenantStackTemplates(ctx, "tenant_123")
	if err != nil {
		t.Fatalf("ListTenantStackTemplates returned error: %v", err)
	}

	got := map[domain.StackTemplateID]domain.StackTemplateLifecycle{}
	for _, stackTemplate := range templates {
		got[stackTemplate.ID] = stackTemplate.Lifecycle
	}
	want := map[domain.StackTemplateID]domain.StackTemplateLifecycle{
		"stack_template_123":    domain.StackTemplateActive,
		"stack_template_failed": domain.StackTemplateFailed,
	}
	if len(got) != len(want) {
		t.Fatalf("templates = %v, want %v", got, want)
	}
	for id, lifecycle := range want {
		if got[id] != lifecycle {
			t.Errorf("template %s lifecycle = %q, want %q", id, got[id], lifecycle)
		}
	}
}

func TestListTemplateRunsByStatusReturnsOnlyThatStatusInTheTenant(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	seedStackWithTemplate(t, ctx, store)
	seedPlanRun(t, ctx, pool, "run_done", domain.OperationPlan, domain.TemplateRunCompleted)
	seedPlanRun(t, ctx, pool, "run_waiting", domain.OperationPlan, domain.TemplateRunWaitingApproval)

	runs, err := store.ListTemplateRunsByStatus(ctx, "tenant_123", domain.TemplateRunWaitingApproval)
	if err != nil {
		t.Fatalf("ListTemplateRunsByStatus returned error: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != "run_waiting" {
		t.Fatalf("runs = %+v, want only run_waiting", runs)
	}
	if runs[0].RunNumber != 2 {
		t.Errorf("run number = %d, want 2", runs[0].RunNumber)
	}

	other, err := store.ListTemplateRunsByStatus(ctx, "tenant_456", domain.TemplateRunWaitingApproval)
	if err != nil {
		t.Fatalf("ListTemplateRunsByStatus(other tenant) returned error: %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("other tenant runs = %+v, want none", other)
	}
}

func TestListLatestTemplateRunsReturnsEachTemplatesNewestRun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	seedStackWithTemplate(t, ctx, store)

	second := domain.StackTemplate{
		ID:                        "stack_template_second",
		TenantID:                  "tenant_123",
		StackID:                   "stack_123",
		SourceTemplateID:          "source_template_vpc",
		DesiredTemplateRevisionID: "template_rev_2",
		WorkspaceName:             "mtp_acme_prod_vpc_second",
		InstalledConfigJSON:       json.RawMessage(`{}`),
		CreatedBy:                 "installer_123",
		Lifecycle:                 domain.StackTemplateActive,
	}
	if err := store.CreateStackTemplate(ctx, second); err != nil {
		t.Fatalf("CreateStackTemplate(second) returned error: %v", err)
	}

	// The failed plan came first, so the completed one is the template's
	// latest: only the newest run of each template comes back.
	older := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	run := func(id domain.TemplateRunID, stackTemplate domain.StackTemplateID, operation domain.OperationType, status domain.TemplateRunStatus, at time.Time, number int) domain.TemplateRun {
		return domain.TemplateRun{
			ID:                 id,
			TenantID:           "tenant_123",
			StackTemplateID:    stackTemplate,
			TemplateRevisionID: "template_rev_2",
			SourceTemplateID:   "source_template_vpc",
			Operation:          operation,
			SourceRef:          "main",
			WorkspaceName:      "mtp_acme_prod_vpc_a13f9c",
			ConfigJSON:         json.RawMessage(`{}`),
			Status:             status,
			TriggerActor:       "user_123",
			CreatedAt:          at,
			RunNumber:          number,
		}
	}
	seedTemplateRun(t, ctx, pool, run("run_old", "stack_template_123", domain.OperationPlan, domain.TemplateRunFailed, older, 1))
	seedTemplateRun(t, ctx, pool, run("run_new", "stack_template_123", domain.OperationPlan, domain.TemplateRunCompleted, newer, 2))
	seedTemplateRun(t, ctx, pool, run("run_other", "stack_template_second", domain.OperationApply, domain.TemplateRunFailed, older, 1))

	runs, err := store.ListLatestTemplateRuns(ctx, "tenant_123")
	if err != nil {
		t.Fatalf("ListLatestTemplateRuns returned error: %v", err)
	}
	got := map[domain.StackTemplateID]domain.TemplateRunID{}
	for _, run := range runs {
		got[run.StackTemplateID] = run.ID
	}
	want := map[domain.StackTemplateID]domain.TemplateRunID{
		"stack_template_123":    "run_new",
		"stack_template_second": "run_other",
	}
	if len(runs) != len(want) {
		t.Fatalf("runs = %v, want %v", got, want)
	}
	for stackTemplate, id := range want {
		if got[stackTemplate] != id {
			t.Errorf("template %s latest run = %q, want %q", stackTemplate, got[stackTemplate], id)
		}
	}

	other, err := store.ListLatestTemplateRuns(ctx, "tenant_456")
	if err != nil {
		t.Fatalf("ListLatestTemplateRuns(other tenant) returned error: %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("other tenant runs = %+v, want none", other)
	}
}
