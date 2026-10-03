package postgres

import (
	"context"
	"encoding/json"
	"testing"

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
