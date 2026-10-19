package postgres

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vishu42/openplan/internal/domain"
)

func samplePlanChanges() *domain.PlanChanges {
	return &domain.PlanChanges{
		Changes: []domain.PlanResourceChange{
			{
				Address: "aws_db_instance.main", Mode: "managed", Type: "aws_db_instance", Name: "main",
				ProviderName: "registry.opentofu.org/hashicorp/aws", Action: domain.PlanChangeReplace,
				CreateBeforeDestroy: true, ActionReason: "replace_because_cannot_update",
				ReplacePaths: json.RawMessage(`[["storage_encrypted"]]`),
				Before:       json.RawMessage(`{"password":"(sensitive)","storage_encrypted":false}`),
				After:        json.RawMessage(`{"password":"(sensitive)","storage_encrypted":true}`),
			},
			{
				Address: "aws_s3_bucket.logs", Mode: "managed", Type: "aws_s3_bucket", Name: "logs",
				Action: domain.PlanChangeDestroy, ActionReason: "delete_because_no_resource_config",
				Before: json.RawMessage(`{"bucket":"logs"}`),
			},
			{
				Address: "aws_instance.web", PreviousAddress: "aws_instance.old", Mode: "managed", Type: "aws_instance",
				Name: "web", Action: domain.PlanChangeImport, ImportingID: "i-123",
			},
		},
		Total: 3,
	}
}

// A finished plan's changes are stored in order, read back a page at a time,
// and the run says the list is complete.
func TestFinishTemplatePlanStoresResourceChanges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := savedPlanStore(t, pool)
	seedStackWithTemplate(t, ctx, store)
	seedPlanRun(t, ctx, pool, "run_123", domain.OperationApply, domain.TemplateRunRunning)

	input := domain.FinishPlanActivityInput{
		TenantID: "tenant_123", RunID: "run_123", StackTemplateID: "stack_template_123", Operation: domain.OperationApply,
		HasChanges: true, Summary: domain.PlanSummary{Add: 1, Destroy: 2},
	}
	_, err := store.FinishTemplatePlan(ctx, input, samplePlanChanges())
	require.NoError(t, err)
	// A retried FinishPlan replaces the rows rather than adding to them.
	_, err = store.FinishTemplatePlan(ctx, input, samplePlanChanges())
	require.NoError(t, err)

	run, err := store.GetTemplateRun(ctx, "tenant_123", "run_123")
	require.NoError(t, err)
	require.Equal(t, domain.PlanChangesComplete, run.PlanChangesState)
	require.NotNil(t, run.PlanChangesTotal)
	require.Equal(t, 3, *run.PlanChangesTotal)

	first, err := store.ListTemplateRunResourceChangesPage(ctx, "tenant_123", "run_123", -1, 2)
	require.NoError(t, err)
	require.Len(t, first, 2)
	want := samplePlanChanges().Changes
	want[0].Position, want[1].Position, want[2].Position = 0, 1, 2
	requireSameChange(t, want[0], first[0])
	requireSameChange(t, want[1], first[1])

	rest, err := store.ListTemplateRunResourceChangesPage(ctx, "tenant_123", "run_123", first[1].Position, 2)
	require.NoError(t, err)
	require.Len(t, rest, 1)
	requireSameChange(t, want[2], rest[0])
}

// A plan run with changes stores them too: it is reviewed in the same view.
func TestFinishTemplatePlanStoresResourceChangesOnAPlanRun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := savedPlanStore(t, pool)
	seedStackWithTemplate(t, ctx, store)
	seedPlanRun(t, ctx, pool, "run_123", domain.OperationPlan, domain.TemplateRunRunning)

	changes := samplePlanChanges()
	changes.Changes = changes.Changes[:1]
	changes.Total = 7
	changes.Truncated = true
	_, err := store.FinishTemplatePlan(ctx, domain.FinishPlanActivityInput{
		TenantID: "tenant_123", RunID: "run_123", StackTemplateID: "stack_template_123", Operation: domain.OperationPlan,
		HasChanges: true, Summary: domain.PlanSummary{Add: 7},
	}, changes)
	require.NoError(t, err)

	run, err := store.GetTemplateRun(ctx, "tenant_123", "run_123")
	require.NoError(t, err)
	require.Equal(t, domain.PlanChangesTruncated, run.PlanChangesState)
	require.Equal(t, 7, *run.PlanChangesTotal)
	listed, err := store.ListTemplateRunResourceChangesPage(ctx, "tenant_123", "run_123", -1, 50)
	require.NoError(t, err)
	require.Len(t, listed, 1)
}

// Changes the control plane couldn't read leave the plan standing, marked
// unavailable, with no rows and no total.
func TestFinishTemplatePlanMarksChangesUnavailable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := savedPlanStore(t, pool)
	seedStackWithTemplate(t, ctx, store)
	seedPlanRun(t, ctx, pool, "run_123", domain.OperationApply, domain.TemplateRunRunning)

	_, err := store.FinishTemplatePlan(ctx, domain.FinishPlanActivityInput{
		TenantID: "tenant_123", RunID: "run_123", StackTemplateID: "stack_template_123", Operation: domain.OperationApply,
		HasChanges: true, Summary: domain.PlanSummary{Add: 1},
	}, nil)
	require.NoError(t, err)

	run, err := store.GetTemplateRun(ctx, "tenant_123", "run_123")
	require.NoError(t, err)
	require.Equal(t, domain.PlanChangesUnavailable, run.PlanChangesState)
	require.Nil(t, run.PlanChangesTotal)
	listed, err := store.ListTemplateRunResourceChangesPage(ctx, "tenant_123", "run_123", -1, 50)
	require.NoError(t, err)
	require.Empty(t, listed)
}

// FinishPlan deletes the upload once it is recorded, so a retry whose first
// attempt committed finds none; the list it recorded stands.
func TestFinishTemplatePlanRetryWithoutTheUploadKeepsTheList(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := savedPlanStore(t, pool)
	seedStackWithTemplate(t, ctx, store)
	seedPlanRun(t, ctx, pool, "run_123", domain.OperationApply, domain.TemplateRunRunning)

	input := domain.FinishPlanActivityInput{
		TenantID: "tenant_123", RunID: "run_123", StackTemplateID: "stack_template_123", Operation: domain.OperationApply,
		HasChanges: true, Summary: domain.PlanSummary{Add: 1, Destroy: 2},
	}
	_, err := store.FinishTemplatePlan(ctx, input, samplePlanChanges())
	require.NoError(t, err)
	_, err = store.FinishTemplatePlan(ctx, input, nil)
	require.NoError(t, err)

	run, err := store.GetTemplateRun(ctx, "tenant_123", "run_123")
	require.NoError(t, err)
	require.Equal(t, domain.PlanChangesComplete, run.PlanChangesState)
	listed, err := store.ListTemplateRunResourceChangesPage(ctx, "tenant_123", "run_123", -1, 50)
	require.NoError(t, err)
	require.Len(t, listed, 3)
}

// A plan with nothing to change has no list to describe.
func TestFinishTemplatePlanWithoutChangesRecordsNoList(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := savedPlanStore(t, pool)
	seedStackWithTemplate(t, ctx, store)
	seedPlanRun(t, ctx, pool, "run_123", domain.OperationApply, domain.TemplateRunRunning)

	_, err := store.FinishTemplatePlan(ctx, domain.FinishPlanActivityInput{
		TenantID: "tenant_123", RunID: "run_123", StackTemplateID: "stack_template_123", Operation: domain.OperationApply,
	}, nil)
	require.NoError(t, err)
	run, err := store.GetTemplateRun(ctx, "tenant_123", "run_123")
	require.NoError(t, err)
	require.Empty(t, run.PlanChangesState)
	require.Nil(t, run.PlanChangesTotal)
}

// requireSameChange compares two changes with their JSON compared as JSON:
// jsonb gives it back normalised.
func requireSameChange(t *testing.T, want, got domain.PlanResourceChange) {
	t.Helper()
	for _, pair := range [][2]json.RawMessage{{want.ReplacePaths, got.ReplacePaths}, {want.Before, got.Before}, {want.After, got.After}} {
		if len(pair[0]) == 0 {
			require.Empty(t, pair[1])
			continue
		}
		require.JSONEq(t, string(pair[0]), string(pair[1]))
	}
	want.ReplacePaths, want.Before, want.After = nil, nil, nil
	got.ReplacePaths, got.Before, got.After = nil, nil, nil
	require.Equal(t, want, got)
}
