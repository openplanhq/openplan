package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vishu42/openplan/internal/app"
	"github.com/vishu42/openplan/internal/domain"
)

// q matches a stack's name or slug as the literal text typed: case-insensitive,
// with LIKE's own metacharacters taken as themselves.
func TestListStacksPageQuery(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewStore(openMigratedTestPool(t, ctx))
	at := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	for i, stack := range []domain.Stack{
		{ID: "stack_payments", Name: "Payments", Slug: "payments"},
		{ID: "stack_search", Name: "Search", Slug: "search-pay"},
		{ID: "stack_percent", Name: "100% uptime", Slug: "uptime"},
		{ID: "stack_other", Name: "Other", Slug: "other"},
	} {
		stack.TenantID = "tenant_123"
		stack.CreatedBy = "user_123"
		stack.CreatedAt = at.Add(-time.Duration(i) * time.Minute)
		require.NoError(t, store.CreateStack(ctx, stack))
	}

	ids := func(query string) []domain.StackID {
		stacks, err := store.ListStacksPage(ctx, "tenant_123", nil, 10, query)
		require.NoError(t, err)
		var got []domain.StackID
		for _, stack := range stacks {
			got = append(got, stack.ID)
		}
		return got
	}
	assert.Equal(t, []domain.StackID{"stack_payments", "stack_search"}, ids("PAY"), "name or slug, any case")
	assert.Equal(t, []domain.StackID{"stack_percent"}, ids("%"), "% is a percent sign")
	assert.Empty(t, ids("_ptime"), "_ is an underscore")
	assert.Len(t, ids(""), 4, "no q is every stack")
}

func TestListTemplateRevisionsPageKeyset(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewStore(openMigratedTestPool(t, ctx))
	seedSourceTemplate(t, ctx, store, "tenant_123", "source_1")
	at := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	for i := range 5 {
		_, err := store.UpsertTemplateRevisionWithVariables(ctx, domain.TemplateRevision{
			ID: domain.TemplateRevisionID(fmt.Sprintf("rev_%d", i)), TenantID: "tenant_123", RepoOwner: "acme",
			RepoName: "infra", SourceRef: "main", ResolvedCommitSHA: fmt.Sprintf("sha_%d", i), RootPath: ".",
			SourceTemplateID: "source_1", Name: "n", Status: domain.TemplateRevisionActive,
			// Two share a created_at, so the id has to break the tie.
			CreatedAt: at.Add(-time.Duration(i/2*2) * time.Minute),
		}, nil)
		require.NoError(t, err)
	}

	var seen []domain.TemplateRevisionID
	var after *app.TemplateRevisionCursor
	for range 5 {
		page, err := store.ListTemplateRevisionsPage(ctx, "tenant_123", after, 2)
		require.NoError(t, err)
		if len(page) == 0 {
			break
		}
		for _, revision := range page {
			seen = append(seen, revision.ID)
		}
		last := page[len(page)-1]
		after = &app.TemplateRevisionCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	assert.Equal(t, []domain.TemplateRevisionID{"rev_1", "rev_0", "rev_3", "rev_2", "rev_4"}, seen)

	_, err := store.ListTemplateRevisionsPage(ctx, "tenant_123", nil, 0)
	require.Error(t, err)
}

func TestListTemplateRunsPageAndGetByNumber(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	for number := 1; number <= 5; number++ {
		seedTemplateRun(t, ctx, pool, domain.TemplateRun{
			ID: domain.TemplateRunID(fmt.Sprintf("run_%d", number)), TenantID: "tenant_123",
			StackTemplateID: "stack_template_123", Operation: domain.OperationPlan, SelectedRef: "main",
			WorkspaceName: "ws", Status: domain.TemplateRunCompleted, TriggerActor: "user_123",
			CreatedAt: time.Date(2026, 10, 17, 9, number, 0, 0, time.UTC),
		})
	}
	seedTemplateRun(t, ctx, pool, domain.TemplateRun{
		ID: "run_elsewhere", TenantID: "tenant_123", StackTemplateID: "stack_template_456",
		Operation: domain.OperationPlan, SelectedRef: "main", WorkspaceName: "ws",
		Status: domain.TemplateRunCompleted, TriggerActor: "user_123", CreatedAt: time.Now(),
	})

	numbers := func(before, limit int) []int {
		runs, err := store.ListTemplateRunsPage(ctx, "tenant_123", "stack_template_123", before, limit)
		require.NoError(t, err)
		var got []int
		for _, run := range runs {
			got = append(got, run.RunNumber)
		}
		return got
	}
	assert.Equal(t, []int{5, 4}, numbers(0, 2))
	assert.Equal(t, []int{3, 2}, numbers(4, 2))
	assert.Equal(t, []int{1}, numbers(2, 2))
	assert.Empty(t, numbers(1, 2))

	run, err := store.GetTemplateRunByNumber(ctx, "tenant_123", "stack_template_123", 3)
	require.NoError(t, err)
	assert.Equal(t, domain.TemplateRunID("run_3"), run.ID)
	_, err = store.GetTemplateRunByNumber(ctx, "tenant_123", "stack_template_123", 9)
	require.ErrorIs(t, err, app.ErrNotFound)
	_, err = store.GetTemplateRunByNumber(ctx, "tenant_456", "stack_template_123", 3)
	require.ErrorIs(t, err, app.ErrNotFound, "another tenant's run is not found")
}

func TestListSourceTemplatesPageKeyset(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewStore(openMigratedTestPool(t, ctx))
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	for i := range 5 {
		_, err := store.CreateSourceTemplate(ctx, domain.SourceTemplate{
			ID: domain.SourceTemplateID(fmt.Sprintf("source_%d", i)), TenantID: "tenant_123",
			RepoOwner: "acme", RepoName: "infra", SourceRef: "main", RootPath: fmt.Sprintf("modules/m%d", i),
			// Two share a created_at, so the id has to break the tie.
			CreatedAt: at.Add(-time.Duration(i/2*2) * time.Minute),
		})
		require.NoError(t, err)
	}
	_, err := store.CreateSourceTemplate(ctx, domain.SourceTemplate{
		ID: "source_elsewhere", TenantID: "tenant_456", RepoOwner: "acme", RepoName: "infra",
		SourceRef: "main", RootPath: "modules/m0", CreatedAt: at,
	})
	require.NoError(t, err)

	var seen []domain.SourceTemplateID
	var after *app.SourceTemplateCursor
	for range 5 {
		page, err := store.ListSourceTemplatesPage(ctx, "tenant_123", after, 2)
		require.NoError(t, err)
		if len(page) == 0 {
			break
		}
		for _, template := range page {
			seen = append(seen, template.ID)
		}
		last := page[len(page)-1]
		after = &app.SourceTemplateCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	assert.Equal(t, []domain.SourceTemplateID{"source_1", "source_0", "source_3", "source_2", "source_4"}, seen)

	_, err = store.ListSourceTemplatesPage(ctx, "tenant_123", nil, 0)
	require.Error(t, err)
}

// A template's revisions page holds only its own revisions, in the tenant's
// own order.
func TestListTemplateRevisionsBySourcePageKeyset(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewStore(openMigratedTestPool(t, ctx))
	seedSourceTemplate(t, ctx, store, "tenant_123", "source_1")
	seedSourceTemplate(t, ctx, store, "tenant_123", "source_2")
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	for i := range 5 {
		_, err := store.UpsertTemplateRevisionWithVariables(ctx, domain.TemplateRevision{
			ID: domain.TemplateRevisionID(fmt.Sprintf("rev_%d", i)), TenantID: "tenant_123", RepoOwner: "acme",
			RepoName: "infra", SourceRef: "main", ResolvedCommitSHA: fmt.Sprintf("sha_%d", i), RootPath: ".",
			SourceTemplateID: "source_1", Name: "n", Status: domain.TemplateRevisionActive,
			CreatedAt: at.Add(-time.Duration(i/2*2) * time.Minute),
		}, nil)
		require.NoError(t, err)
	}
	_, err := store.UpsertTemplateRevisionWithVariables(ctx, domain.TemplateRevision{
		ID: "rev_other_template", TenantID: "tenant_123", RepoOwner: "acme", RepoName: "infra",
		SourceRef: "main", ResolvedCommitSHA: "sha_other", RootPath: "modules/db",
		SourceTemplateID: "source_2", Name: "db", Status: domain.TemplateRevisionActive, CreatedAt: at,
	}, nil)
	require.NoError(t, err)

	var seen []domain.TemplateRevisionID
	var after *app.TemplateRevisionCursor
	for range 5 {
		page, err := store.ListTemplateRevisionsBySourcePage(ctx, "tenant_123", "source_1", after, 2)
		require.NoError(t, err)
		if len(page) == 0 {
			break
		}
		for _, revision := range page {
			seen = append(seen, revision.ID)
		}
		last := page[len(page)-1]
		after = &app.TemplateRevisionCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	assert.Equal(t, []domain.TemplateRevisionID{"rev_1", "rev_0", "rev_3", "rev_2", "rev_4"}, seen)

	page, err := store.ListTemplateRevisionsBySourcePage(ctx, "tenant_456", "source_1", nil, 10)
	require.NoError(t, err)
	assert.Empty(t, page, "another tenant sees none of the template's revisions")
	page, err = store.ListTemplateRevisionsBySourcePage(ctx, "tenant_123", "source_unknown", nil, 10)
	require.NoError(t, err)
	assert.Empty(t, page)
	_, err = store.ListTemplateRevisionsBySourcePage(ctx, "tenant_123", "source_1", nil, 0)
	require.Error(t, err)
}
