package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vishu42/openplan/internal/authentication"
	"github.com/vishu42/openplan/internal/authorization"
	"github.com/vishu42/openplan/internal/domain"
)

// numberedStacks are count stacks, newest first, named stack_000 and up.
func numberedStacks(count int) []domain.Stack {
	stacks := make([]domain.Stack, count)
	for i := range stacks {
		stacks[i] = domain.Stack{
			ID:        domain.StackID(fmt.Sprintf("stack_%03d", i)),
			Name:      fmt.Sprintf("Stack %03d", i),
			Slug:      fmt.Sprintf("stack-%03d", i),
			CreatedAt: time.Unix(int64(10_000-i), 0),
		}
	}
	return stacks
}

func stackIDs(stacks []domain.Stack) []domain.StackID {
	ids := make([]domain.StackID, 0, len(stacks))
	for _, stack := range stacks {
		ids = append(ids, stack.ID)
	}
	return ids
}

// A non-admin who can see 1 stack in 40 still gets a full page, and the walk
// stops once the page is full: it does not drain the tenant on every request.
func TestListStacksPageFillsAcrossCandidateBatches(t *testing.T) {
	t.Parallel()

	repository := &pagedStackRepository{stacks: numberedStacks(200)}
	auth := newTestAuthorization(t)
	for i := 0; i < 200; i += 40 {
		grantStack(t, auth, "user_123", fmt.Sprintf("stack_%03d", i), authorization.RelationViewer)
	}
	service := NewService(Service{Authorization: auth, Stacks: repository})
	ctx := authentication.ContextWithPrincipal(context.Background(), authentication.Principal{Subject: "user_123"})

	first, err := service.ListStacks(ctx, ListStacksCommand{TenantID: "tenant_123", Page: PageRequest{Limit: 3}})
	require.NoError(t, err)
	assert.Equal(t, []domain.StackID{"stack_000", "stack_040", "stack_080"}, stackIDs(first.Items))
	require.NotEmpty(t, first.NextCursor)
	// stack_120 proves a next page exists; it is in the third batch of 50,
	// so the fourth is never read.
	assert.Equal(t, 3, repository.pageCalls)

	last, err := service.ListStacks(ctx, ListStacksCommand{TenantID: "tenant_123", Page: PageRequest{Limit: 3, Cursor: first.NextCursor}})
	require.NoError(t, err)
	assert.Equal(t, []domain.StackID{"stack_120", "stack_160"}, stackIDs(last.Items))
	assert.Empty(t, last.NextCursor)
}

// A page that ends exactly at the last visible stack has no cursor: a client
// is never sent to a page with nothing on it.
func TestListStacksPageNoCursorWhenExactlyFull(t *testing.T) {
	t.Parallel()

	auth := newTestAuthorization(t)
	for _, id := range []string{"stack_000", "stack_001", "stack_002"} {
		grantStack(t, auth, "user_123", id, authorization.RelationViewer)
	}
	service := NewService(Service{Authorization: auth, Stacks: &pagedStackRepository{stacks: numberedStacks(10)}})
	ctx := authentication.ContextWithPrincipal(context.Background(), authentication.Principal{Subject: "user_123"})

	page, err := service.ListStacks(ctx, ListStacksCommand{TenantID: "tenant_123", Page: PageRequest{Limit: 3}})
	require.NoError(t, err)
	assert.Len(t, page.Items, 3)
	assert.Empty(t, page.NextCursor)
}

// An administrator sees every stack without a check per stack, and still
// gets them a page at a time: first, next and last.
func TestListStacksPageAdministrator(t *testing.T) {
	t.Parallel()

	repository := &pagedStackRepository{stacks: numberedStacks(5)}
	auth := newTestAuthorization(t)
	grantPlatform(t, auth, "user_123", authorization.RelationAdmin)
	service := NewService(Service{Authorization: auth, Stacks: repository})
	ctx := authentication.ContextWithPrincipal(context.Background(), authentication.Principal{Subject: "user_123"})

	var seen []domain.StackID
	cursor := ""
	for pages := 1; ; pages++ {
		page, err := service.ListStacks(ctx, ListStacksCommand{TenantID: "tenant_123", Page: PageRequest{Limit: 2, Cursor: cursor}})
		require.NoError(t, err)
		seen = append(seen, stackIDs(page.Items)...)
		if page.NextCursor == "" {
			assert.Equal(t, 3, pages)
			break
		}
		cursor = page.NextCursor
	}
	assert.Equal(t, stackIDs(numberedStacks(5)), seen)
}

// q narrows the walk itself, so a match on a later page is found without the
// client loading every page first.
func TestListStacksQueryMatchesNameOrSlug(t *testing.T) {
	t.Parallel()

	stacks := numberedStacks(120)
	stacks[110].Name = "Payments"
	stacks[115].Slug = "payments-eu"
	repository := &pagedStackRepository{stacks: stacks}
	auth := newTestAuthorization(t)
	grantPlatform(t, auth, "user_123", authorization.RelationAdmin)
	service := NewService(Service{Authorization: auth, Stacks: repository})
	ctx := authentication.ContextWithPrincipal(context.Background(), authentication.Principal{Subject: "user_123"})

	page, err := service.ListStacks(ctx, ListStacksCommand{TenantID: "tenant_123", Page: FirstPage(), Query: "  PAY "})
	require.NoError(t, err)
	assert.Equal(t, []domain.StackID{"stack_110", "stack_115"}, stackIDs(page.Items))
	assert.Equal(t, "PAY", repository.gotQuery, "q is trimmed before it reaches the store")
}

func TestListStacksRejectsBadPaging(t *testing.T) {
	t.Parallel()

	service := NewService(Service{Authorization: newTestAuthorization(t), Stacks: &pagedStackRepository{}})
	ctx := authentication.ContextWithPrincipal(context.Background(), authentication.Principal{Subject: "user_123"})

	for name, test := range map[string]struct {
		command ListStacksCommand
		want    error
		issue   Issue
		param   string
	}{
		"limit 0":           {ListStacksCommand{TenantID: "t", Page: PageRequest{Limit: 0}}, ErrInvalidCommand, IssueOutOfRange, "limit"},
		"limit 101":         {ListStacksCommand{TenantID: "t", Page: PageRequest{Limit: 101}}, ErrInvalidCommand, IssueOutOfRange, "limit"},
		"q too long":        {ListStacksCommand{TenantID: "t", Page: FirstPage(), Query: strings.Repeat("a", 201)}, ErrInvalidCommand, IssueTooLong, "q"},
		"garbage cursor":    {ListStacksCommand{TenantID: "t", Page: PageRequest{Limit: 5, Cursor: "%%%"}}, ErrInvalidQuery, IssueInvalidFormat, "cursor"},
		"a runs cursor":     {ListStacksCommand{TenantID: "t", Page: PageRequest{Limit: 5, Cursor: EncodeCursor(templateRunsList, templateRunCursor{RunNumber: 3})}}, ErrInvalidQuery, IssueInvalidFormat, "cursor"},
		"a revision cursor": {ListStacksCommand{TenantID: "t", Page: PageRequest{Limit: 5, Cursor: EncodeCursor(templateRevisionsList, TemplateRevisionCursor{CreatedAt: time.Now(), ID: "rev"})}}, ErrInvalidQuery, IssueInvalidFormat, "cursor"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := service.ListStacks(ctx, test.command)
			require.ErrorIs(t, err, test.want)
			fieldErrs := FieldErrors(err)
			require.Len(t, fieldErrs, 1)
			assert.Equal(t, test.param, fieldErrs[0].Parameter)
			assert.Equal(t, test.issue, fieldErrs[0].Issue)
		})
	}
}

func runsNewestFirst(count int) []domain.TemplateRun {
	runs := make([]domain.TemplateRun, 0, count)
	for number := count; number >= 1; number-- {
		runs = append(runs, domain.TemplateRun{
			ID: domain.TemplateRunID(fmt.Sprintf("run_%d", number)), TenantID: "tenant_123",
			StackTemplateID: "stack_template_123", RunNumber: number,
		})
	}
	return runs
}

func runNumbers(runs []domain.TemplateRun) []int {
	numbers := make([]int, 0, len(runs))
	for _, run := range runs {
		numbers = append(numbers, run.RunNumber)
	}
	return numbers
}

func runsService(t *testing.T, runs *recordingTemplateRunRepository) *Service {
	t.Helper()
	return NewService(Service{
		Authorization:  testPlatformAuthorizer(t),
		TemplateRuns:   runs,
		Users:          &fakeUserRepository{},
		StackTemplates: &recordingStackTemplateRepository{stackTemplate: domain.StackTemplate{ID: "stack_template_123", TenantID: "tenant_123", StackID: "stack_123"}},
	})
}

// A run started between two page reads lands at the head of the list, so the
// next page goes on where the last stopped: no run twice, none skipped.
func TestListTemplateRunsPageStableUnderNewRun(t *testing.T) {
	t.Parallel()

	runs := &recordingTemplateRunRepository{list: runsNewestFirst(5)}
	service := runsService(t, runs)
	command := ListTemplateRunsCommand{TenantID: "tenant_123", StackTemplateID: "stack_template_123", Page: PageRequest{Limit: 2}}

	first, err := service.ListTemplateRuns(authenticatedContext(), command)
	require.NoError(t, err)
	assert.Equal(t, []int{5, 4}, runNumbers(first.Items))

	runs.list = runsNewestFirst(6)
	command.Page.Cursor = first.NextCursor
	second, err := service.ListTemplateRuns(authenticatedContext(), command)
	require.NoError(t, err)
	assert.Equal(t, []int{3, 2}, runNumbers(second.Items))

	command.Page.Cursor = second.NextCursor
	last, err := service.ListTemplateRuns(authenticatedContext(), command)
	require.NoError(t, err)
	assert.Equal(t, []int{1}, runNumbers(last.Items))
	assert.Empty(t, last.NextCursor)
}

func TestGetTemplateRunByNumber(t *testing.T) {
	t.Parallel()

	runs := &recordingTemplateRunRepository{
		list:       runsNewestFirst(3),
		executions: []domain.TemplateRunWorkflowExecution{{Phase: domain.RunPhasePlan}},
	}
	service := runsService(t, runs)

	run, err := service.GetTemplateRunByNumber(authenticatedContext(), GetTemplateRunByNumberCommand{
		TenantID: "tenant_123", StackTemplateID: "stack_template_123", RunNumber: 2,
	})
	require.NoError(t, err)
	assert.Equal(t, domain.TemplateRunID("run_2"), run.ID)
	assert.Len(t, run.Executions, 1, "a read of one run carries its executions, as GetTemplateRun's does")

	for _, number := range []int{0, -1, 4} {
		_, err := service.GetTemplateRunByNumber(authenticatedContext(), GetTemplateRunByNumberCommand{
			TenantID: "tenant_123", StackTemplateID: "stack_template_123", RunNumber: number,
		})
		require.ErrorIs(t, err, ErrNotFound, "run #%d", number)
	}
}

// Reading a run by number checks the stack first, so a run on a stack the
// caller can't see is not found, the same answer as no run at all.
func TestGetTemplateRunByNumberRequiresViewingTheStack(t *testing.T) {
	t.Parallel()

	runs := &recordingTemplateRunRepository{list: runsNewestFirst(1)}
	service := NewService(Service{
		Authorization:  newTestAuthorization(t),
		TemplateRuns:   runs,
		Users:          &fakeUserRepository{},
		StackTemplates: &recordingStackTemplateRepository{stackTemplate: domain.StackTemplate{ID: "stack_template_123", TenantID: "tenant_123", StackID: "stack_123"}},
	})
	ctx := authentication.ContextWithPrincipal(context.Background(), authentication.Principal{Subject: "user_123"})

	_, err := service.GetTemplateRunByNumber(ctx, GetTemplateRunByNumberCommand{TenantID: "tenant_123", StackTemplateID: "stack_template_123", RunNumber: 1})
	require.ErrorIs(t, err, ErrNotFound)
	assert.Empty(t, runs.gotListStackTemplateID, "the run is not read before the stack is authorized")
}

func TestListTemplateRevisionsPaging(t *testing.T) {
	t.Parallel()

	revisions := make([]domain.TemplateRevision, 5)
	for i := range revisions {
		revisions[i] = domain.TemplateRevision{ID: domain.TemplateRevisionID(fmt.Sprintf("rev_%d", i)), CreatedAt: time.Unix(int64(100-i), 0)}
	}
	service := NewService(Service{TemplateRevisions: &recordingTemplateRepository{templates: revisions}, Authorization: testPlatformAuthorizer(t)})

	var seen []domain.TemplateRevisionID
	cursor := ""
	for {
		page, err := service.ListTemplateRevisions(authenticatedContext(), ListTemplateRevisionsCommand{TenantID: "tenant_123", Page: PageRequest{Limit: 2, Cursor: cursor}})
		require.NoError(t, err)
		for _, revision := range page.Items {
			seen = append(seen, revision.ID)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	assert.Equal(t, []domain.TemplateRevisionID{"rev_0", "rev_1", "rev_2", "rev_3", "rev_4"}, seen)

	_, err := service.ListTemplateRevisions(authenticatedContext(), ListTemplateRevisionsCommand{
		TenantID: "tenant_123", Page: PageRequest{Limit: 2, Cursor: EncodeCursor(stacksList, StackPageCursor{CreatedAt: time.Now(), ID: "s"})},
	})
	require.ErrorIs(t, err, ErrInvalidQuery)
}

func TestSearchUsersPaging(t *testing.T) {
	t.Parallel()

	users := &fakeUserRepository{users: []UserProfile{
		{Sub: "a", DisplayName: "Alice"}, {Sub: "b", DisplayName: "Alicia"}, {Sub: "c", DisplayName: "Alina"},
	}}
	service := NewService(Service{Users: users, Authorization: testPlatformAuthorizer(t)})

	first, err := service.SearchUsers(contextWithPlatformAdmin(), SearchUsersCommand{TenantID: "tenant_1", Query: "ali", Page: PageRequest{Limit: 2}})
	require.NoError(t, err)
	assert.Len(t, first.Items, 2)
	require.NotEmpty(t, first.NextCursor)
	assert.Equal(t, 3, users.gotLimit, "one more than the page, to prove a next one")

	// The cursor is only a position, so it still reads under a changed q.
	last, err := service.SearchUsers(contextWithPlatformAdmin(), SearchUsersCommand{TenantID: "tenant_1", Query: "alin", Page: PageRequest{Limit: 2, Cursor: first.NextCursor}})
	require.NoError(t, err)
	assert.Equal(t, &UserCursor{DisplayName: "Alicia", Sub: "b"}, users.gotAfter)
	assert.Equal(t, []UserProfile{{Sub: "c", DisplayName: "Alina"}}, last.Items)
	assert.Empty(t, last.NextCursor)
}
