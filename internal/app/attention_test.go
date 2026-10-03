package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vishu42/openplan/internal/authentication"
	"github.com/vishu42/openplan/internal/authorization"
	"github.com/vishu42/openplan/internal/domain"
)

// fakeStackOverviewRepository holds a tenant's templates and runs in memory.
type fakeStackOverviewRepository struct {
	templates      []domain.StackTemplate
	runsByStatus   map[domain.TemplateRunStatus][]domain.TemplateRun
	templatesCalls int
	templatesErr   error
}

func (repository *fakeStackOverviewRepository) ListTenantStackTemplates(_ context.Context, _ domain.TenantID) ([]domain.StackTemplate, error) {
	repository.templatesCalls++
	if repository.templatesErr != nil {
		return nil, repository.templatesErr
	}
	return repository.templates, nil
}

func (repository *fakeStackOverviewRepository) ListTemplateRunsByStatus(_ context.Context, _ domain.TenantID, status domain.TemplateRunStatus) ([]domain.TemplateRun, error) {
	return repository.runsByStatus[status], nil
}

// runsPerTemplateRepository answers ListTemplateRuns per stack template, which
// the recording fake cannot: it returns one list whatever template is asked for.
type runsPerTemplateRepository struct {
	*recordingTemplateRunRepository
	runs map[domain.StackTemplateID][]domain.TemplateRun
}

func (repository runsPerTemplateRepository) ListTemplateRuns(_ context.Context, _ domain.TenantID, stackTemplateID domain.StackTemplateID) ([]domain.TemplateRun, error) {
	return repository.runs[stackTemplateID], nil
}

type attentionFixture struct {
	service  *Service
	overview *fakeStackOverviewRepository
	ctx      context.Context
}

// newAttentionFixture builds a tenant of three stacks of which the viewer can
// see a and b. Stack a has a plan waiting; stack b has a newer plan waiting and
// a template whose destroy failed; stack c, which the viewer cannot see, has a
// plan waiting too.
func newAttentionFixture(t *testing.T) attentionFixture {
	t.Helper()

	planA := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	planB := time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)
	destroyed := time.Date(2026, 9, 29, 8, 0, 0, 0, time.UTC)

	auth := newTestAuthorization(t)
	grantStack(t, auth, "user_123", "stack_a", authorization.RelationViewer)
	grantStack(t, auth, "user_123", "stack_b", authorization.RelationViewer)

	stacks := &pagedStackRepository{stacks: []domain.Stack{
		{ID: "stack_a", Name: "prod", Slug: "prod", CreatedAt: time.Unix(300, 0)},
		{ID: "stack_b", Name: "Edge CDN", Slug: "edge-cdn", CreatedAt: time.Unix(200, 0)},
		{ID: "stack_c", Name: "secret", Slug: "secret", CreatedAt: time.Unix(100, 0)},
	}}
	overview := &fakeStackOverviewRepository{
		templates: []domain.StackTemplate{
			{ID: "tpl_a1", StackID: "stack_a", DesiredTemplateRevisionID: "rev_eks", Lifecycle: domain.StackTemplateActive, PendingPlanRunID: "run_a1", PendingPlanAt: planA},
			{ID: "tpl_a2", StackID: "stack_a", DesiredTemplateRevisionID: "rev_root", Lifecycle: domain.StackTemplateActive},
			{ID: "tpl_b1", StackID: "stack_b", DesiredTemplateRevisionID: "rev_cdn", Lifecycle: domain.StackTemplateFailed},
			{ID: "tpl_b2", StackID: "stack_b", DesiredTemplateRevisionID: "rev_eks", Lifecycle: domain.StackTemplateActive, PendingPlanRunID: "run_b2", PendingPlanAt: planB},
			{ID: "tpl_c1", StackID: "stack_c", DesiredTemplateRevisionID: "rev_eks", Lifecycle: domain.StackTemplateActive, PendingPlanRunID: "run_c1", PendingPlanAt: planB},
		},
		runsByStatus: map[domain.TemplateRunStatus][]domain.TemplateRun{
			domain.TemplateRunWaitingApproval: {
				{ID: "run_c1", StackTemplateID: "tpl_c1", Status: domain.TemplateRunWaitingApproval, TriggerActor: "user_dana"},
				{ID: "run_b2", StackTemplateID: "tpl_b2", Status: domain.TemplateRunWaitingApproval, TriggerActor: "user_dana", RunNumber: 4},
				{ID: "run_a1", StackTemplateID: "tpl_a1", Status: domain.TemplateRunWaitingApproval, TriggerActor: "user_priya", RunNumber: 7},
			},
		},
	}
	runs := runsPerTemplateRepository{
		recordingTemplateRunRepository: &recordingTemplateRunRepository{},
		runs: map[domain.StackTemplateID][]domain.TemplateRun{
			// Most recent first, as the repository returns them. A plan after
			// the failed destroy must not be taken for it.
			"tpl_b1": {
				{ID: "run_b1_plan", StackTemplateID: "tpl_b1", Operation: domain.OperationPlan, Status: domain.TemplateRunCompleted},
				{ID: "run_b1_destroy", StackTemplateID: "tpl_b1", Operation: domain.OperationDestroy, Status: domain.TemplateRunFailed, RunNumber: 3, TriggerActor: "user_priya", CompletedAt: destroyed},
			},
		},
	}
	revisions := &recordingTemplateRepository{templates: []domain.TemplateRevision{
		{ID: "rev_eks", RepoName: "infra-modules", RootPath: "modules/eks-cluster"},
		{ID: "rev_cdn", RepoName: "infra-modules", RootPath: "modules/cloudfront"},
		{ID: "rev_root", RepoName: "network", RootPath: "."},
	}}
	users := &fakeUserRepository{users: []UserProfile{
		{Sub: "user_priya", DisplayName: "Priya Shah"},
		{Sub: "user_dana", DisplayName: "Dana Lee"},
	}}

	service := NewService(Service{
		Authorization:     auth,
		Stacks:            stacks,
		StackOverview:     overview,
		TemplateRuns:      runs,
		TemplateRevisions: revisions,
		Users:             users,
	})
	ctx := authentication.ContextWithPrincipal(context.Background(), authentication.Principal{Subject: "user_123"})
	return attentionFixture{service: service, overview: overview, ctx: ctx}
}

func TestListAttentionListsWaitingPlansThenFailedDestroysOnVisibleStacks(t *testing.T) {
	t.Parallel()

	fixture := newAttentionFixture(t)

	items, err := fixture.service.ListAttention(fixture.ctx, ListAttentionCommand{TenantID: "tenant_123"})
	if err != nil {
		t.Fatalf("ListAttention() error = %v", err)
	}

	type row struct {
		kind     AttentionKind
		stack    domain.StackID
		template domain.StackTemplateID
		name     string
		run      domain.TemplateRunID
	}
	var got []row
	for _, item := range items {
		var runID domain.TemplateRunID
		if item.Run != nil {
			runID = item.Run.ID
		}
		got = append(got, row{item.Kind, item.Stack.ID, item.StackTemplate.ID, item.StackTemplate.DisplayName, runID})
	}
	// Waiting plans newest first, then the failure; nothing from stack c.
	want := []row{
		{AttentionWaitingApproval, "stack_b", "tpl_b2", "eks-cluster", "run_b2"},
		{AttentionWaitingApproval, "stack_a", "tpl_a1", "eks-cluster", "run_a1"},
		{AttentionDestroyFailed, "stack_b", "tpl_b1", "cloudfront", "run_b1_destroy"},
	}
	if len(got) != len(want) {
		t.Fatalf("items = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("items[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestListAttentionDatesEachItemAndNamesWhoStartedIt(t *testing.T) {
	t.Parallel()

	fixture := newAttentionFixture(t)

	items, err := fixture.service.ListAttention(fixture.ctx, ListAttentionCommand{TenantID: "tenant_123"})
	if err != nil {
		t.Fatalf("ListAttention() error = %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("len(items) = %d, want 3", len(items))
	}

	// A waiting plan dates from when it finished, which is when it began
	// waiting; a failed destroy from when it stopped.
	if want := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC); !items[1].At.Equal(want) {
		t.Errorf("waiting item At = %v, want %v", items[1].At, want)
	}
	if want := time.Date(2026, 9, 29, 8, 0, 0, 0, time.UTC); !items[2].At.Equal(want) {
		t.Errorf("failed item At = %v, want %v", items[2].At, want)
	}
	if got := items[1].Run.TriggerActorDisplayName; got != "Priya Shah" {
		t.Errorf("waiting run trigger actor = %q, want Priya Shah", got)
	}
	if got := items[0].Stack.Name; got != "Edge CDN" {
		t.Errorf("first item stack name = %q, want Edge CDN", got)
	}
}

func TestListAttentionIsEmptyWithoutVisibleStacks(t *testing.T) {
	t.Parallel()

	overview := &fakeStackOverviewRepository{}
	service := NewService(Service{
		Authorization: newTestAuthorization(t),
		Stacks:        &pagedStackRepository{stacks: []domain.Stack{{ID: "stack_a", CreatedAt: time.Unix(100, 0)}}},
		StackOverview: overview,
	})
	ctx := authentication.ContextWithPrincipal(context.Background(), authentication.Principal{Subject: "user_123"})

	items, err := service.ListAttention(ctx, ListAttentionCommand{TenantID: "tenant_123"})
	if err != nil {
		t.Fatalf("ListAttention() error = %v", err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("items = %#v, want an empty, non-nil list", items)
	}
	if overview.templatesCalls != 0 {
		t.Errorf("templates read %d times, want 0 when no stack is visible", overview.templatesCalls)
	}
}

func TestListAttentionRequiresTenant(t *testing.T) {
	t.Parallel()

	fixture := newAttentionFixture(t)

	_, err := fixture.service.ListAttention(fixture.ctx, ListAttentionCommand{})
	if !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("error = %v, want ErrInvalidCommand", err)
	}
}

func TestListStackSummariesCountsTemplatesOnVisibleStacks(t *testing.T) {
	t.Parallel()

	fixture := newAttentionFixture(t)

	summaries, err := fixture.service.ListStackSummaries(fixture.ctx, ListStacksCommand{TenantID: "tenant_123"})
	if err != nil {
		t.Fatalf("ListStackSummaries() error = %v", err)
	}

	got := map[domain.StackID]int{}
	for _, summary := range summaries {
		got[summary.Stack.ID] = summary.TemplateCount
	}
	want := map[domain.StackID]int{"stack_a": 2, "stack_b": 2}
	if len(got) != len(want) {
		t.Fatalf("summaries = %v, want %v", got, want)
	}
	for id, count := range want {
		if got[id] != count {
			t.Errorf("template count for %s = %d, want %d", id, got[id], count)
		}
	}
}

func TestListStackSummariesReportsTemplateReadFailure(t *testing.T) {
	t.Parallel()

	fixture := newAttentionFixture(t)
	fixture.overview.templatesErr = errors.New("database unavailable")

	if _, err := fixture.service.ListStackSummaries(fixture.ctx, ListStacksCommand{TenantID: "tenant_123"}); err == nil {
		t.Fatal("ListStackSummaries() error = nil, want the template read failure")
	}
}
