package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vishu42/openplan/internal/app"
	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/queue"
)

// listPage is one page of a list response, as a client reads it.
type listPage[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

func getListPage[T any](t *testing.T, server http.Handler, path string) listPage[T] {
	t.Helper()
	response := httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedRequest(http.MethodGet, path, nil))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	page := listPage[T]{Items: decodeItems[T](t, response)}
	var envelope struct {
		NextCursor *string `json:"next_cursor"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	page.NextCursor = envelope.NextCursor
	return page
}

// walkPages follows next_cursor from path until the last page, returning each
// page's items so a test can check first, next and last.
func walkPages[T any](t *testing.T, server http.Handler, path string) [][]T {
	t.Helper()
	var pages [][]T
	cursor := ""
	for range 20 {
		next := path
		if cursor != "" {
			next += "&cursor=" + url.QueryEscape(cursor)
		}
		page := getListPage[T](t, server, next)
		pages = append(pages, page.Items)
		if page.NextCursor == nil {
			return pages
		}
		require.NotEmpty(t, *page.NextCursor)
		cursor = *page.NextCursor
	}
	t.Fatal("list never reached its last page")
	return nil
}

func assertQueryProblem(t *testing.T, server http.Handler, path string, status int, problem, parameter string, issue app.Issue) {
	t.Helper()
	response := httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedRequest(http.MethodGet, path, nil))
	require.Equal(t, status, response.Code, response.Body.String())
	body := decodeProblem(t, response)
	assert.Equal(t, problemTypeURI(problem), body.Type)
	require.Len(t, body.Errors, 1, response.Body.String())
	assert.Equal(t, parameter, body.Errors[0].Parameter)
	assert.Equal(t, issue, body.Errors[0].Issue)
}

func TestListStacksPaging(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	for i := range 5 {
		deps.stacks.list = append(deps.stacks.list, domain.Stack{
			ID: domain.StackID(fmt.Sprintf("stack_%d", i)), TenantID: "tenant_123", Name: fmt.Sprintf("Stack %d", i),
			Slug: fmt.Sprintf("stack-%d", i), CreatedAt: time.Unix(int64(100-i), 0),
		})
	}
	server := NewServer(deps.service(), configuredTenantID)

	pages := walkPages[stackListItemResponse](t, server, "/v1/tenants/tenant_123/stacks?limit=2")

	require.Len(t, pages, 3)
	var ids []string
	for _, page := range pages {
		for _, stack := range page {
			ids = append(ids, stack.ID)
		}
	}
	assert.Equal(t, []string{"stack_0", "stack_1", "stack_2", "stack_3", "stack_4"}, ids)
	assert.Len(t, pages[2], 1)
}

func TestListStacksFiltersByQuery(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.stacks.list = []domain.Stack{
		{ID: "stack_a", TenantID: "tenant_123", Name: "Payments", Slug: "payments", CreatedAt: time.Unix(3, 0)},
		{ID: "stack_b", TenantID: "tenant_123", Name: "Search", Slug: "search", CreatedAt: time.Unix(2, 0)},
	}
	server := NewServer(deps.service(), configuredTenantID)

	page := getListPage[stackListItemResponse](t, server, "/v1/tenants/tenant_123/stacks?q=pay")

	require.Len(t, page.Items, 1)
	assert.Equal(t, "stack_a", page.Items[0].ID)
	assert.Equal(t, "pay", deps.stacks.gotQuery)
	assert.Nil(t, page.NextCursor)
}

// Every paged list refuses the same way: a limit or cursor it can't read is a
// 400 invalid-query, a limit out of range a 422, each naming its parameter.
func TestPagedListsRejectBadPaging(t *testing.T) {
	t.Parallel()

	stacksCursor := app.EncodeCursor("stacks", app.StackPageCursor{CreatedAt: time.Now(), ID: "stack_1"})
	for _, path := range []string{
		"/v1/tenants/tenant_123/stacks",
		"/v1/tenants/tenant_123/templates",
		"/v1/tenants/tenant_123/templates/source_template_123/revisions",
		"/v1/tenants/tenant_123/stack-templates/stack_template_123/runs",
		"/v1/tenants/tenant_123/queue",
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			deps := newAPITestDependencies(t)
			deps.stackTemplates.stackTemplate.StackID = "stack_123"
			server := NewServer(deps.service(), configuredTenantID, WithQueueReader(&stubQueueReader{}))

			assertQueryProblem(t, server, path+"?limit=0", http.StatusUnprocessableEntity, "validation-failed", "limit", app.IssueOutOfRange)
			assertQueryProblem(t, server, path+"?limit=101", http.StatusUnprocessableEntity, "validation-failed", "limit", app.IssueOutOfRange)
			assertQueryProblem(t, server, path+"?limit=abc", http.StatusBadRequest, "invalid-query", "limit", app.IssueInvalidFormat)
			assertQueryProblem(t, server, path+"?cursor=%25%25%25", http.StatusBadRequest, "invalid-query", "cursor", app.IssueInvalidFormat)
			if !strings.HasSuffix(path, "/stacks") {
				// A cursor from another list is refused, not read as a
				// position in this one.
				assertQueryProblem(t, server, path+"?cursor="+stacksCursor, http.StatusBadRequest, "invalid-query", "cursor", app.IssueInvalidFormat)
			}
		})
	}
}

func TestListTemplatesPaging(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	for i := range 3 {
		deps.sourceTemplates.templates = append(deps.sourceTemplates.templates, domain.SourceTemplate{
			ID: domain.SourceTemplateID(fmt.Sprintf("source_template_%d", i)), TenantID: "tenant_123", CreatedAt: time.Unix(int64(100-i), 0),
		})
	}
	server := NewServer(deps.service(), configuredTenantID)

	pages := walkPages[templateResponse](t, server, "/v1/tenants/tenant_123/templates?limit=2")

	require.Len(t, pages, 2)
	assert.Equal(t, "source_template_0", pages[0][0].ID)
	assert.Equal(t, "source_template_1", pages[0][1].ID)
	assert.Equal(t, "source_template_2", pages[1][0].ID)
}

func TestListTemplateRevisionsForTemplatePaging(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.sourceTemplates.templates = []domain.SourceTemplate{{ID: "source_template_123", TenantID: "tenant_123"}}
	for i := range 3 {
		deps.sourceTemplates.revisions = append(deps.sourceTemplates.revisions, domain.TemplateRevision{
			ID: domain.TemplateRevisionID(fmt.Sprintf("rev_%d", i)), TenantID: "tenant_123",
			SourceTemplateID: "source_template_123", CreatedAt: time.Unix(int64(100-i), 0),
		})
	}
	server := NewServer(deps.service(), configuredTenantID)

	pages := walkPages[templateRevisionResponse](t, server, "/v1/tenants/tenant_123/templates/source_template_123/revisions?limit=2")

	require.Len(t, pages, 2)
	assert.Equal(t, "rev_0", pages[0][0].ID)
	assert.Equal(t, "rev_2", pages[1][0].ID)
}

func TestListTemplateRunsPaging(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.stackTemplates.stackTemplate.StackID = "stack_123"
	for number := 5; number >= 1; number-- {
		deps.templateRuns.list = append(deps.templateRuns.list, domain.TemplateRun{
			ID: domain.TemplateRunID(fmt.Sprintf("run_%d", number)), TenantID: "tenant_123",
			StackTemplateID: "stack_template_123", RunNumber: number,
		})
	}
	server := NewServer(deps.service(), configuredTenantID)

	pages := walkPages[templateRunResponse](t, server, "/v1/tenants/tenant_123/stack-templates/stack_template_123/runs?limit=2")

	var numbers [][]int
	for _, page := range pages {
		var pageNumbers []int
		for _, run := range page {
			pageNumbers = append(pageNumbers, run.RunNumber)
		}
		numbers = append(numbers, pageNumbers)
	}
	assert.Equal(t, [][]int{{5, 4}, {3, 2}, {1}}, numbers)
}

// A run's page is addressed by its number, so a run past the runs list's
// first page still opens.
func TestGetTemplateRunByNumber(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.stackTemplates.stackTemplate.StackID = "stack_123"
	deps.templateRuns.list = []domain.TemplateRun{
		{ID: "run_2", TenantID: "tenant_123", StackTemplateID: "stack_template_123", RunNumber: 2, ConfigJSON: json.RawMessage(`{"password":"x"}`)},
		{ID: "run_1", TenantID: "tenant_123", StackTemplateID: "stack_template_123", RunNumber: 1},
	}
	deps.templateRuns.executions = []domain.TemplateRunWorkflowExecution{{Phase: domain.RunPhasePlan, WorkflowID: "wf"}}
	server := NewServer(deps.service(), configuredTenantID)

	response := httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/stack-templates/stack_template_123/runs/1", nil))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var run templateRunResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &run))
	assert.Equal(t, "run_1", run.ID)
	assert.Len(t, run.Executions, 1)
	assert.NotContains(t, response.Body.String(), "workflow_id")

	for _, number := range []string{"3", "0", "abc", "1.5"} {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/stack-templates/stack_template_123/runs/"+number, nil))
		assert.Equal(t, http.StatusNotFound, response.Code, "run %s: %s", number, response.Body.String())
	}
}

// The queue used to stop at 50 without saying so; it now pages past that.
func TestListQueuePagesPastFifty(t *testing.T) {
	t.Parallel()

	reader := &stubQueueReader{}
	for i := range 120 {
		reader.statuses = append(reader.statuses, queue.Status{
			ID: int64(1000 - i), Kind: "reconcile_stack_grant", State: queue.StatePending,
			CreatedAt: time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC).Add(-time.Duration(i) * time.Second),
		})
	}
	deps := newAPITestDependencies(t)
	server := NewServer(deps.service(), configuredTenantID, WithQueueReader(reader))

	pages := walkPages[queueItemResponse](t, server, "/v1/tenants/tenant_123/queue?limit=100")

	require.Len(t, pages, 2)
	assert.Len(t, pages[0], 100)
	assert.Len(t, pages[1], 20)
	assert.Equal(t, 101, reader.gotLimit, "one more than the page, to prove a next one")
}

// apiCredentialRepository lists one credential per scope, so the credential
// lists can be read in tests that don't configure encryption.
type apiCredentialRepository struct{}

func (apiCredentialRepository) CreateCredential(context.Context, domain.CredentialSet) error {
	return nil
}

func (apiCredentialRepository) ListCredentials(_ context.Context, _ domain.TenantID, scope domain.CredentialScope, ownerID string) ([]domain.CredentialSet, error) {
	credential := domain.CredentialSet{ID: "cred_1", Name: "AWS_REGION"}
	if scope == domain.CredentialScopeStack {
		credential.StackID = domain.StackID(ownerID)
	} else {
		credential.StackTemplateID = domain.StackTemplateID(ownerID)
	}
	return []domain.CredentialSet{credential}, nil
}

func (apiCredentialRepository) DeleteCredential(context.Context, domain.TenantID, domain.CredentialSetID) error {
	return nil
}

func (apiCredentialRepository) ListCredentialsForStackTemplate(context.Context, domain.TenantID, domain.StackTemplateID) ([]domain.CredentialSet, error) {
	return nil, nil
}

// tenantGETRoute matches a GET route registered in server.go.
var tenantGETRoute = regexp.MustCompile(`handleTenantRoute\("GET (/v1/tenants/[^"]+)"`)

// TestEveryListRouteAnswersEnvelope reads the routes from server.go, so a list
// route added later can't answer with a bare array: every GET whose path ends
// in a collection, not an {id}, must answer {items, next_cursor}.
func TestEveryListRouteAnswersEnvelope(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("server.go")
	require.NoError(t, err)
	fixture := strings.NewReplacer(
		"{tenant_id}", "tenant_123", "{stack_id}", "stack_123", "{stack_template_id}", "stack_template_123",
		"{run_id}", "run_123", "{template_revision_id}", "template_123",
		"{template_id}", "source_template_123",
	)
	// Search needs q; the rest take no parameters.
	queries := map[string]string{"/v1/tenants/{tenant_id}/users/search": "?q=a"}
	// A log's event stream ends in a word, not an {id}, but is one log's
	// output, not a list.
	notLists := map[string]bool{"/v1/tenants/{tenant_id}/template-runs/{run_id}/logs/{name}/events": true}

	var lists []string
	for _, match := range tenantGETRoute.FindAllStringSubmatch(string(source), -1) {
		pattern := match[1]
		if strings.HasSuffix(pattern, "}") || notLists[pattern] {
			continue // one resource, not a list
		}
		lists = append(lists, pattern)
	}
	require.GreaterOrEqual(t, len(lists), 13, "the route parse found too few lists: %v", lists)

	for _, pattern := range lists {
		t.Run(pattern, func(t *testing.T) {
			t.Parallel()

			deps := newAPITestDependencies(t)
			deps.stackTemplates.stackTemplate.StackID = "stack_123"
			deps.sourceTemplates.templates = []domain.SourceTemplate{{ID: "source_template_123", TenantID: "tenant_123"}}
			service := deps.service()
			service.Credentials = apiCredentialRepository{}
			server := NewServer(service, configuredTenantID, WithQueueReader(&stubQueueReader{}))
			response := httptest.NewRecorder()

			server.ServeHTTP(response, authenticatedRequest(http.MethodGet, fixture.Replace(pattern)+queries[pattern], nil))

			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			decodeItems[json.RawMessage](t, response)
		})
	}
}

// The small bounded lists take the envelope but never page: next_cursor is
// always null, so a client can treat every list the same way.
func TestBoundedListsNeverPage(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/v1/tenants/tenant_123/attention",
		"/v1/tenants/tenant_123/stacks/stack_123/credentials",
		"/v1/tenants/tenant_123/stack-templates/stack_template_123/credentials",
		"/v1/tenants/tenant_123/stacks/stack_123/grants",
		"/v1/tenants/tenant_123/template-runs/run_123/logs",
		"/v1/tenants/tenant_123/template-revisions/template_123/variables",
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			deps := newAPITestDependencies(t)
			deps.stackTemplates.stackTemplate.StackID = "stack_123"
			service := deps.service()
			service.Credentials = apiCredentialRepository{}
			server := NewServer(service, configuredTenantID)

			page := getListPage[json.RawMessage](t, server, path+"?limit=1")
			assert.Nil(t, page.NextCursor)
		})
	}
}
