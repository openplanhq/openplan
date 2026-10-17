package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/openfga/openfga/pkg/storage"
	"github.com/openfga/openfga/pkg/storage/memory"
	"github.com/vishu42/openplan/internal/app"
	"github.com/vishu42/openplan/internal/authentication"

	"github.com/vishu42/openplan/internal/authorization"
	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/queue"
)

const apiOIDCSubject = "6fdb4b4c-2a8f-4cf7-945f-38f67f6a0e91"
const configuredTenantID = domain.TenantID("tenant_123")

func authenticatedRequest(method, target string, body io.Reader) *http.Request {
	request := httptest.NewRequest(method, target, body)
	ctx := authentication.ContextWithPrincipal(request.Context(), authentication.Principal{Subject: apiOIDCSubject})
	return request.WithContext(ctx)
}

func ordinaryAuthenticatedRequest(method, target string, body io.Reader) *http.Request {
	request := httptest.NewRequest(method, target, body)
	ctx := authentication.ContextWithPrincipal(request.Context(), authentication.Principal{Subject: apiOIDCSubject})
	return request.WithContext(ctx)
}

// platformTier is the capability set the model derives for a tier, which a
// test grants through its authorizer rather than through a token claim. There
// is no request-level variant any more: identity comes from the principal and
// the tier comes from OpenFGA, which is the whole point of #141.
// platformRole maps a tier name onto the role tuple that grants it. The
// capabilities each implies are the model's business, which is why they are no
// longer listed here.
func platformRole(tier string) (authorization.Relation, bool) {
	switch tier {
	case "admin":
		return authorization.RelationAdmin, true
	case "editor":
		return authorization.RelationEditor, true
	case "viewer":
		return authorization.RelationViewer, true
	default:
		return authorization.Relation{}, false
	}
}

func TestHealthzReturnsOK(t *testing.T) {
	t.Parallel()

	server := NewServer(app.NewService(app.Service{}), configuredTenantID)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if strings.TrimSpace(response.Body.String()) != `{"status":"ok"}` {
		t.Fatalf("body = %q", response.Body.String())
	}
}

func TestAuthenticatedServerProtectsEverythingButHealth(t *testing.T) {
	server := NewAuthenticatedServer(app.NewService(app.Service{}), configuredTenantID, false)
	// A route registered outside /v1 later, the way an admin or /v2 route
	// would be, is protected without being listed anywhere.
	server.mux.HandleFunc("GET /admin/settings", func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
	})

	for _, test := range []struct {
		path   string
		status int
	}{
		{path: "/healthz", status: http.StatusOK},
		{path: "/v1/tenants/tenant_123/stacks", status: http.StatusUnauthorized},
		{path: "/admin/settings", status: http.StatusUnauthorized},
		{path: "/v2/tenants/tenant_123/stacks", status: http.StatusUnauthorized},
		{path: "/", status: http.StatusUnauthorized},
	} {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if response.Code != test.status {
			t.Fatalf("%s status = %d, want %d", test.path, response.Code, test.status)
		}
	}
}

func TestTenantScopedRoutesRejectOtherTenantBeforeHandler(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, configuredTenantID)
	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "register template", method: http.MethodPost, path: "/v1/tenants/tenant_other/template-revisions"},
		{name: "list template revisions", method: http.MethodGet, path: "/v1/tenants/tenant_other/template-revisions"},
		{name: "get template registration", method: http.MethodGet, path: "/v1/tenants/tenant_other/template-registrations/registration_123"},
		{name: "get template variables", method: http.MethodGet, path: "/v1/tenants/tenant_other/template-revisions/revision_123/variables"},
		{name: "create stack", method: http.MethodPost, path: "/v1/tenants/tenant_other/stacks"},
		{name: "list stacks", method: http.MethodGet, path: "/v1/tenants/tenant_other/stacks"},
		{name: "list attention", method: http.MethodGet, path: "/v1/tenants/tenant_other/attention"},
		{name: "get stack", method: http.MethodGet, path: "/v1/tenants/tenant_other/stacks/stack_123"},
		{name: "install template", method: http.MethodPost, path: "/v1/tenants/tenant_other/stacks/stack_123/templates"},
		{name: "update template config", method: http.MethodPatch, path: "/v1/tenants/tenant_other/stack-templates/stack_template_123/config"},
		{name: "upgrade template", method: http.MethodPost, path: "/v1/tenants/tenant_other/stack-templates/stack_template_123/upgrade"},
		{name: "start run", method: http.MethodPost, path: "/v1/tenants/tenant_other/stack-templates/stack_template_123/runs"},
		{name: "get run", method: http.MethodGet, path: "/v1/tenants/tenant_other/template-runs/run_123"},
		{name: "list run logs", method: http.MethodGet, path: "/v1/tenants/tenant_other/template-runs/run_123/logs"},
		{name: "get run log artifact", method: http.MethodGet, path: "/v1/tenants/tenant_other/template-runs/run_123/logs/plan"},
		{name: "approve run", method: http.MethodPost, path: "/v1/tenants/tenant_other/template-runs/run_123/approval"},
		{name: "discard run", method: http.MethodPost, path: "/v1/tenants/tenant_other/template-runs/run_123/discard"},
		{name: "search users", method: http.MethodGet, path: "/v1/tenants/tenant_other/users/search?q=test"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			response := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, test.path, strings.NewReader("{"))

			server.ServeHTTP(response, request)

			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusNotFound, response.Body.String())
			}
			body := decodeProblem(t, response)
			if body.Type != blankType || body.Title != "Not Found" {
				t.Fatalf("body = %#v", body)
			}
		})
	}
}

func TestTenantBoundaryRejectsMissingAndMalformedPaths(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, configuredTenantID)
	for _, path := range []string{
		"/v1/tenants/stacks",
		"/v1/tenants/-tenant/stacks",
		"/v1/tenants/tenant%2Fother/stacks",
	} {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want %d", path, response.Code, http.StatusNotFound)
		}
	}
}

func TestAuthenticatedServerEvaluatesTenantAfterAuthentication(t *testing.T) {
	t.Parallel()

	server, cookie := sessionCookieServer(t, nil)
	path := "/v1/tenants/tenant_other/stacks"

	unauthenticated := httptest.NewRecorder()
	server.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, path, nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d", unauthenticated.Code, http.StatusUnauthorized)
	}

	authenticatedRequest := httptest.NewRequest(http.MethodGet, path, nil)
	authenticatedRequest.AddCookie(cookie)
	authenticated := httptest.NewRecorder()
	server.ServeHTTP(authenticated, authenticatedRequest)
	if authenticated.Code != http.StatusNotFound {
		t.Fatalf("authenticated status = %d, want %d", authenticated.Code, http.StatusNotFound)
	}
}

func TestAuthenticatedServerAllowsConfiguredTenantToReachService(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.stacks.list = []domain.Stack{{
		ID:       domain.StackID("stack_123"),
		TenantID: configuredTenantID,
		Name:     "Acme Prod",
		Slug:     "acme-prod",
	}}
	server, cookie := sessionCookieServer(t, deps.service())
	request := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant_123/stacks", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if deps.stacks.gotListTenantID != configuredTenantID {
		t.Fatalf("tenant list lookup = %q, want %q", deps.stacks.gotListTenantID, configuredTenantID)
	}
	var body []stackResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body) != 1 || body[0].ID != "stack_123" || body[0].Slug != "acme-prod" {
		t.Fatalf("stack response = %#v", body)
	}
}

// sessionCookieServer builds a server whose middleware can actually
// authenticate someone. The cookie is the only credential now, so a test that
// wants to reach a protected route has to hold a live session row rather than
// set an Authorization header.
func sessionCookieServer(t *testing.T, service *app.Service) (*Server, *http.Cookie) {
	t.Helper()

	const raw = "server-test-session"
	now := time.Now().UTC()
	sessions := &serverTestSessionStore{byHash: map[string]authentication.Session{
		authentication.HashSessionID(raw): {
			IDHash:            authentication.HashSessionID(raw),
			Subject:           "user-123",
			LastSeenAt:        now,
			AbsoluteExpiresAt: now.Add(time.Hour),
		},
	}}
	server := NewAuthenticatedServer(service, configuredTenantID, false, WithAuth(AuthConfig{
		Sessions:       sessions,
		SessionIdleTTL: time.Hour,
	}))
	return server, &http.Cookie{Name: authentication.SessionCookieName, Value: raw}
}

// serverTestSessionStore satisfies authentication.SessionStore with only the lookup the
// middleware performs; the rest is unreachable from these tests.
type serverTestSessionStore struct {
	byHash map[string]authentication.Session
}

func (store *serverTestSessionStore) CreateSession(context.Context, authentication.Session) error {
	return nil
}

func (store *serverTestSessionStore) SessionByHash(_ context.Context, idHash string) (authentication.Session, error) {
	session, ok := store.byHash[idHash]
	if !ok {
		return authentication.Session{}, authentication.ErrSessionNotFound
	}
	return session, nil
}

func (store *serverTestSessionStore) TouchSession(context.Context, string, time.Time) error {
	return nil
}

func (store *serverTestSessionStore) RevokeSession(context.Context, string, time.Time) error {
	return nil
}

func (store *serverTestSessionStore) RevokeSessionsByIDPSessionID(context.Context, string, time.Time) (int, error) {
	return 0, nil
}

func (store *serverTestSessionStore) RevokeSessionsBySubject(context.Context, string, time.Time) (int, error) {
	return 0, nil
}

func (store *serverTestSessionStore) RevokeSessionsBySubjectWithoutIDPSession(context.Context, string, time.Time) (int, error) {
	return 0, nil
}

func (store *serverTestSessionStore) DeleteSessionsExpiredBefore(context.Context, time.Time) (int, error) {
	return 0, nil
}

func TestStartTemplateRunCallsService(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, 7, 3, 11, 30, 0, 0, time.UTC)
	deps := newAPITestDependencies(t)
	deps.stackTemplates.stackTemplate = domain.StackTemplate{
		ID:                        domain.StackTemplateID("stack_template_123"),
		StackID:                   domain.StackID("stack_123"),
		DesiredTemplateRevisionID: domain.TemplateRevisionID("template_123"),
		WorkspaceName:             "smoke-workspace",
		Lifecycle:                 domain.StackTemplateActive,
	}
	deps.runID = domain.TemplateRunID("run_123")
	deps.now = startedAt
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(
		http.MethodPost,
		"/v1/tenants/tenant_123/stack-templates/stack_template_123/runs",
		strings.NewReader(`{"operation":"plan"}`),
	)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusCreated, response.Body.String())
	}
	if got, want := response.Header().Get("Location"), "/v1/tenants/tenant_123/template-runs/run_123"; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
	if deps.stackTemplates.gotTenantID != domain.TenantID("tenant_123") {
		t.Fatalf("tenant id = %q", deps.stackTemplates.gotTenantID)
	}
	if deps.stackTemplates.gotID != domain.StackTemplateID("stack_template_123") {
		t.Fatalf("stack template revision id = %q", deps.stackTemplates.gotID)
	}
	if deps.templateRuns.created.Operation != domain.OperationPlan {
		t.Fatalf("operation = %q", deps.templateRuns.created.Operation)
	}
	if deps.templateRuns.created.TriggerActor != domain.UserID(apiOIDCSubject) {
		t.Fatalf("trigger actor = %q, want %q", deps.templateRuns.created.TriggerActor, apiOIDCSubject)
	}

	var body domain.TemplateRun
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.ID != domain.TemplateRunID("run_123") {
		t.Fatalf("id = %q, want run_123", body.ID)
	}
	if body.Status != domain.TemplateRunQueued {
		t.Fatalf("status = %q, want queued", body.Status)
	}
	if !body.CreatedAt.Equal(startedAt) {
		t.Fatalf("created_at = %v, want %v", body.CreatedAt, startedAt)
	}
}

func TestStartTemplateRunPassesAutoApproveThrough(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.stackTemplates.stackTemplate = domain.StackTemplate{
		ID:                        "stack_template_123",
		StackID:                   "stack_123",
		DesiredTemplateRevisionID: "template_123",
		WorkspaceName:             "smoke-workspace",
		Lifecycle:                 domain.StackTemplateActive,
	}
	deps.templates.template = domain.TemplateRevision{ID: "template_123", Status: domain.TemplateRevisionActive}
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(
		http.MethodPost,
		"/v1/tenants/tenant_123/stack-templates/stack_template_123/runs",
		strings.NewReader(`{"operation":"apply","auto_approve":true}`),
	)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusCreated, response.Body.String())
	}
	if !deps.templateRuns.created.AutoApprove {
		t.Fatal("created run is not auto-approved")
	}
	var body domain.TemplateRun
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.AutoApprove {
		t.Fatal("response does not report auto_approve")
	}
}

func TestApproveRunMapsStalePlanToConflictWithItsOwnCode(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.stackTemplates.stackTemplate = domain.StackTemplate{
		ID:                        domain.StackTemplateID("stack_template_123"),
		StackID:                   domain.StackID("stack_123"),
		DesiredTemplateRevisionID: domain.TemplateRevisionID("template_123"),
		DesiredConfigJSON:         json.RawMessage(`{"region":"eu-west-1"}`),
		WorkspaceName:             "smoke-workspace",
		Lifecycle:                 domain.StackTemplateActive,
		// Planned against the config as it was before the last save.
		PendingPlanRunID:              domain.TemplateRunID("run_123"),
		PendingPlanTemplateRevisionID: domain.TemplateRevisionID("template_123"),
		PendingPlanConfigJSON:         json.RawMessage(`{"region":"us-east-1"}`),
	}
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(
		http.MethodPost,
		"/v1/tenants/tenant_123/template-runs/run_123/approval",
		strings.NewReader(`{}`),
	)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusConflict, response.Body.String())
	}
	body := decodeProblem(t, response)
	// Its own type so the client can tell the user to
	// re-plan rather than showing a generic failure.
	if body.Type != problemTypeURI("plan-stale") {
		t.Fatalf("type = %q, want %q", body.Type, problemTypeURI("plan-stale"))
	}
	if deps.templateRuns.created.ID != "" {
		t.Fatalf("created run = %#v, want no persisted run", deps.templateRuns.created)
	}
}

func TestStartTemplateRunMapsAnInFlightRunToConflictWithItsOwnCode(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.stackTemplates.stackTemplate = domain.StackTemplate{
		ID:                        domain.StackTemplateID("stack_template_123"),
		StackID:                   domain.StackID("stack_123"),
		DesiredTemplateRevisionID: domain.TemplateRevisionID("template_123"),
		DesiredConfigJSON:         json.RawMessage(`{"region":"eu-west-1"}`),
		WorkspaceName:             "smoke-workspace",
		Lifecycle:                 domain.StackTemplateActive,
	}
	// What the store returns when template_runs_in_flight_idx refuses the insert.
	deps.templateRuns.createErr = app.ErrTemplateRunInFlight
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(
		http.MethodPost,
		"/v1/tenants/tenant_123/stack-templates/stack_template_123/runs",
		strings.NewReader(`{"operation":"plan"}`),
	)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusConflict, response.Body.String())
	}
	body := decodeProblem(t, response)
	// Its own type so the client knows its run history is
	// stale and can refetch, rather than showing an unexplained failure.
	if body.Type != problemTypeURI("run-in-flight") {
		t.Fatalf("type = %q, want %q", body.Type, problemTypeURI("run-in-flight"))
	}
}

func TestAddTemplateToStackMapsDuplicateComponentKeyToConflictWithItsOwnCode(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.stacks.stack = domain.Stack{ID: domain.StackID("stack_123"), TenantID: domain.TenantID("tenant_123"), Slug: "acme-prod"}
	deps.templates.template = domain.TemplateRevision{ID: domain.TemplateRevisionID("template_123"), TenantID: domain.TenantID("tenant_123"), Status: domain.TemplateRevisionActive}
	deps.templates.variables = []domain.TemplateVariable{{Name: "region", Required: true}}
	deps.stackTemplateID = domain.StackTemplateID("stack_template_a1b2c3d4")
	// What the store returns when the stack already has a template under this
	// component_key.
	deps.stackTemplateInstaller.createErr = app.ErrDuplicateStackTemplateComponentKey
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(
		http.MethodPost,
		"/v1/tenants/tenant_123/stacks/stack_123/templates",
		strings.NewReader(`{"template_revision_id":"template_123","config":{"region":"us-east-1"}}`),
	)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusConflict, response.Body.String())
	}
	body := decodeProblem(t, response)
	// Its own type, and an errors entry on component_key, so the client can
	// mark the field instead of showing a generic failure.
	if body.Type != problemTypeURI("duplicate-component-key") {
		t.Fatalf("type = %q, want %q", body.Type, problemTypeURI("duplicate-component-key"))
	}
}

func TestCreateCredentialWithoutEncryptionKeyReturnsServiceUnavailable(t *testing.T) {
	t.Parallel()

	// The test service wires no credential store or encryptor, which is how the
	// API runs when no encryption key is configured.
	deps := newAPITestDependencies(t)
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(
		http.MethodPost,
		"/v1/tenants/tenant_123/stacks/stack_123/credentials",
		strings.NewReader(`{"name":"AWS_ACCESS_KEY_ID","value":"secret"}`),
	)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
	body := decodeProblem(t, response)
	if body.Type != problemTypeURI("credential-encryption-unavailable") {
		t.Fatalf("type = %q, want %q", body.Type, problemTypeURI("credential-encryption-unavailable"))
	}
}

func TestStackTemplateResponseCarriesDerivedStatesNotRawConfigs(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.stackTemplates.stackTemplate = domain.StackTemplate{
		ID:                            domain.StackTemplateID("stack_template_123"),
		TenantID:                      domain.TenantID("tenant_123"),
		DesiredTemplateRevisionID:     domain.TemplateRevisionID("template_rev_1"),
		Lifecycle:                     domain.StackTemplateActive,
		PendingPlanRunID:              domain.TemplateRunID("run_plan_1"),
		PendingPlanTemplateRevisionID: domain.TemplateRevisionID("template_rev_1"),
		PendingPlanConfigJSON:         json.RawMessage(`{"region":"us-west-2"}`),
		LastAppliedRunID:              domain.TemplateRunID("run_apply_1"),
		LastAppliedTemplateRevisionID: domain.TemplateRevisionID("template_rev_1"),
		LastAppliedConfigJSON:         json.RawMessage(`{"region":"us-east-1"}`),
	}
	deps.templates.variables = []domain.TemplateVariable{{Name: "region", Required: true}}
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(
		http.MethodPatch,
		"/v1/tenants/tenant_123/stack-templates/stack_template_123/config",
		strings.NewReader(`{"config":{"region":"us-west-2"}}`),
	)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["plan_state"] != "matches" {
		t.Fatalf("plan_state = %#v, want matches", body["plan_state"])
	}
	if body["live_state"] != "differs" {
		t.Fatalf("live_state = %#v, want differs", body["live_state"])
	}
	if body["pending_plan_run_id"] != "run_plan_1" {
		t.Fatalf("pending_plan_run_id = %#v", body["pending_plan_run_id"])
	}
	// The snapshot configs stay server-side; returning them invites the client
	// to redo the comparison, which is the mistake these states replace.
	for _, key := range []string{"pending_plan_config_json", "last_applied_config_json"} {
		if _, present := body[key]; present {
			t.Fatalf("response exposes %s", key)
		}
	}
}

func TestStartTemplateRunRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	server := NewServer(newAPITestDependencies(t).service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(
		http.MethodPost,
		"/v1/tenants/tenant_123/stack-templates/stack_template_123/runs",
		strings.NewReader(`{"operation":`),
	)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

// The body parses, but its content breaks a rule: 422, on the field.
func TestStartTemplateRunMapsUnsupportedOperationToValidationFailed(t *testing.T) {
	t.Parallel()

	server := NewServer(newAPITestDependencies(t).service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(
		http.MethodPost,
		"/v1/tenants/tenant_123/stack-templates/stack_template_123/runs",
		strings.NewReader(`{"operation":"refresh"}`),
	)

	server.ServeHTTP(response, request)

	assertValidationFailed(t, response, "#/operation unsupported")
}

// assertValidationFailed checks a 422 validation-failed and its errors, each
// written as "<pointer or parameter> <issue>", in order.
func assertValidationFailed(t *testing.T, response *httptest.ResponseRecorder, want ...string) {
	t.Helper()
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body = %s", response.Code, response.Body.String())
	}
	problem := decodeProblem(t, response)
	if problem.Type != problemTypeURI("validation-failed") {
		t.Fatalf("type = %q, want validation-failed", problem.Type)
	}
	got := make([]string, 0, len(problem.Errors))
	for _, entry := range problem.Errors {
		where := entry.Pointer
		if entry.Parameter != "" {
			where = entry.Parameter
		}
		got = append(got, where+" "+string(entry.Issue))
	}
	if strings.Join(got, ", ") != strings.Join(want, ", ") {
		t.Fatalf("errors = %q, want %q", got, want)
	}
}

// A missing stack template answers 404 on a write, as it does on a read (#387).
func TestStartTemplateRunHidesMissingStackTemplateAsNotFound(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.stackTemplates.getErr = app.ErrNotFound
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(
		http.MethodPost,
		"/v1/tenants/tenant_123/stack-templates/missing_stack_template/runs",
		strings.NewReader(`{"operation":"plan"}`),
	)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusNotFound, response.Body.String())
	}
}

func TestRegisterTemplateCallsService(t *testing.T) {
	t.Parallel()

	requestedAt := time.Date(2026, 7, 6, 11, 30, 0, 0, time.UTC)
	deps := newAPITestDependencies(t).withPlatformTier("admin")
	deps.registrationID = domain.TemplateRegistrationID("template_registration_123")
	deps.now = requestedAt
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(
		http.MethodPost,
		"/v1/tenants/tenant_123/template-revisions",
		strings.NewReader(`{"repo_owner":"acme","repo_name":"infra-templates","source_ref":"v0.0.1","root_path":"modules/vpc"}`),
	)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusAccepted, response.Body.String())
	}
	// The registration is read back at its own URL, not the one it was posted to.
	if got, want := response.Header().Get("Location"), "/v1/tenants/tenant_123/template-registrations/template_registration_123"; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
	if deps.registrations.created.TenantID != domain.TenantID("tenant_123") {
		t.Fatalf("tenant id = %q", deps.registrations.created.TenantID)
	}
	if deps.registrations.created.RepoOwner != "acme" {
		t.Fatalf("repo owner = %q", deps.registrations.created.RepoOwner)
	}
	if deps.registrations.created.SourceRef != "v0.0.1" {
		t.Fatalf("source ref = %q", deps.registrations.created.SourceRef)
	}
	if deps.registrations.created.RequestedBy != domain.UserID(apiOIDCSubject) {
		t.Fatalf("requested by = %q, want %q", deps.registrations.created.RequestedBy, apiOIDCSubject)
	}
	if len(deps.work.requests) != 1 || deps.work.requests[0].Kind != app.KindStartTemplateSync {
		t.Fatalf("queued requests = %#v, want one start_template_sync request", deps.work.requests)
	}

	var body domain.TemplateRegistration
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.ID != domain.TemplateRegistrationID("template_registration_123") {
		t.Fatalf("id = %q, want template_registration_123", body.ID)
	}
	if body.Status != domain.TemplateRegistrationPending {
		t.Fatalf("status = %q, want pending", body.Status)
	}
	if !body.RequestedAt.Equal(requestedAt) {
		t.Fatalf("requested_at = %v, want %v", body.RequestedAt, requestedAt)
	}
}

func TestRegisterTemplateRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	server := NewServer(newAPITestDependencies(t).service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(
		http.MethodPost,
		"/v1/tenants/tenant_123/template-revisions",
		strings.NewReader(`{"repo_owner":`),
	)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestRegisterTemplateMapsMissingSourceRefToValidationFailed(t *testing.T) {
	t.Parallel()

	server := NewServer(newAPITestDependencies(t).withPlatformTier("admin").service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(
		http.MethodPost,
		"/v1/tenants/tenant_123/template-revisions",
		strings.NewReader(`{"repo_owner":"acme","repo_name":"infra-templates","root_path":"modules/vpc"}`),
	)

	server.ServeHTTP(response, request)

	assertValidationFailed(t, response, "#/source_ref required")
}

func TestCreateStackCallsService(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 7, 6, 13, 30, 0, 0, time.UTC)
	deps := newAPITestDependencies(t).withPlatformTier("admin")
	deps.stackID = domain.StackID("stack_123")
	deps.now = createdAt
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(
		http.MethodPost,
		"/v1/tenants/tenant_123/stacks",
		strings.NewReader(`{"name":"Acme Prod","tags":{"env":"prod"},"default_credential_ids":["credential_123"]}`),
	)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusCreated, response.Body.String())
	}
	if got, want := response.Header().Get("Location"), "/v1/tenants/tenant_123/stacks/"+string(deps.createdStackID); got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
	if deps.stacks.created.TenantID != domain.TenantID("tenant_123") {
		t.Fatalf("tenant id = %q, want tenant_123", deps.stacks.created.TenantID)
	}
	if deps.stacks.created.Slug != "acme-prod" {
		t.Fatalf("slug = %q, want acme-prod", deps.stacks.created.Slug)
	}
	if deps.stacks.created.CreatedBy != domain.UserID(apiOIDCSubject) {
		t.Fatalf("created by = %q, want %q", deps.stacks.created.CreatedBy, apiOIDCSubject)
	}

	var body stackResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.ID != string(deps.createdStackID) {
		t.Fatalf("response id = %q, want %q", body.ID, deps.createdStackID)
	}
	if body.Tags["env"] != "prod" {
		t.Fatalf("response tags = %#v", body.Tags)
	}
}

func TestWriteAppErrorMapsAuthorizationDependencyFailure(t *testing.T) {
	t.Parallel()

	response := httptest.NewRecorder()
	writeAppError(response, errors.New("authorization unavailable"))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	body := decodeProblem(t, response)
	if body.Type != blankType {
		t.Fatalf("type = %q, want %q", body.Type, blankType)
	}
}

func TestCreateStackRejectsPrincipalWithoutCreatorRole(t *testing.T) {
	t.Parallel()

	deps := newBareAPITestDependencies(t)
	server := NewServer(deps.service(), configuredTenantID)
	request := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant_123/stacks", strings.NewReader(`{"name":"Acme"}`))
	request = request.WithContext(authentication.ContextWithPrincipal(request.Context(), authentication.Principal{Subject: apiOIDCSubject}))
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if deps.stacks.created.ID != "" {
		t.Fatalf("created stack = %#v, want no persistence", deps.stacks.created)
	}
}

// CreateStack no longer writes to OpenFGA at request time, so there is no
// "owner write failed after persistence" case left to map. What replaces it is
// a failing unit of work, which persists nothing and surfaces as 503.
func TestCreateStackMapsUnitOfWorkFailure(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t).withPlatformTier("admin")
	service := app.NewService(app.Service{
		Authorization: deps.authorizer,
		Stacks:        &deps.stacks,
		Work:          &apiUnitOfWork{stacks: &deps.stacks, err: errors.New("authorization unavailable")},
		StackIDs:      fixedStackIDGenerator{id: deps.createdStackID},
		Clock:         fixedClock{now: deps.now},
	})
	server := NewServer(service, configuredTenantID)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, authenticatedRequest(http.MethodPost, "/v1/tenants/tenant_123/stacks", strings.NewReader(`{"name":"Acme"}`)))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if deps.stacks.created.ID != "" {
		t.Fatal("a failed unit of work must not persist the stack")
	}
}

func TestCreateStackWritesTheOwnerGrantWithoutQueueing(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t).withPlatformTier("admin")
	work := &apiUnitOfWork{stacks: &deps.stacks}
	service := app.NewService(app.Service{
		Authorization: deps.authorizer,
		Stacks:        &deps.stacks,
		Work:          work,
		StackIDs:      fixedStackIDGenerator{id: deps.createdStackID},
		Clock:         fixedClock{now: deps.now},
	})
	server := NewServer(service, configuredTenantID)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, authenticatedRequest(http.MethodPost, "/v1/tenants/tenant_123/stacks", strings.NewReader(`{"name":"Acme"}`)))

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusCreated, response.Body.String())
	}
	if len(work.requests) != 0 {
		t.Fatalf("enqueued %d intents, want 0 -- the owner grant is part of the commit", len(work.requests))
	}
	if deps.stacks.created.ID == "" {
		t.Fatal("stack was not persisted")
	}

	// The caller gets a usable stack rather than one to poll. Nothing is
	// deferred, so there is no state between created and ready to report.
	var body stackResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != string(domain.StackStatusReady) {
		t.Fatalf("status = %q, want %q", body.Status, domain.StackStatusReady)
	}
}

func TestListStacksReturnsTenantStacks(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 7, 6, 13, 30, 0, 0, time.UTC)
	deps := newAPITestDependencies(t)
	deps.stacks.list = []domain.Stack{
		{
			ID:        domain.StackID("stack_123"),
			TenantID:  domain.TenantID("tenant_123"),
			Name:      "Acme Prod",
			Slug:      "acme-prod",
			Tags:      map[string]string{"env": "prod"},
			CreatedBy: domain.UserID("user_123"),
			CreatedAt: createdAt,
		},
	}
	deps.stackOverview.templates = []domain.StackTemplate{
		{ID: "stack_template_1", StackID: "stack_123"},
		{ID: "stack_template_2", StackID: "stack_123"},
		{ID: "stack_template_other", StackID: "stack_hidden"},
	}
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/stacks", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if deps.stacks.gotListTenantID != domain.TenantID("tenant_123") {
		t.Fatalf("tenant list lookup = %q, want tenant_123", deps.stacks.gotListTenantID)
	}

	var body []stackListItemResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body) != 1 {
		t.Fatalf("len(body) = %d, want 1", len(body))
	}
	if body[0].ID != "stack_123" || body[0].Slug != "acme-prod" {
		t.Fatalf("stack response = %#v", body[0])
	}
	if body[0].TemplateCount != 2 {
		t.Fatalf("template_count = %d, want 2", body[0].TemplateCount)
	}
}

func TestListAttentionReturnsWaitingPlans(t *testing.T) {
	t.Parallel()

	plannedAt := time.Date(2026, 10, 3, 9, 30, 0, 0, time.UTC)
	deps := newAPITestDependencies(t)
	deps.stacks.list = []domain.Stack{{ID: "stack_123", TenantID: "tenant_123", Name: "Acme Prod", Slug: "acme-prod"}}
	deps.stackOverview.templates = []domain.StackTemplate{{
		ID:                        "stack_template_123",
		StackID:                   "stack_123",
		DesiredTemplateRevisionID: "template_123",
		WorkspaceName:             "acme-prod-template_123",
		Lifecycle:                 domain.StackTemplateActive,
		PendingPlanRunID:          "run_123",
		PendingPlanAt:             plannedAt,
	}}
	deps.stackOverview.waiting = []domain.TemplateRun{{
		ID:              "run_123",
		TenantID:        "tenant_123",
		StackTemplateID: "stack_template_123",
		Operation:       domain.OperationPlan,
		Status:          domain.TemplateRunWaitingApproval,
		TriggerActor:    "user_123",
		RunNumber:       7,
		PlanSummary:     &domain.PlanSummary{Add: 2, Change: 1},
	}}
	deps.templates.templates = []domain.TemplateRevision{{ID: "template_123", RepoName: "infra-modules", RootPath: "modules/eks-cluster"}}
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/attention", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	var body []struct {
		Kind  string `json:"kind"`
		At    string `json:"at"`
		Stack struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Slug string `json:"slug"`
		} `json:"stack"`
		StackTemplate struct {
			ID            string `json:"id"`
			WorkspaceName string `json:"workspace_name"`
			DisplayName   string `json:"display_name"`
		} `json:"stack_template"`
		Run *struct {
			ID          string             `json:"id"`
			RunNumber   int                `json:"run_number"`
			PlanSummary domain.PlanSummary `json:"plan_summary"`
		} `json:"run"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body) != 1 {
		t.Fatalf("len(body) = %d, want 1", len(body))
	}
	item := body[0]
	if item.Kind != "waiting_approval" || item.At != "2026-10-03T09:30:00Z" {
		t.Errorf("kind, at = %q, %q", item.Kind, item.At)
	}
	if item.Stack.ID != "stack_123" || item.Stack.Name != "Acme Prod" || item.Stack.Slug != "acme-prod" {
		t.Errorf("stack = %+v", item.Stack)
	}
	if item.StackTemplate.ID != "stack_template_123" || item.StackTemplate.WorkspaceName != "acme-prod-template_123" || item.StackTemplate.DisplayName != "eks-cluster" {
		t.Errorf("stack template = %+v", item.StackTemplate)
	}
	if item.Run == nil || item.Run.ID != "run_123" || item.Run.RunNumber != 7 || item.Run.PlanSummary.Add != 2 {
		t.Errorf("run = %+v", item.Run)
	}
}

func TestListAttentionReturnsAnEmptyList(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/attention", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if got := strings.TrimSpace(response.Body.String()); got != "[]" {
		t.Fatalf("body = %s, want []", got)
	}
}

func TestGetStackReturnsStackView(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.stacks.view = app.StackView{
		Stack: domain.Stack{
			ID:       domain.StackID("stack_123"),
			TenantID: domain.TenantID("tenant_123"),
			Name:     "Acme Prod",
			Slug:     "acme-prod",
			Tags:     map[string]string{"env": "prod"},
		},
		Templates: []app.StackTemplateView{
			{StackTemplate: domain.StackTemplate{
				ID:                        domain.StackTemplateID("stack_template_123"),
				TenantID:                  domain.TenantID("tenant_123"),
				StackID:                   domain.StackID("stack_123"),
				DesiredTemplateRevisionID: domain.TemplateRevisionID("template_123"),
				WorkspaceName:             "meg_acme_prod_late_123",
				// A persisted row always has a desired config: the column is
				// not null and CreateStackTemplate seeds it from the install
				// config. Setting only the install config modelled a row that
				// cannot exist, which the removed fallback used to paper over.
				InstalledConfigJSON: json.RawMessage(`{"region":"us-east-1"}`),
				DesiredConfigJSON:   json.RawMessage(`{"region":"us-east-1"}`),
				Lifecycle:           domain.StackTemplateActive,
			}},
		},
	}
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/stacks/stack_123", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if deps.stacks.gotStackID != domain.StackID("stack_123") {
		t.Fatalf("stack lookup = %q, want stack_123", deps.stacks.gotStackID)
	}

	var body stackViewResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Stack.ID != "stack_123" {
		t.Fatalf("stack id = %q, want stack_123", body.Stack.ID)
	}
	if len(body.Templates) != 1 {
		t.Fatalf("len(templates) = %d, want 1", len(body.Templates))
	}
	if body.Templates[0].Config["region"] != "us-east-1" {
		t.Fatalf("template config = %#v", body.Templates[0].Config)
	}
}

func TestAddTemplateToStackCallsService(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.stacks.stack = domain.Stack{ID: domain.StackID("stack_123"), TenantID: domain.TenantID("tenant_123"), Slug: "acme-prod"}
	deps.templates.template = domain.TemplateRevision{
		ID: domain.TemplateRevisionID("template_123"), TenantID: domain.TenantID("tenant_123"), Status: domain.TemplateRevisionActive,
		RepoName: "infra", RootPath: "modules/vpc", SourceRef: "v1.2.0",
	}
	deps.templates.variables = []domain.TemplateVariable{{Name: "region", Required: true}}
	deps.stackTemplateID = domain.StackTemplateID("stack_template_a1b2c3d4")
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(
		http.MethodPost,
		"/v1/tenants/tenant_123/stacks/stack_123/templates",
		strings.NewReader(`{"template_revision_id":"template_123","config":{"region":"us-east-1"}}`),
	)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusCreated, response.Body.String())
	}
	if deps.stackTemplateInstaller.created.StackID != domain.StackID("stack_123") {
		t.Fatalf("stack id = %q, want stack_123", deps.stackTemplateInstaller.created.StackID)
	}
	if deps.stackTemplateInstaller.created.CreatedBy != domain.UserID(apiOIDCSubject) {
		t.Fatalf("created by = %q, want %q", deps.stackTemplateInstaller.created.CreatedBy, apiOIDCSubject)
	}

	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := body["template_revision_id"]; ok {
		t.Fatalf("response should not include legacy template_revision_id: %#v", body)
	}
	if body["id"] != "stack_template_a1b2c3d4" {
		t.Fatalf("response id = %q, want stack_template_a1b2c3d4", body["id"])
	}
	if body["created_by"] != apiOIDCSubject {
		t.Fatalf("response created by = %q, want %q", body["created_by"], apiOIDCSubject)
	}
	config, ok := body["config"].(map[string]any)
	if !ok || config["region"] != "us-east-1" {
		t.Fatalf("response config = %#v", body["config"])
	}
	// Labelled like a read of the stack, so a client caching this response
	// does not show blanks.
	if body["display_name"] != "vpc" || body["source_ref"] != "v1.2.0" {
		t.Fatalf("display_name = %q, source_ref = %q, want vpc and v1.2.0", body["display_name"], body["source_ref"])
	}
}

func TestUpdateStackTemplateConfigCallsService(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.stackTemplates.stackTemplate = domain.StackTemplate{
		ID:                        domain.StackTemplateID("stack_template_123"),
		TenantID:                  domain.TenantID("tenant_123"),
		DesiredTemplateRevisionID: domain.TemplateRevisionID("template_rev_1"),
		Lifecycle:                 domain.StackTemplateActive,
	}
	deps.templates.template = domain.TemplateRevision{
		ID: domain.TemplateRevisionID("template_rev_1"), TenantID: domain.TenantID("tenant_123"),
		RepoName: "infra", RootPath: "modules/vpc", SourceRef: "v1.2.0",
	}
	deps.templates.variables = []domain.TemplateVariable{{Name: "region", Required: true}}
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(
		http.MethodPatch,
		"/v1/tenants/tenant_123/stack-templates/stack_template_123/config",
		strings.NewReader(`{"config":{"region":"us-west-2"}}`),
	)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if string(deps.stackTemplates.gotConfigJSON) != `{"region":"us-west-2"}` {
		t.Fatalf("config update = %s", deps.stackTemplates.gotConfigJSON)
	}

	var body stackTemplateResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Config["region"] != "us-west-2" {
		t.Fatalf("response config = %#v", body.Config)
	}
	if body.DisplayName != "vpc" || body.SourceRef != "v1.2.0" {
		t.Fatalf("display_name = %q, source_ref = %q, want vpc and v1.2.0", body.DisplayName, body.SourceRef)
	}
}

// The edit replaces the whole config, so leaving config out must not read as
// "clear everything".
func TestUpdateStackTemplateConfigRequiresConfig(t *testing.T) {
	t.Parallel()

	for _, requestBody := range []string{`{}`, `{"config":null}`} {
		deps := newAPITestDependencies(t)
		deps.stackTemplates.stackTemplate = domain.StackTemplate{
			ID:                        domain.StackTemplateID("stack_template_123"),
			TenantID:                  domain.TenantID("tenant_123"),
			DesiredTemplateRevisionID: domain.TemplateRevisionID("template_rev_1"),
			Lifecycle:                 domain.StackTemplateActive,
		}
		server := NewServer(deps.service(), configuredTenantID)
		response := httptest.NewRecorder()

		server.ServeHTTP(response, authenticatedRequest(
			http.MethodPatch,
			"/v1/tenants/tenant_123/stack-templates/stack_template_123/config",
			strings.NewReader(requestBody),
		))

		assertValidationFailed(t, response, "#/config required")
		if deps.stackTemplates.gotConfigJSON != nil {
			t.Fatalf("%s: config updated to %s, want no update", requestBody, deps.stackTemplates.gotConfigJSON)
		}
	}
}

func TestUpgradeStackTemplateCallsService(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.stackTemplates.stackTemplate = domain.StackTemplate{
		ID:                        domain.StackTemplateID("stack_template_123"),
		TenantID:                  domain.TenantID("tenant_123"),
		SourceTemplateID:          domain.SourceTemplateID("source_template_vpc"),
		DesiredTemplateRevisionID: domain.TemplateRevisionID("template_rev_1"),
		DesiredConfigJSON:         json.RawMessage(`{"region":"us-east-1"}`),
		Lifecycle:                 domain.StackTemplateActive,
	}
	deps.templates.template = domain.TemplateRevision{
		ID:               domain.TemplateRevisionID("template_rev_2"),
		TenantID:         domain.TenantID("tenant_123"),
		SourceTemplateID: domain.SourceTemplateID("source_template_vpc"),
		Status:           domain.TemplateRevisionActive,
		RepoName:         "infra",
		RootPath:         "modules/vpc",
		SourceRef:        "v2.0.0",
	}
	deps.templates.variables = []domain.TemplateVariable{{Name: "region", Required: true}}
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(
		http.MethodPost,
		"/v1/tenants/tenant_123/stack-templates/stack_template_123/upgrade",
		strings.NewReader(`{"target_template_revision_id":"template_rev_2"}`),
	)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if deps.stackTemplates.gotDesiredTemplateRevisionID != domain.TemplateRevisionID("template_rev_2") {
		t.Fatalf("desired template revision update = %q, want template_rev_2", deps.stackTemplates.gotDesiredTemplateRevisionID)
	}

	var body stackTemplateResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.DesiredTemplateRevisionID != "template_rev_2" {
		t.Fatalf("desired template revision id = %q, want template_rev_2", body.DesiredTemplateRevisionID)
	}
	if body.Config["region"] != "us-east-1" {
		t.Fatalf("response config = %#v", body.Config)
	}
	// The target revision's ref, not the one the template had before.
	if body.DisplayName != "vpc" || body.SourceRef != "v2.0.0" {
		t.Fatalf("display_name = %q, source_ref = %q, want vpc and v2.0.0", body.DisplayName, body.SourceRef)
	}
}

func TestUpgradeStackTemplateMapsMissingRequiredVariableToValidationFailed(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.stackTemplates.stackTemplate = domain.StackTemplate{
		ID:                        domain.StackTemplateID("stack_template_123"),
		TenantID:                  domain.TenantID("tenant_123"),
		SourceTemplateID:          domain.SourceTemplateID("source_template_vpc"),
		DesiredTemplateRevisionID: domain.TemplateRevisionID("template_rev_1"),
		DesiredConfigJSON:         json.RawMessage(`{"region":"us-east-1"}`),
		Lifecycle:                 domain.StackTemplateActive,
	}
	deps.templates.template = domain.TemplateRevision{
		ID:               domain.TemplateRevisionID("template_rev_2"),
		TenantID:         domain.TenantID("tenant_123"),
		SourceTemplateID: domain.SourceTemplateID("source_template_vpc"),
		Status:           domain.TemplateRevisionActive,
	}
	deps.templates.variables = []domain.TemplateVariable{
		{Name: "region", Required: true},
		{Name: "cidr_block", Required: true},
	}
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(
		http.MethodPost,
		"/v1/tenants/tenant_123/stack-templates/stack_template_123/upgrade",
		strings.NewReader(`{"target_template_revision_id":"template_rev_2"}`),
	)

	server.ServeHTTP(response, request)

	// region carries forward from the current config; cidr_block is new and
	// required, so it is the one the form has to mark.
	assertValidationFailed(t, response, "#/config/cidr_block required")
}

func TestUpgradeStackTemplateMapsSourceMismatchToValidationFailed(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.stackTemplates.stackTemplate = domain.StackTemplate{
		ID:               domain.StackTemplateID("stack_template_123"),
		TenantID:         domain.TenantID("tenant_123"),
		SourceTemplateID: domain.SourceTemplateID("source_template_vpc"),
		Lifecycle:        domain.StackTemplateActive,
	}
	deps.templates.template = domain.TemplateRevision{
		ID:               domain.TemplateRevisionID("template_rev_2"),
		TenantID:         domain.TenantID("tenant_123"),
		SourceTemplateID: domain.SourceTemplateID("source_template_db"),
		Status:           domain.TemplateRevisionActive,
	}
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(
		http.MethodPost,
		"/v1/tenants/tenant_123/stack-templates/stack_template_123/upgrade",
		strings.NewReader(`{"target_template_revision_id":"template_rev_2"}`),
	)

	server.ServeHTTP(response, request)

	// The caller fixes this by picking another revision, so it is the
	// field's problem, not the template's state.
	assertValidationFailed(t, response, "#/target_template_revision_id different_template")
}

func TestStackTemplateEditRoutesHideMissingStackTemplateAsNotFound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{
			name:   "config edit",
			method: http.MethodPatch,
			path:   "/v1/tenants/tenant_123/stack-templates/missing_stack_template/config",
			body:   `{"config":{}}`,
		},
		{
			name:   "upgrade",
			method: http.MethodPost,
			path:   "/v1/tenants/tenant_123/stack-templates/missing_stack_template/upgrade",
			body:   `{"target_template_revision_id":"template_rev_2"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			deps := newAPITestDependencies(t)
			deps.stackTemplates.getErr = app.ErrNotFound
			server := NewServer(deps.service(), configuredTenantID)
			response := httptest.NewRecorder()
			request := authenticatedRequest(tt.method, tt.path, strings.NewReader(tt.body))

			server.ServeHTTP(response, request)

			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusNotFound, response.Body.String())
			}
		})
	}
}

func TestGetTemplateRegistrationReturnsRegistration(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t).withPlatformTier("admin")
	deps.registrations.registration = domain.TemplateRegistration{
		ID:       domain.TemplateRegistrationID("template_registration_123"),
		TenantID: domain.TenantID("tenant_123"),
		Status:   domain.TemplateRegistrationCompleted,
	}
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/template-registrations/template_registration_123", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if deps.registrations.gotGetTenantID != domain.TenantID("tenant_123") {
		t.Fatalf("tenant id = %q, want tenant_123", deps.registrations.gotGetTenantID)
	}

	var body domain.TemplateRegistration
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.ID != domain.TemplateRegistrationID("template_registration_123") {
		t.Fatalf("id = %q, want template_registration_123", body.ID)
	}
}

func TestListTemplateRevisionsReturnsTenantTemplateRevisions(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	deps := newAPITestDependencies(t).withPlatformTier("admin")
	deps.templates.templates = []domain.TemplateRevision{
		{
			ID:                domain.TemplateRevisionID("template_123"),
			TenantID:          domain.TenantID("tenant_123"),
			RepoOwner:         "acme",
			RepoName:          "infra-templates",
			SourceRef:         "main",
			ResolvedCommitSHA: "abc123",
			RootPath:          ".",
			Name:              "infra-templates",
			Tags:              []string{"aws"},
			Status:            domain.TemplateRevisionActive,
			CreatedAt:         createdAt,
		},
	}
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/template-revisions", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if deps.templates.gotListTenantID != domain.TenantID("tenant_123") {
		t.Fatalf("tenant list lookup = %q, want tenant_123", deps.templates.gotListTenantID)
	}

	var body []domain.TemplateRevision
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body) != 1 {
		t.Fatalf("len(body) = %d, want 1", len(body))
	}
	if body[0].ID != domain.TemplateRevisionID("template_123") || body[0].Status != domain.TemplateRevisionActive {
		t.Fatalf("template revision response = %#v", body[0])
	}
}

func TestListTemplateRunsReturnsRunsForStackTemplate(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	deps := newAPITestDependencies(t)
	deps.templateRuns.list = []domain.TemplateRun{
		{
			ID:              domain.TemplateRunID("run_apply_1"),
			TenantID:        domain.TenantID("tenant_123"),
			StackTemplateID: domain.StackTemplateID("stack_template_123"),
			Operation:       domain.OperationPlan,
			Status:          domain.TemplateRunWaitingApproval,
			TriggerActor:    domain.UserID("user_456"),
			CreatedAt:       startedAt,
		},
	}
	deps.stackTemplates.stackTemplate = domain.StackTemplate{
		ID:       domain.StackTemplateID("stack_template_123"),
		TenantID: domain.TenantID("tenant_123"),
		StackID:  domain.StackID("stack_123"),
	}
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/stack-templates/stack_template_123/runs", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if deps.templateRuns.gotListStackTemplateID != domain.StackTemplateID("stack_template_123") {
		t.Fatalf("stack template lookup = %q, want stack_template_123", deps.templateRuns.gotListStackTemplateID)
	}

	var body []domain.TemplateRun
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body) != 1 || body[0].ID != domain.TemplateRunID("run_apply_1") || body[0].Status != domain.TemplateRunWaitingApproval {
		t.Fatalf("runs = %#v", body)
	}
}

func TestGetTemplateRevisionVariablesReturnsVariables(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t).withPlatformTier("admin")
	deps.templates.variables = []domain.TemplateVariable{
		{
			TemplateRevisionID: domain.TemplateRevisionID("template_123"),
			Name:               "region",
			TypeExpression:     "string",
			Required:           true,
		},
	}
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/template-revisions/template_123/variables", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if deps.templates.gotVariablesTemplateRevisionID != domain.TemplateRevisionID("template_123") {
		t.Fatalf("template revision id = %q, want template_123", deps.templates.gotVariablesTemplateRevisionID)
	}

	var body []domain.TemplateVariable
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body) != 1 {
		t.Fatalf("len(body) = %d, want 1", len(body))
	}
	if body[0].Name != "region" {
		t.Fatalf("variable name = %q, want region", body[0].Name)
	}
}

func TestGetTemplateRunReturnsRun(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.templateRuns.run = domain.TemplateRun{
		ID:              domain.TemplateRunID("run_123"),
		TenantID:        domain.TenantID("tenant_123"),
		StackTemplateID: domain.StackTemplateID("stack_template_123"),
		Operation:       domain.OperationPlan,
		Status:          domain.TemplateRunCompleted,
	}
	deps.templateRuns.executions = []domain.TemplateRunWorkflowExecution{{
		Phase: domain.RunPhasePlan, WorkflowID: "template-run/tenant_123/run_123", Status: domain.TemplateRunExecutionSucceeded,
		Steps: []domain.TemplateRunStepExecution{{Step: domain.TemplateRunStepPlanning, Status: domain.TemplateRunExecutionSucceeded}},
	}}
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/template-runs/run_123", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if deps.templateRuns.gotGetTenantID != domain.TenantID("tenant_123") {
		t.Fatalf("tenant id = %q, want tenant_123", deps.templateRuns.gotGetTenantID)
	}
	if deps.templateRuns.gotGetRunID != domain.TemplateRunID("run_123") {
		t.Fatalf("run id = %q, want run_123", deps.templateRuns.gotGetRunID)
	}

	var body domain.TemplateRun
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.ID != domain.TemplateRunID("run_123") {
		t.Fatalf("id = %q, want run_123", body.ID)
	}
	if body.Status != domain.TemplateRunCompleted {
		t.Fatalf("status = %q, want completed", body.Status)
	}
	if len(body.Executions) != 1 || len(body.Executions[0].Steps) != 1 || body.Executions[0].Steps[0].Step != domain.TemplateRunStepPlanning {
		t.Fatalf("executions = %#v, want the plan with its planning step", body.Executions)
	}
}

// An unfinished run leaves its end times out rather than sending Go's zero
// time, which a client would otherwise have to recognise as "not yet".
func TestGetTemplateRunOmitsEndTimesUntilTheyHappen(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.templateRuns.run = domain.TemplateRun{
		ID:              domain.TemplateRunID("run_123"),
		TenantID:        domain.TenantID("tenant_123"),
		StackTemplateID: domain.StackTemplateID("stack_template_123"),
		Operation:       domain.OperationPlan,
		Status:          domain.TemplateRunRunning,
	}
	deps.templateRuns.executions = []domain.TemplateRunWorkflowExecution{{
		Phase: domain.RunPhasePlan, Status: domain.TemplateRunExecutionRunning,
		StartedAt: time.Date(2026, 7, 3, 11, 30, 0, 0, time.UTC),
		Steps:     []domain.TemplateRunStepExecution{{Step: domain.TemplateRunStepPlanning, Status: domain.TemplateRunExecutionRunning}},
	}}
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/template-runs/run_123", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	var body struct {
		CompletedAt *string `json:"completed_at"`
		Executions  []struct {
			StartedAt  *string `json:"started_at"`
			FinishedAt *string `json:"finished_at"`
			Steps      []struct {
				FinishedAt *string `json:"finished_at"`
			} `json:"steps"`
		} `json:"executions"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.CompletedAt != nil {
		t.Fatalf("completed_at = %q, want it omitted", *body.CompletedAt)
	}
	execution := body.Executions[0]
	if execution.StartedAt == nil {
		t.Fatal("started_at omitted, want it sent")
	}
	if execution.FinishedAt != nil || execution.Steps[0].FinishedAt != nil {
		t.Fatalf("finished_at sent for a running execution or step: %+v", execution)
	}
}

// A single read always sends executions, as [] for a run no workflow has
// carried yet; the run list never sends them.
func TestTemplateRunExecutionsArePresentOnlyOnASingleRead(t *testing.T) {
	t.Parallel()

	run := domain.TemplateRun{
		ID:              domain.TemplateRunID("run_123"),
		TenantID:        domain.TenantID("tenant_123"),
		StackTemplateID: domain.StackTemplateID("stack_template_123"),
		Operation:       domain.OperationPlan,
		Status:          domain.TemplateRunQueued,
	}
	deps := newAPITestDependencies(t)
	deps.templateRuns.run = run
	deps.templateRuns.executions = []domain.TemplateRunWorkflowExecution{}
	deps.templateRuns.list = []domain.TemplateRun{run}
	deps.stackTemplates.stackTemplate = domain.StackTemplate{
		ID:       domain.StackTemplateID("stack_template_123"),
		TenantID: domain.TenantID("tenant_123"),
		StackID:  domain.StackID("stack_123"),
	}
	server := NewServer(deps.service(), configuredTenantID)

	response := httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/template-runs/run_123", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	var single map[string]json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&single); err != nil {
		t.Fatalf("decode run: %v", err)
	}
	if got := string(single["executions"]); got != "[]" {
		t.Fatalf("executions = %q, want []", got)
	}

	response = httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/stack-templates/stack_template_123/runs", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	var listed []map[string]json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&listed); err != nil {
		t.Fatalf("decode runs: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("runs = %d, want 1", len(listed))
	}
	if got, ok := listed[0]["executions"]; ok {
		t.Fatalf("listed run has executions %s, want none", got)
	}
}

func TestGetTemplateRunLogReturnsPlainText(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.templateRuns.run = domain.TemplateRun{
		ID:       domain.TemplateRunID("run_123"),
		TenantID: domain.TenantID("tenant_123"),
	}
	deps.logs.content = []byte("terraform plan output\n")
	deps.logMetadata.log = domain.TemplateRunLog{
		TenantID:  domain.TenantID("tenant_123"),
		RunID:     domain.TemplateRunID("run_123"),
		Phase:     "plan",
		ObjectKey: "tenants/tenant_123/runs/run_123/logs/plan.log",
	}
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/template-runs/run_123/logs/plan", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/plain; charset=utf-8", contentType)
	}
	if response.Body.String() != "terraform plan output\n" {
		t.Fatalf("body = %q, want terraform plan output", response.Body.String())
	}
	if deps.logs.gotPhase != "plan" {
		t.Fatalf("phase = %q, want plan", deps.logs.gotPhase)
	}
}

func TestListTemplateRunLogsReturnsMetadata(t *testing.T) {
	t.Parallel()

	uploadedAt := time.Date(2026, 7, 6, 10, 15, 0, 0, time.UTC)
	deps := newAPITestDependencies(t)
	deps.templateRuns.run = domain.TemplateRun{
		ID:       domain.TemplateRunID("run_123"),
		TenantID: domain.TenantID("tenant_123"),
	}
	deps.logMetadata.logs = []domain.TemplateRunLog{
		{
			TenantID:    domain.TenantID("tenant_123"),
			RunID:       domain.TemplateRunID("run_123"),
			Phase:       "init",
			ObjectKey:   "tenants/tenant_123/runs/run_123/logs/init.log",
			ContentType: "text/plain; charset=utf-8",
			SizeBytes:   12,
			UploadedAt:  uploadedAt,
		},
	}
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/template-runs/run_123/logs", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if deps.logMetadata.gotListRunID != domain.TemplateRunID("run_123") {
		t.Fatalf("metadata run ID = %q, want run_123", deps.logMetadata.gotListRunID)
	}

	var body []domain.TemplateRunLog
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body) != 1 {
		t.Fatalf("len(body) = %d, want 1", len(body))
	}
	if body[0].Phase != "init" {
		t.Fatalf("phase = %q, want init", body[0].Phase)
	}
	if body[0].SizeBytes != 12 {
		t.Fatalf("size_bytes = %d, want 12", body[0].SizeBytes)
	}
}

func TestListTemplateRunLogsReturnsEmptyArray(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.templateRuns.run = domain.TemplateRun{
		ID:       domain.TemplateRunID("run_123"),
		TenantID: domain.TenantID("tenant_123"),
	}
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/template-runs/run_123/logs", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if strings.TrimSpace(response.Body.String()) != "[]" {
		t.Fatalf("body = %q, want []", response.Body.String())
	}
}

// Any well-formed name is looked up, since the workflow decides which logs a
// run has; only a name that could not be one is refused outright.
// A log name of the wrong shape names a log that can't exist.
func TestGetTemplateRunLogMapsInvalidPhaseToNotFound(t *testing.T) {
	t.Parallel()

	server := NewServer(newAPITestDependencies(t).service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/v1/tenants/tenant_123/template-runs/run_123/logs/plan.log",
		nil,
	)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestGetTemplateRunMapsMissingRunToNotFound(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.templateRuns.getErr = app.ErrNotFound
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/template-runs/run_123", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestGetTemplateRunLogMapsMissingLogToNotFound(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.templateRuns.run = domain.TemplateRun{
		ID:       domain.TemplateRunID("run_123"),
		TenantID: domain.TenantID("tenant_123"),
	}
	deps.logs.err = app.ErrNotFound
	deps.logMetadata.log = domain.TemplateRunLog{
		TenantID:  domain.TenantID("tenant_123"),
		RunID:     domain.TemplateRunID("run_123"),
		Phase:     "plan",
		ObjectKey: "tenants/tenant_123/runs/run_123/logs/plan.log",
	}
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/template-runs/run_123/logs/plan", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestApproveRunCallsService(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(
		http.MethodPost,
		"/v1/tenants/tenant_123/template-runs/run_123/approval",
		strings.NewReader(`{}`),
	)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusNoContent, response.Body.String())
	}
	if deps.templateRuns.approval.TenantID != domain.TenantID("tenant_123") {
		t.Fatalf("tenant id = %q", deps.templateRuns.approval.TenantID)
	}
	if deps.templateRuns.approval.RunID != domain.TemplateRunID("run_123") {
		t.Fatalf("run id = %q", deps.templateRuns.approval.RunID)
	}
	if deps.templateRuns.approval.ApprovedBy != domain.UserID(apiOIDCSubject) {
		t.Fatalf("approved by = %q, want %q", deps.templateRuns.approval.ApprovedBy, apiOIDCSubject)
	}
	if len(deps.work.requests) != 1 || deps.work.requests[0].Kind != app.KindStartTemplateApply {
		t.Fatalf("queued requests = %#v, want one start_template_apply request", deps.work.requests)
	}
}

// Approving carries no data, so a client may send no body at all; a body it
// does send is still held to the schema.
func TestApproveRunTakesAnEmptyBodyButNotAMalformedOne(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, authenticatedRequest(http.MethodPost, "/v1/tenants/tenant_123/template-runs/run_123/approval", nil))

	if response.Code != http.StatusNoContent {
		t.Fatalf("empty body: status = %d, want %d; body = %s", response.Code, http.StatusNoContent, response.Body.String())
	}
	if deps.templateRuns.approval.RunID != domain.TemplateRunID("run_123") {
		t.Fatalf("empty body: approval was not recorded")
	}

	for _, requestBody := range []string{`{"unexpected":true}`, `[]`, `{`} {
		deps := newAPITestDependencies(t)
		server := NewServer(deps.service(), configuredTenantID)
		response := httptest.NewRecorder()

		server.ServeHTTP(response, authenticatedRequest(http.MethodPost, "/v1/tenants/tenant_123/template-runs/run_123/approval", strings.NewReader(requestBody)))

		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want %d", requestBody, response.Code, http.StatusBadRequest)
		}
		if deps.templateRuns.approval.RunID != "" {
			t.Fatalf("%s: approval recorded, want none", requestBody)
		}
	}
}

func TestApproveRunAllowsSelfApproval(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.templateRuns.run = domain.TemplateRun{
		ID:              "run_123",
		TenantID:        "tenant_123",
		StackTemplateID: "stack_template_123",
		Status:          domain.TemplateRunWaitingApproval,
		TriggerActor:    domain.UserID(apiOIDCSubject),
	}
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(
		http.MethodPost,
		"/v1/tenants/tenant_123/template-runs/run_123/approval",
		strings.NewReader(`{}`),
	)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusNoContent, response.Body.String())
	}
	if deps.templateRuns.approval.RunID == "" {
		t.Fatalf("approval was not recorded, want approval")
	}
	if len(deps.work.requests) != 1 || deps.work.requests[0].Kind != app.KindStartTemplateApply {
		t.Fatalf("queued requests = %#v, want one start_template_apply request", deps.work.requests)
	}
}

func TestDiscardRunCallsService(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(
		http.MethodPost,
		"/v1/tenants/tenant_123/template-runs/run_123/discard",
		strings.NewReader(`{"reason":"testing"}`),
	)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusNoContent, response.Body.String())
	}
	if deps.templateRuns.discarded.TenantID != domain.TenantID("tenant_123") {
		t.Fatalf("tenant id = %q", deps.templateRuns.discarded.TenantID)
	}
	if deps.templateRuns.discarded.RunID != domain.TemplateRunID("run_123") {
		t.Fatalf("run id = %q", deps.templateRuns.discarded.RunID)
	}
	if deps.templateRuns.discarded.RequestedBy != domain.UserID(apiOIDCSubject) {
		t.Fatalf("requested by = %q, want %q", deps.templateRuns.discarded.RequestedBy, apiOIDCSubject)
	}
	if deps.templateRuns.discarded.Reason != "testing" {
		t.Fatalf("reason = %q", deps.templateRuns.discarded.Reason)
	}
	if len(deps.work.requests) != 0 {
		t.Fatalf("queued requests = %#v, want none", deps.work.requests)
	}
}

func TestDiscardRunTakesAnEmptyBody(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, authenticatedRequest(http.MethodPost, "/v1/tenants/tenant_123/template-runs/run_123/discard", nil))

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusNoContent, response.Body.String())
	}
	if deps.templateRuns.discarded.RunID != domain.TemplateRunID("run_123") || deps.templateRuns.discarded.Reason != "" {
		t.Fatalf("discard = %+v, want run_123 with no reason", deps.templateRuns.discarded)
	}
}

func TestRunDecisionRequestsRejectTopLevelNull(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		path            string
		assertNoEffects func(*testing.T, *apiTestDependencies)
	}{
		{
			name: "approval",
			path: "/v1/tenants/tenant_123/template-runs/run_123/approval",
			assertNoEffects: func(t *testing.T, deps *apiTestDependencies) {
				t.Helper()
				if deps.templateRuns.approval.RunID != "" {
					t.Errorf("approval run ID = %q, want no approval", deps.templateRuns.approval.RunID)
				}
				if deps.workflows.approvalRunID != "" {
					t.Errorf("workflow approval run ID = %q, want no signal", deps.workflows.approvalRunID)
				}
			},
		},
		{
			name: "discard",
			path: "/v1/tenants/tenant_123/template-runs/run_123/discard",
			assertNoEffects: func(t *testing.T, deps *apiTestDependencies) {
				t.Helper()
				if deps.templateRuns.discarded.RunID != "" {
					t.Errorf("discarded run ID = %q, want no discard", deps.templateRuns.discarded.RunID)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			deps := newAPITestDependencies(t)
			server := NewServer(deps.service(), configuredTenantID)
			response := httptest.NewRecorder()
			request := authenticatedRequest(http.MethodPost, test.path, strings.NewReader(`null`))

			server.ServeHTTP(response, request)

			if response.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d; body = %s", response.Code, http.StatusBadRequest, response.Body.String())
			}
			test.assertNoEffects(t, deps)
		})
	}
}

func TestRunDecisionConflictErrorsReturnConflict(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		body       string
		configure  func(*apiTestDependencies)
		statusCode int
	}{
		{
			name: "approval",
			path: "/v1/tenants/tenant_123/template-runs/run_123/approval",
			body: `{}`,
			configure: func(deps *apiTestDependencies) {
				deps.templateRuns.approvalErr = app.ErrRunNotApprovable
			},
			statusCode: http.StatusConflict,
		},
		{
			name: "discard",
			path: "/v1/tenants/tenant_123/template-runs/run_123/discard",
			body: `{}`,
			configure: func(deps *apiTestDependencies) {
				deps.templateRuns.notDiscardable = true
			},
			statusCode: http.StatusConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			deps := newAPITestDependencies(t)
			tt.configure(deps)
			server := NewServer(deps.service(), configuredTenantID)
			response := httptest.NewRecorder()
			request := authenticatedRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))

			server.ServeHTTP(response, request)

			if response.Code != tt.statusCode {
				t.Fatalf("status = %d, want %d", response.Code, tt.statusCode)
			}
		})
	}
}

func TestMutationRequestsRejectIdentityOverrides(t *testing.T) {
	assertNoStackCreation := func(t *testing.T, deps *apiTestDependencies) {
		t.Helper()
		if deps.stacks.created.ID != "" {
			t.Errorf("created stack ID = %q, want no mutation", deps.stacks.created.ID)
		}
	}
	assertNoRegistration := func(t *testing.T, deps *apiTestDependencies) {
		t.Helper()
		if deps.registrations.created.ID != "" {
			t.Errorf("created registration ID = %q, want no mutation", deps.registrations.created.ID)
		}
		if deps.workflows.syncInput.RegistrationID != "" {
			t.Errorf("workflow registration ID = %q, want no workflow start", deps.workflows.syncInput.RegistrationID)
		}
	}
	assertNoRun := func(t *testing.T, deps *apiTestDependencies) {
		t.Helper()
		if deps.templateRuns.created.ID != "" {
			t.Errorf("created run ID = %q, want no mutation", deps.templateRuns.created.ID)
		}
		if deps.workflows.input.RunID != "" {
			t.Errorf("workflow run ID = %q, want no workflow start", deps.workflows.input.RunID)
		}
	}
	assertNoApproval := func(t *testing.T, deps *apiTestDependencies) {
		t.Helper()
		if deps.templateRuns.approval.RunID != "" {
			t.Errorf("approval run ID = %q, want no mutation", deps.templateRuns.approval.RunID)
		}
		if deps.workflows.approvalRunID != "" {
			t.Errorf("workflow approval run ID = %q, want no signal", deps.workflows.approvalRunID)
		}
	}

	tests := []struct {
		name            string
		path            string
		body            string
		assertNoEffects func(*testing.T, *apiTestDependencies)
	}{
		{name: "actor", path: "/v1/tenants/tenant_123/stacks", body: `{"name":"Acme","actor":"spoofed"}`, assertNoEffects: assertNoStackCreation},
		{name: "requested by", path: "/v1/tenants/tenant_123/template-revisions", body: `{"repo_owner":"acme","repo_name":"infra","source_ref":"main","root_path":".","requested_by":"spoofed"}`, assertNoEffects: assertNoRegistration},
		{name: "trigger actor", path: "/v1/tenants/tenant_123/stack-templates/stack_template_123/runs", body: `{"operation":"plan","trigger_actor":"spoofed"}`, assertNoEffects: assertNoRun},
		{name: "approved by", path: "/v1/tenants/tenant_123/template-runs/run_123/approval", body: `{"approved_by":"spoofed"}`, assertNoEffects: assertNoApproval},
		{name: "created by", path: "/v1/tenants/tenant_123/stacks", body: `{"name":"Acme","created_by":"spoofed"}`, assertNoEffects: assertNoStackCreation},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			deps := newAPITestDependencies(t)
			server := NewServer(deps.service(), configuredTenantID)
			response := httptest.NewRecorder()
			request := authenticatedRequest(http.MethodPost, test.path, strings.NewReader(test.body))

			server.ServeHTTP(response, request)

			if response.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d; body = %s", response.Code, http.StatusBadRequest, response.Body.String())
			}
			test.assertNoEffects(t, deps)
		})
	}
}

func TestDecodeRequestBodyRejectsNonObjectAndMultipleValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "array", body: `[]`},
		{name: "string", body: `"value"`},
		{name: "number", body: `42`},
		{name: "boolean", body: `true`},
		{name: "second value", body: `{"name":"accepted"} {"name":"extra"}`},
		{name: "unknown field", body: `{"surname":"x"}`},
		{name: "wrong type", body: `{"name":1}`},
		{name: "truncated", body: `{"name":`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(test.body))
			var destination struct {
				Name string `json:"name"`
			}

			if decodeRequestBody(response, request, &destination) {
				t.Fatal("decodeRequestBody accepted request, want rejection before service effect")
			}
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusBadRequest, response.Body.String())
			}
			// Unparseable is a 400 of its own type, never the 422 a client
			// shows on a form.
			if problem := decodeProblem(t, response); problem.Type != problemTypeURI("malformed-body") {
				t.Fatalf("type = %q, want malformed-body", problem.Type)
			}
		})
	}
}

func TestDecodeRequestBodyRejectsOversizedBody(t *testing.T) {
	t.Parallel()

	padding := strings.Repeat("a", maxRequestBodyBytes)
	tests := []struct {
		name string
		body string
	}{
		// The cap must hold while the object is still being read, not only
		// once it has been buffered.
		{name: "one oversized object", body: `{"name":"` + padding + `"}`},
		// A valid object under the cap followed by enough trailing bytes to
		// cross it is still an oversized body, not a second JSON value.
		{name: "trailing bytes past the cap", body: `{"name":"accepted"}` + strings.Repeat(" ", maxRequestBodyBytes)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(test.body))
			var destination struct {
				Name string `json:"name"`
			}

			if decodeRequestBody(response, request, &destination) {
				t.Fatal("decodeRequestBody accepted request, want rejection before service effect")
			}
			if response.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
			}
			body := decodeProblem(t, response)
			if body.Type != blankType {
				t.Fatalf("type = %q, want %q", body.Type, blankType)
			}
		})
	}
}

func TestDecodeRequestBodyAcceptsBodyAtTheCap(t *testing.T) {
	t.Parallel()

	prefix, suffix := `{"name":"`, `"}`
	padding := strings.Repeat("a", maxRequestBodyBytes-len(prefix)-len(suffix))
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(prefix+padding+suffix))
	var destination struct {
		Name string `json:"name"`
	}

	if !decodeRequestBody(response, request, &destination) {
		t.Fatalf("decodeRequestBody rejected a body of exactly %d bytes: status %d, body %s", maxRequestBodyBytes, response.Code, response.Body.String())
	}
	if len(destination.Name) != len(padding) {
		t.Fatalf("len(Name) = %d, want %d", len(destination.Name), len(padding))
	}
}

func TestMutationRequestsRejectMissingPrincipal(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/tenants/tenant_123/stacks",
		strings.NewReader(`{"name":"Acme Prod","tags":{"env":"prod"},"default_credential_ids":["credential_123"]}`),
	)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusUnauthorized, response.Body.String())
	}
	body := decodeProblem(t, response)
	if body.Type != blankType {
		t.Fatalf("type = %q, want %q", body.Type, blankType)
	}
	if deps.stacks.created.ID != "" {
		t.Fatalf("created stack ID = %q, want no mutation", deps.stacks.created.ID)
	}
}

func TestUnknownRouteReturnsNotFound(t *testing.T) {
	t.Parallel()

	server := NewServer(newAPITestDependencies(t).service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/nope", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestTemplateCatalogRoutesRejectOrdinaryUser(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "register", method: http.MethodPost, path: "/v1/tenants/tenant_123/template-revisions", body: `{"repo_owner":"acme","repo_name":"infra","source_ref":"main","root_path":"."}`},
		{name: "list revisions", method: http.MethodGet, path: "/v1/tenants/tenant_123/template-revisions"},
		{name: "registration status", method: http.MethodGet, path: "/v1/tenants/tenant_123/template-registrations/registration_123"},
		{name: "revision variables", method: http.MethodGet, path: "/v1/tenants/tenant_123/template-revisions/revision_123/variables"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			deps := newBareAPITestDependencies(t)
			server := NewServer(deps.service(), configuredTenantID)
			response := httptest.NewRecorder()
			request := ordinaryAuthenticatedRequest(test.method, test.path, strings.NewReader(test.body))

			server.ServeHTTP(response, request)

			if response.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusForbidden, response.Body.String())
			}
			if deps.registrations.created.ID != "" {
				t.Fatalf("registration = %#v, want no mutation", deps.registrations.created)
			}
		})
	}
}

func TestTemplateCatalogRoutesAllowGlobalRoles(t *testing.T) {
	t.Parallel()

	routes := []struct {
		name   string
		method string
		path   string
		body   string
		status int
	}{
		{name: "register", method: http.MethodPost, path: "/v1/tenants/tenant_123/template-revisions", body: `{"repo_owner":"acme","repo_name":"infra","source_ref":"main","root_path":"."}`, status: http.StatusAccepted},
		{name: "list revisions", method: http.MethodGet, path: "/v1/tenants/tenant_123/template-revisions", status: http.StatusOK},
		{name: "registration status", method: http.MethodGet, path: "/v1/tenants/tenant_123/template-registrations/registration_123", status: http.StatusOK},
		{name: "revision variables", method: http.MethodGet, path: "/v1/tenants/tenant_123/template-revisions/revision_123/variables", status: http.StatusOK},
	}

	for _, tier := range []string{"admin", "editor"} {
		for _, route := range routes {
			t.Run(tier+" "+route.name, func(t *testing.T) {
				t.Parallel()
				deps := newAPITestDependencies(t).withPlatformTier(tier)
				server := NewServer(deps.service(), configuredTenantID)
				response := httptest.NewRecorder()
				request := authenticatedRequest(route.method, route.path, strings.NewReader(route.body))

				server.ServeHTTP(response, request)

				if response.Code != route.status {
					t.Fatalf("status = %d, want %d; body = %s", response.Code, route.status, response.Body.String())
				}
			})
		}
	}
}

func TestCreateStackAllowsGlobalRoles(t *testing.T) {
	t.Parallel()

	for _, tier := range []string{"admin", "editor"} {
		t.Run(tier, func(t *testing.T) {
			t.Parallel()
			deps := newAPITestDependencies(t).withPlatformTier(tier)
			server := NewServer(deps.service(), configuredTenantID)
			response := httptest.NewRecorder()
			request := authenticatedRequest(http.MethodPost, "/v1/tenants/tenant_123/stacks", strings.NewReader(`{"name":"Acme"}`))

			server.ServeHTTP(response, request)

			if response.Code != http.StatusCreated {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusCreated, response.Body.String())
			}
		})
	}
}

func TestStackRoleRoutesUseInheritedPermissions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		role       authorization.Relation
		method     string
		path       string
		body       string
		status     int
		permission authorization.Relation
	}{
		{name: "viewer lists stacks", role: authorization.RelationViewer, method: http.MethodGet, path: "/v1/tenants/tenant_123/stacks", status: http.StatusOK, permission: authorization.RelationCanView},
		{name: "viewer reads stack", role: authorization.RelationViewer, method: http.MethodGet, path: "/v1/tenants/tenant_123/stacks/stack_123", status: http.StatusOK, permission: authorization.RelationCanView},
		{name: "operator installs template", role: authorization.RelationOperator, method: http.MethodPost, path: "/v1/tenants/tenant_123/stacks/stack_123/templates", body: `{"template_revision_id":"revision_123","config":{}}`, status: http.StatusCreated, permission: authorization.RelationCanOperate},
		{name: "owner operates config", role: authorization.RelationOwner, method: http.MethodPatch, path: "/v1/tenants/tenant_123/stack-templates/stack_template_123/config", body: `{"config":{}}`, status: http.StatusOK, permission: authorization.RelationCanOperate},
		{name: "operator upgrades template", role: authorization.RelationOperator, method: http.MethodPost, path: "/v1/tenants/tenant_123/stack-templates/stack_template_123/upgrade", body: `{"target_template_revision_id":"revision_123"}`, status: http.StatusOK, permission: authorization.RelationCanOperate},
		{name: "operator starts run", role: authorization.RelationOperator, method: http.MethodPost, path: "/v1/tenants/tenant_123/stack-templates/stack_template_123/runs", body: `{"operation":"plan"}`, status: http.StatusCreated, permission: authorization.RelationCanOperate},
		{name: "approver approves run", role: authorization.RelationApprover, method: http.MethodPost, path: "/v1/tenants/tenant_123/template-runs/run_123/approval", body: `{}`, status: http.StatusNoContent, permission: authorization.RelationCanApprove},
		{name: "owner discards run", role: authorization.RelationOwner, method: http.MethodPost, path: "/v1/tenants/tenant_123/template-runs/run_123/discard", body: `{}`, status: http.StatusNoContent, permission: authorization.RelationCanOperate},
		{name: "viewer reads run", role: authorization.RelationViewer, method: http.MethodGet, path: "/v1/tenants/tenant_123/template-runs/run_123", status: http.StatusOK, permission: authorization.RelationCanView},
		{name: "approver lists run logs", role: authorization.RelationApprover, method: http.MethodGet, path: "/v1/tenants/tenant_123/template-runs/run_123/logs", status: http.StatusOK, permission: authorization.RelationCanView},
		{name: "viewer reads run log", role: authorization.RelationViewer, method: http.MethodGet, path: "/v1/tenants/tenant_123/template-runs/run_123/logs/plan", status: http.StatusOK, permission: authorization.RelationCanView},
		{name: "viewer lists run history", role: authorization.RelationViewer, method: http.MethodGet, path: "/v1/tenants/tenant_123/stack-templates/stack_template_123/runs", status: http.StatusOK, permission: authorization.RelationCanView},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			deps := newPermissionMatrixDependencies(t).withStackRole(test.role)
			server := NewServer(deps.service(), configuredTenantID)
			response := httptest.NewRecorder()
			request := ordinaryAuthenticatedRequest(test.method, test.path, strings.NewReader(test.body))

			server.ServeHTTP(response, request)

			if response.Code != test.status {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.status, response.Body.String())
			}
			// The status is the assertion. The role is a real tuple and the
			// answer comes from the real model, so a route that asked for the
			// wrong permission would allow or refuse the wrong roles here --
			// which is what the allowed and refused rows together pin. Recording
			// which relation was asked for added nothing the outcome does not
			// already prove, and it could only ever confirm the test's own
			// expectation back to itself.
		})
	}
}

func TestStackRoleRoutesDenyInsufficientRoles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		role       authorization.Relation
		method     string
		path       string
		body       string
		status     int
		permission authorization.Relation
	}{
		{name: "unassigned list is empty", method: http.MethodGet, path: "/v1/tenants/tenant_123/stacks", status: http.StatusOK, permission: authorization.RelationCanView},
		{name: "unassigned cannot read stack", method: http.MethodGet, path: "/v1/tenants/tenant_123/stacks/stack_123", status: http.StatusNotFound, permission: authorization.RelationCanView},
		{name: "unassigned cannot install template", method: http.MethodPost, path: "/v1/tenants/tenant_123/stacks/stack_123/templates", body: `{"template_revision_id":"revision_123","config":{}}`, status: http.StatusNotFound, permission: authorization.RelationCanOperate},
		{name: "unassigned cannot approve run", method: http.MethodPost, path: "/v1/tenants/tenant_123/template-runs/run_123/approval", body: `{}`, status: http.StatusNotFound, permission: authorization.RelationCanApprove},
		{name: "unassigned cannot assign role", method: http.MethodPost, path: "/v1/tenants/tenant_123/stacks/stack_123/grants", body: `{"user_sub":"user-2","role":"viewer"}`, status: http.StatusNotFound, permission: authorization.RelationCanManageAccess},
		{name: "viewer cannot install template", role: authorization.RelationViewer, method: http.MethodPost, path: "/v1/tenants/tenant_123/stacks/stack_123/templates", body: `{"template_revision_id":"revision_123","config":{}}`, status: http.StatusForbidden, permission: authorization.RelationCanOperate},
		{name: "approver cannot update config", role: authorization.RelationApprover, method: http.MethodPatch, path: "/v1/tenants/tenant_123/stack-templates/stack_template_123/config", body: `{"config":{}}`, status: http.StatusForbidden, permission: authorization.RelationCanOperate},
		{name: "viewer cannot upgrade template", role: authorization.RelationViewer, method: http.MethodPost, path: "/v1/tenants/tenant_123/stack-templates/stack_template_123/upgrade", body: `{"target_template_revision_id":"revision_123"}`, status: http.StatusForbidden, permission: authorization.RelationCanOperate},
		{name: "approver cannot start run", role: authorization.RelationApprover, method: http.MethodPost, path: "/v1/tenants/tenant_123/stack-templates/stack_template_123/runs", body: `{"operation":"plan"}`, status: http.StatusForbidden, permission: authorization.RelationCanOperate},
		{name: "operator cannot approve run", role: authorization.RelationOperator, method: http.MethodPost, path: "/v1/tenants/tenant_123/template-runs/run_123/approval", body: `{}`, status: http.StatusForbidden, permission: authorization.RelationCanApprove},
		{name: "approver cannot discard run", role: authorization.RelationApprover, method: http.MethodPost, path: "/v1/tenants/tenant_123/template-runs/run_123/discard", body: `{}`, status: http.StatusForbidden, permission: authorization.RelationCanOperate},
		{name: "unassigned cannot read run", method: http.MethodGet, path: "/v1/tenants/tenant_123/template-runs/run_123", status: http.StatusNotFound, permission: authorization.RelationCanView},
		{name: "unassigned cannot list logs", method: http.MethodGet, path: "/v1/tenants/tenant_123/template-runs/run_123/logs", status: http.StatusNotFound, permission: authorization.RelationCanView},
		{name: "unassigned cannot read log", method: http.MethodGet, path: "/v1/tenants/tenant_123/template-runs/run_123/logs/plan", status: http.StatusNotFound, permission: authorization.RelationCanView},
		{name: "unassigned cannot list run history", method: http.MethodGet, path: "/v1/tenants/tenant_123/stack-templates/stack_template_123/runs", status: http.StatusNotFound, permission: authorization.RelationCanView},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			deps := newPermissionMatrixDependencies(t).withStackRole(test.role)
			server := NewServer(deps.service(), configuredTenantID)
			response := httptest.NewRecorder()
			request := ordinaryAuthenticatedRequest(test.method, test.path, strings.NewReader(test.body))

			server.ServeHTTP(response, request)

			if response.Code != test.status {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.status, response.Body.String())
			}
			// The status is the assertion. The role is a real tuple and the
			// answer comes from the real model, so a route that asked for the
			// wrong permission would allow or refuse the wrong roles here --
			// which is what the allowed and refused rows together pin. Recording
			// which relation was asked for added nothing the outcome does not
			// already prove, and it could only ever confirm the test's own
			// expectation back to itself.
			if deps.stackTemplateInstaller.created.ID != "" || deps.templateRuns.created.ID != "" || deps.templateRuns.approval.RunID != "" || deps.templateRuns.discarded.RunID != "" {
				t.Fatal("denied mutation had side effects")
			}
			if len(deps.stackTemplates.gotConfigJSON) != 0 || deps.stackTemplates.gotDesiredTemplateRevisionID != "" || deps.workflows.approvalRunID != "" {
				t.Fatal("denied mutation updated state or signaled a workflow")
			}
			if test.name == "unassigned list is empty" {
				var stacks []stackResponse
				if err := json.NewDecoder(response.Body).Decode(&stacks); err != nil {
					t.Fatalf("decode stack list: %v", err)
				}
				if len(stacks) != 0 {
					t.Fatalf("stacks = %#v, want empty", stacks)
				}
			}
		})
	}
}

func TestStackListFiltersMixedDecisions(t *testing.T) {
	t.Parallel()

	deps := newBareAPITestDependencies(t)
	deps.stacks.list = []domain.Stack{
		{ID: "stack_allowed", TenantID: "tenant_123", CreatedAt: time.Unix(2, 0)},
		{ID: "stack_denied", TenantID: "tenant_123", CreatedAt: time.Unix(1, 0)},
	}
	deps.withGrants(mustAPIGrant(t, apiOIDCSubject, "stack_allowed", authorization.RelationViewer))
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := ordinaryAuthenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/stacks", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	var stacks []stackResponse
	if err := json.NewDecoder(response.Body).Decode(&stacks); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(stacks) != 1 || stacks[0].ID != "stack_allowed" {
		t.Fatalf("stacks = %#v, want only stack_allowed", stacks)
	}
}

func TestStackListLaterBatchFailureReturnsNoPartialResponse(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.stacks.list = make([]domain.Stack, 51)
	for i := range deps.stacks.list {
		deps.stacks.list[i] = domain.Stack{ID: domain.StackID(fmt.Sprintf("stack_%02d", 51-i)), TenantID: "tenant_123", CreatedAt: time.Unix(int64(100-i), 0)}
	}
	deps.authorizer = newFailingAPIAuthorization(t)
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := ordinaryAuthenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/stacks", nil)

	server.ServeHTTP(response, request)

	// 500, not 503: with OpenFGA embedded there is no separate authorization
	// service to report as unavailable. What matters is that the failure does
	// not render as an empty list, which would tell the caller they have access
	// to nothing rather than that the answer is unknown.
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusInternalServerError, response.Body.String())
	}
	if strings.Contains(response.Body.String(), `"stacks"`) {
		t.Fatalf("failed listing returned a partial body: %s", response.Body.String())
	}
}

// A caller with no grant on the stack can't tell a missing object from one it
// can't see: both are 404, on writes as well as reads (#387).
func TestInheritedRouteMissingAndDeniedStatusesMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		method   string
		path     string
		body     string
		resource string
	}{
		{name: "config", method: http.MethodPatch, path: "/v1/tenants/tenant_123/stack-templates/stack_template_123/config", body: `{"config":{}}`, resource: "stack-template"},
		{name: "upgrade", method: http.MethodPost, path: "/v1/tenants/tenant_123/stack-templates/stack_template_123/upgrade", body: `{"target_template_revision_id":"revision_123"}`, resource: "stack-template"},
		{name: "start run", method: http.MethodPost, path: "/v1/tenants/tenant_123/stack-templates/stack_template_123/runs", body: `{"operation":"plan"}`, resource: "stack-template"},
		{name: "run detail", method: http.MethodGet, path: "/v1/tenants/tenant_123/template-runs/run_123"},
		{name: "approval", method: http.MethodPost, path: "/v1/tenants/tenant_123/template-runs/run_123/approval", body: `{}`},
		{name: "discard", method: http.MethodPost, path: "/v1/tenants/tenant_123/template-runs/run_123/discard", body: `{}`},
		{name: "log list", method: http.MethodGet, path: "/v1/tenants/tenant_123/template-runs/run_123/logs"},
		{name: "log body", method: http.MethodGet, path: "/v1/tenants/tenant_123/template-runs/run_123/logs/plan"},
		{name: "run history", method: http.MethodGet, path: "/v1/tenants/tenant_123/stack-templates/stack_template_123/runs", resource: "stack-template"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			statuses := make([]int, 0, 2)
			for _, condition := range []string{"missing", "denied"} {
				deps := newPermissionMatrixDependencies(t)
				if condition == "missing" {
					if test.resource == "stack-template" {
						deps.stackTemplates.getErr = app.ErrNotFound
					} else {
						deps.templateRuns.getErr = app.ErrNotFound
					}
				}
				// "denied" needs no setup: the matrix subject holds no grants
				// unless a test gives it one, which is what an unassigned user
				// actually looks like.
				server := NewServer(deps.service(), configuredTenantID)
				response := httptest.NewRecorder()
				request := ordinaryAuthenticatedRequest(test.method, test.path, strings.NewReader(test.body))
				server.ServeHTTP(response, request)
				statuses = append(statuses, response.Code)
				if deps.stackTemplateInstaller.created.ID != "" || len(deps.stackTemplates.gotConfigJSON) != 0 || deps.stackTemplates.gotDesiredTemplateRevisionID != "" || deps.templateRuns.created.ID != "" || deps.templateRuns.approval.RunID != "" || deps.templateRuns.discarded.RunID != "" || deps.workflows.approvalRunID != "" {
					t.Fatal("protected missing or denied request had side effects")
				}
			}
			if statuses[0] != http.StatusNotFound || statuses[1] != http.StatusNotFound {
				t.Fatalf("missing=%d denied=%d want=%d", statuses[0], statuses[1], http.StatusNotFound)
			}
		})
	}
}

func newPermissionMatrixDependencies(t *testing.T) *apiTestDependencies {
	// Bare on purpose: the matrix is about what one stack role reaches, so the
	// subject must hold nothing except what each case grants.
	deps := newBareAPITestDependencies(t)
	stack := domain.Stack{ID: "stack_123", TenantID: "tenant_123", Name: "Acme", Slug: "acme", CreatedAt: time.Unix(1, 0)}
	deps.stacks.stack = stack
	deps.stacks.list = []domain.Stack{stack}
	deps.stacks.view = app.StackView{Stack: stack}
	deps.stackTemplates.stackTemplate = domain.StackTemplate{
		ID:                        "stack_template_123",
		TenantID:                  "tenant_123",
		StackID:                   "stack_123",
		DesiredTemplateRevisionID: "revision_123",
		WorkspaceName:             "acme-vpc",
		Lifecycle:                 domain.StackTemplateActive,
		// run_123 is a plan waiting for approval that still matches desired,
		// so the approval routes have something to approve.
		PendingPlanRunID:              "run_123",
		PendingPlanTemplateRevisionID: "revision_123",
		PendingPlanConfigJSON:         json.RawMessage(`{}`),
	}
	deps.templates.template = domain.TemplateRevision{ID: "revision_123", TenantID: "tenant_123", Status: domain.TemplateRevisionActive}
	deps.templateRuns.run = domain.TemplateRun{ID: "run_123", TenantID: "tenant_123", StackTemplateID: "stack_template_123", Status: domain.TemplateRunWaitingApproval}
	// The history the routes list is finished, so nothing blocks the config
	// and revision routes: those refuse while a run is in flight.
	deps.templateRuns.list = []domain.TemplateRun{{ID: "run_122", TenantID: "tenant_123", StackTemplateID: "stack_template_123", Status: domain.TemplateRunCompleted}}
	deps.logMetadata.logs = []domain.TemplateRunLog{{TenantID: "tenant_123", RunID: "run_123", Phase: "plan"}}
	deps.logMetadata.log = domain.TemplateRunLog{TenantID: "tenant_123", RunID: "run_123", Phase: "plan", ObjectKey: "runs/run_123/plan.log"}
	deps.logs.content = []byte("plan output")
	return deps
}

func TestDeniedStackMutationReturnsForbiddenWithoutSideEffects(t *testing.T) {
	t.Parallel()

	// A viewer can see the stack, so the refusal is 403. A subject with no
	// grant gets 404 instead (TestStackRoleRoutesDenyInsufficientRoles).
	deps := newBareAPITestDependencies(t).withGrants(mustAPIGrant(t, apiOIDCSubject, "stack_123", authorization.RelationViewer))
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := ordinaryAuthenticatedRequest(http.MethodPost, "/v1/tenants/tenant_123/stacks/stack_123/templates", strings.NewReader(`{"template_revision_id":"revision_123","config":{}}`))

	server.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusForbidden, response.Body.String())
	}
	if deps.stackTemplateInstaller.created.ID != "" {
		t.Fatalf("stack template = %#v, want no mutation", deps.stackTemplateInstaller.created)
	}
}

func TestAuthorizationDependencyFailureReturnsServiceUnavailable(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.authorizer = newFailingAPIAuthorization(t)
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := ordinaryAuthenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/stacks/stack_123", nil)

	server.ServeHTTP(response, request)

	// The load-bearing part is that this is not 403 and not 404: a caller must
	// never be told they lack access when the truth is that we could not tell.
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusInternalServerError, response.Body.String())
	}
	body := decodeProblem(t, response)
	if body.Type != blankType {
		t.Fatalf("type = %q, want %q", body.Type, blankType)
	}
}

func TestMissingAuthorizerFailsRatherThanRefusing(t *testing.T) {
	t.Parallel()

	service := app.NewService(app.Service{Stacks: &recordingStackRepository{}})
	server := NewServer(service, configuredTenantID)
	response := httptest.NewRecorder()
	request := ordinaryAuthenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/stacks/stack_123", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusInternalServerError, response.Body.String())
	}
}

func TestCreateStackWithMissingAuthorizerReturnsServiceUnavailable(t *testing.T) {
	t.Parallel()

	stacks := &recordingStackRepository{}
	service := app.NewService(app.Service{Stacks: stacks})
	server := NewServer(service, configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(http.MethodPost, "/v1/tenants/tenant_123/stacks", strings.NewReader(`{"name":"Acme"}`))

	server.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusInternalServerError, response.Body.String())
	}
	if stacks.created.ID != "" {
		t.Fatalf("created stack = %#v, want no persistence", stacks.created)
	}
}

func TestPlatformAdminCannotBypassMissingAuthorizer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "stack creation", method: http.MethodPost, path: "/v1/tenants/tenant_123/stacks", body: `{"name":"Acme"}`},
		{name: "stack list", method: http.MethodGet, path: "/v1/tenants/tenant_123/stacks"},
		{name: "stack detail", method: http.MethodGet, path: "/v1/tenants/tenant_123/stacks/stack_123"},
		{name: "stack mutation", method: http.MethodPost, path: "/v1/tenants/tenant_123/stacks/stack_123/templates", body: `{"template_revision_id":"revision_123","config":{}}`},
		{name: "config mutation", method: http.MethodPatch, path: "/v1/tenants/tenant_123/stack-templates/stack_template_123/config", body: `{"config":{}}`},
		{name: "upgrade mutation", method: http.MethodPost, path: "/v1/tenants/tenant_123/stack-templates/stack_template_123/upgrade", body: `{"target_template_revision_id":"revision_123"}`},
		{name: "start run", method: http.MethodPost, path: "/v1/tenants/tenant_123/stack-templates/stack_template_123/runs", body: `{"operation":"plan"}`},
		{name: "inherited read", method: http.MethodGet, path: "/v1/tenants/tenant_123/template-runs/run_123"},
		{name: "approval", method: http.MethodPost, path: "/v1/tenants/tenant_123/template-runs/run_123/approval", body: `{}`},
		{name: "discard", method: http.MethodPost, path: "/v1/tenants/tenant_123/template-runs/run_123/discard", body: `{}`},
		{name: "log list", method: http.MethodGet, path: "/v1/tenants/tenant_123/template-runs/run_123/logs"},
		{name: "log body", method: http.MethodGet, path: "/v1/tenants/tenant_123/template-runs/run_123/logs/plan"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			stacks := &recordingStackRepository{}
			runs := &recordingTemplateRunRepository{}
			installer := &recordingStackTemplateInstaller{}
			service := app.NewService(app.Service{Stacks: stacks, TemplateRuns: runs, StackTemplateInstaller: installer})
			server := NewServer(service, configuredTenantID)
			response := httptest.NewRecorder()
			request := authenticatedRequest(test.method, test.path, strings.NewReader(test.body))

			server.ServeHTTP(response, request)

			if response.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusInternalServerError, response.Body.String())
			}
			if stacks.gotStackID != "" || stacks.gotListTenantID != "" || stacks.created.ID != "" || runs.gotGetRunID != "" || runs.created.ID != "" || runs.approval.RunID != "" || runs.discarded.RunID != "" || installer.created.ID != "" {
				t.Fatal("missing authorizer allowed repository access or mutation")
			}
		})
	}
}

func TestMalformedStackIDReturnsProtectedNotFound(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := ordinaryAuthenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/stacks/stack:bad", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusNotFound, response.Body.String())
	}
}

func TestPlatformAdminMalformedStackIDReturnsProtectedNotFound(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t).withPlatformTier("admin")
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/stacks/stack:bad", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusNotFound, response.Body.String())
	}
}

func TestMalformedPersistedOwningStackIDReturnsServiceUnavailable(t *testing.T) {
	t.Parallel()

	for _, tier := range []string{"", "admin"} {
		t.Run(tier, func(t *testing.T) {
			t.Parallel()
			deps := newPermissionMatrixDependencies(t)
			deps.stackTemplates.stackTemplate.StackID = "bad:id"
			server := NewServer(deps.service(), configuredTenantID)
			response := httptest.NewRecorder()
			if tier != "" {
				deps.withPlatformTier(tier)
			}
			request := ordinaryAuthenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/template-runs/run_123", nil)

			server.ServeHTTP(response, request)

			if response.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusInternalServerError, response.Body.String())
			}
		})
	}
}

func TestMalformedGeneratedStackIDReturnsServiceUnavailable(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.createdStackID = "bad:id"
	deps.withPlatformTier("editor")
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(http.MethodPost, "/v1/tenants/tenant_123/stacks", strings.NewReader(`{"name":"Acme"}`))

	server.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusInternalServerError, response.Body.String())
	}
	if deps.stacks.created.ID != "" {
		t.Fatalf("created stack = %#v, want no persistence", deps.stacks.created)
	}
}

func TestSearchUsersPlatformAdminAllowed(t *testing.T) {
	t.Parallel()

	expected := []app.UserProfile{
		{Sub: "u1", DisplayName: "Alice Smith", Email: "alice@example.com"},
		{Sub: "u2", DisplayName: "Bob Jones", Email: "bob@example.com"},
	}
	deps := newAPITestDependencies(t).withPlatformTier("admin")
	deps.users = apiFakeUserRepository{users: expected}
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/users/search?q=ali", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	var body searchUsersResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Users) != 2 {
		t.Fatalf("users = %d, want 2", len(body.Users))
	}
	if body.Users[0].DisplayName != "Alice Smith" {
		t.Fatalf("first user = %q, want Alice Smith", body.Users[0].DisplayName)
	}
	if body.First != 0 {
		t.Fatalf("first = %d, want 0", body.First)
	}
	if body.Max != 20 {
		t.Fatalf("max = %d, want 20", body.Max)
	}
}

func TestSearchUsersNonAdminForbidden(t *testing.T) {
	t.Parallel()

	deps := newBareAPITestDependencies(t)
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := ordinaryAuthenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/users/search?q=ali", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusForbidden, response.Body.String())
	}
}

func TestSearchUsersMissingQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
	}{
		{name: "no q param", url: "/v1/tenants/tenant_123/users/search"},
		{name: "empty q", url: "/v1/tenants/tenant_123/users/search?q="},
		{name: "whitespace q", url: "/v1/tenants/tenant_123/users/search?q=+"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			deps := newAPITestDependencies(t)
			server := NewServer(deps.service(), configuredTenantID)
			response := httptest.NewRecorder()
			request := authenticatedRequest(http.MethodGet, test.url, nil)

			server.ServeHTTP(response, request)

			assertValidationFailed(t, response, "q required")
		})
	}
}

// A parameter that isn't an integer can't be parsed (400 invalid-query); one
// that parses but is out of range breaks a rule (422). Either way, every bad
// parameter is named.
func TestSearchUsersInvalidPagination(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		url        string
		status     int
		problem    string
		parameters string
	}{
		{name: "negative first", url: "/v1/tenants/tenant_123/users/search?q=test&first=-1", status: 422, problem: "validation-failed", parameters: "first out_of_range"},
		{name: "max=0", url: "/v1/tenants/tenant_123/users/search?q=test&max=0", status: 422, problem: "validation-failed", parameters: "max out_of_range"},
		{name: "max=51", url: "/v1/tenants/tenant_123/users/search?q=test&max=51", status: 422, problem: "validation-failed", parameters: "max out_of_range"},
		{name: "both out of range", url: "/v1/tenants/tenant_123/users/search?q=&first=-1&max=99", status: 422, problem: "validation-failed", parameters: "q required, first out_of_range, max out_of_range"},
		{name: "non-numeric first", url: "/v1/tenants/tenant_123/users/search?q=test&first=abc", status: 400, problem: "invalid-query", parameters: "first invalid_format"},
		{name: "non-numeric max", url: "/v1/tenants/tenant_123/users/search?q=test&max=abc", status: 400, problem: "invalid-query", parameters: "max invalid_format"},
		{name: "both non-numeric", url: "/v1/tenants/tenant_123/users/search?q=test&first=x&max=y", status: 400, problem: "invalid-query", parameters: "first invalid_format, max invalid_format"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			deps := newAPITestDependencies(t)
			server := NewServer(deps.service(), configuredTenantID)
			response := httptest.NewRecorder()
			request := authenticatedRequest(http.MethodGet, test.url, nil)

			server.ServeHTTP(response, request)

			if response.Code != test.status {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.status, response.Body.String())
			}
			problem := decodeProblem(t, response)
			if problem.Type != problemTypeURI(test.problem) {
				t.Fatalf("type = %q, want %s", problem.Type, test.problem)
			}
			got := make([]string, 0, len(problem.Errors))
			for _, entry := range problem.Errors {
				if entry.Pointer != "" {
					t.Fatalf("entry %+v has a pointer; a query problem names its parameter", entry)
				}
				got = append(got, entry.Parameter+" "+string(entry.Issue))
			}
			if strings.Join(got, ", ") != test.parameters {
				t.Fatalf("errors = %q, want %q", got, test.parameters)
			}
		})
	}
}

// The projection is a local table, so a read failure is a database failure and
// gets the same 500 as any other repository error. There is no longer an
// external directory that can be "unavailable" on its own.
func TestSearchUsersRepositoryFailureIsInternalError(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t).withPlatformTier("admin")
	deps.users = apiFakeUserRepository{searchErr: errors.New("connection refused")}
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/users/search?q=test", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusInternalServerError, response.Body.String())
	}
	body := decodeProblem(t, response)
	if body.Type != blankType {
		t.Fatalf("type = %q, want %q", body.Type, blankType)
	}
}

func TestSearchUsersUnauthenticated(t *testing.T) {
	t.Parallel()

	server := NewServer(newAPITestDependencies(t).service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant_123/users/search?q=test", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusUnauthorized, response.Body.String())
	}
}

type apiTestDependencies struct {
	authorizer             *authorization.Authorization
	t                      *testing.T
	stacks                 recordingStackRepository
	stackOverview          apiStackOverviewRepository
	stackTemplates         recordingStackTemplateRepository
	stackTemplateInstaller recordingStackTemplateInstaller
	templateRuns           recordingTemplateRunRepository
	registrations          recordingTemplateRegistrationRepository
	templates              recordingTemplateRepository
	logs                   recordingTemplateRunLogReader
	logMetadata            recordingTemplateRunLogRepository
	workflows              recordingWorkflowDispatcher
	users                  apiFakeUserRepository
	stackID                domain.StackID
	// createdStackID is what the generator mints, kept distinct from stackID so
	// a creation test does not collide with the tuples seeded for the fixture
	// stack.
	createdStackID  domain.StackID
	stackTemplateID domain.StackTemplateID
	runID           domain.TemplateRunID
	registrationID  domain.TemplateRegistrationID
	now             time.Time
	work            *apiUnitOfWork
}

// newAPITestDependencies builds a harness whose subject is a platform
// administrator, which is what most of these tests want: they are about routing,
// serialisation and status codes, and authorization is incidental. The tests
// that are about authorization start from newBareAPITestDependencies and grant
// exactly what they mean to.
// sessionCookieSubject is who sessionCookieServer authenticates as.
const sessionCookieSubject = "user-123"

func newAPITestDependencies(t *testing.T) *apiTestDependencies {
	t.Helper()
	deps := newBareAPITestDependencies(t).withPlatformTier("admin")
	// The session-cookie tests authenticate as a different subject than the
	// bearer-token ones, and they are no more about authorization than the
	// rest, so both are administrators here.
	return deps.withPlatformTierFor(sessionCookieSubject, "admin")
}

func newBareAPITestDependencies(t *testing.T) *apiTestDependencies {
	t.Helper()
	auth, err := authorization.NewWithDatastore(context.Background(), memory.New(), "openplan-test")
	if err != nil {
		t.Fatalf("build authorization: %v", err)
	}
	t.Cleanup(auth.Close)
	// Every stack these tests reach is served by a fake repository rather than
	// created through the API, so nothing has written its parent edge. Without
	// it a platform administrator cannot reach the stack, because the model
	// routes that access through "can_administer from parent".
	seedAPIParentEdge(t, auth, "stack_123")
	deps := &apiTestDependencies{
		t:               t,
		authorizer:      auth,
		stackID:         domain.StackID("stack_123"),
		createdStackID:  domain.StackID("stack_new"),
		stackTemplateID: domain.StackTemplateID("stack_template_123"),
		runID:           domain.TemplateRunID("run_123"),
		registrationID:  domain.TemplateRegistrationID("template_registration_123"),
		now:             time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC),
	}
	// The fixture run is a plan waiting for approval, and the fixture stack
	// template's pending plan, so an approval request has something to
	// approve. Tests about other run states set their own.
	deps.templateRuns.run = domain.TemplateRun{ID: "run_123", TenantID: "tenant_123", StackTemplateID: "stack_template_123", Status: domain.TemplateRunWaitingApproval}
	deps.stackTemplates.stackTemplate.PendingPlanRunID = "run_123"
	deps.stackTemplates.stackTemplate.PendingPlanConfigJSON = json.RawMessage(`{}`)
	return deps
}

// withPlatformTier sets the subject's platform role, replacing any it already
// holds. Replacing rather than adding is what lets the default administrator
// harness be narrowed by a test that wants a weaker tier.
func (deps *apiTestDependencies) withPlatformTier(tier string) *apiTestDependencies {
	return deps.withPlatformTierFor(apiOIDCSubject, tier)
}

func (deps *apiTestDependencies) withPlatformTierFor(sub, tier string) *apiTestDependencies {
	deps.t.Helper()
	subject, err := authorization.SubjectFromOIDCSub(sub)
	if err != nil {
		deps.t.Fatalf("SubjectFromOIDCSub: %v", err)
	}

	held, err := deps.authorizer.ListGrants(context.Background(), authorization.Platform)
	if err != nil {
		deps.t.Fatalf("ListGrants: %v", err)
	}
	var stale []authorization.Grant
	for _, grant := range held {
		if grant.Subject().String() == subject.String() {
			stale = append(stale, grant)
		}
	}
	if len(stale) > 0 {
		if err := deps.authorizer.Revoke(context.Background(), stale...); err != nil {
			deps.t.Fatalf("revoke platform grants: %v", err)
		}
	}

	role, ok := platformRole(tier)
	if !ok {
		return deps
	}
	grant, err := authorization.NewGrant(subject, authorization.Platform, role)
	if err != nil {
		deps.t.Fatalf("NewGrant: %v", err)
	}
	return deps.withGrants(grant)
}

// withStackRole grants the request subject one role on the fixture stack. What
// that role can reach is the model's answer, not a table in this file.
func (deps *apiTestDependencies) withStackRole(role authorization.Relation) *apiTestDependencies {
	deps.t.Helper()
	if role == (authorization.Relation{}) {
		return deps
	}
	subject, err := authorization.SubjectFromOIDCSub(apiOIDCSubject)
	if err != nil {
		deps.t.Fatalf("SubjectFromOIDCSub: %v", err)
	}
	object, err := authorization.ObjectFromID(authorization.TypeStack, string(deps.stackID))
	if err != nil {
		deps.t.Fatalf("ObjectFromID: %v", err)
	}
	grant, err := authorization.NewGrant(subject, object, role)
	if err != nil {
		deps.t.Fatalf("NewGrant: %v", err)
	}
	return deps.withGrants(grant)
}

func (deps *apiTestDependencies) withGrants(grants ...authorization.Grant) *apiTestDependencies {
	deps.t.Helper()
	if len(grants) == 0 {
		return deps
	}
	if err := deps.authorizer.Grant(context.Background(), grants...); err != nil {
		deps.t.Fatalf("seed grants: %v", err)
	}
	return deps
}

// seedAPIParentEdge stores the structural edge linking a stack to the platform.
func seedAPIParentEdge(t *testing.T, auth *authorization.Authorization, stackID string) {
	t.Helper()
	object, err := authorization.ObjectFromID(authorization.TypeStack, stackID)
	if err != nil {
		t.Fatalf("ObjectFromID: %v", err)
	}
	edge, err := authorization.NewStructuralRelationship(authorization.PlatformSubject, object, authorization.RelationParent)
	if err != nil {
		t.Fatalf("NewStructuralRelationship: %v", err)
	}
	if err := auth.Grant(context.Background(), edge); err != nil {
		t.Fatalf("seed parent edge: %v", err)
	}
}

func (deps *apiTestDependencies) service() *app.Service {
	work := &apiUnitOfWork{
		stacks:                &deps.stacks,
		templateRuns:          &deps.templateRuns,
		templateRegistrations: &deps.registrations,
	}
	deps.work = work
	return app.NewService(app.Service{
		Authorization:            deps.authorizer,
		Work:                     work,
		Stacks:                   &deps.stacks,
		StackOverview:            &deps.stackOverview,
		StackTemplates:           &deps.stackTemplates,
		StackTemplateInstaller:   &deps.stackTemplateInstaller,
		TemplateRuns:             &deps.templateRuns,
		TemplateRegistrations:    &deps.registrations,
		TemplateRevisionMetadata: &deps.templates,
		TemplateRevisions:        &deps.templates,
		TemplateRunLogs:          &deps.logs,
		TemplateRunLogMetadata:   &deps.logMetadata,
		Workflows:                &deps.workflows,
		Users:                    &deps.users,
		StackIDs:                 fixedStackIDGenerator{id: deps.createdStackID},
		StackTemplateIDs:         fixedStackTemplateIDGenerator{id: deps.stackTemplateID},
		RunIDs:                   fixedTemplateRunIDGenerator{runID: deps.runID},
		RegistrationIDs:          fixedTemplateRegistrationIDGenerator{id: deps.registrationID},
		Clock:                    fixedClock{now: deps.now},
	})
}

// apiStackOverviewRepository holds a tenant's templates and waiting runs in
// memory.
type apiStackOverviewRepository struct {
	templates []domain.StackTemplate
	waiting   []domain.TemplateRun
}

func (repository *apiStackOverviewRepository) ListTenantStackTemplates(context.Context, domain.TenantID) ([]domain.StackTemplate, error) {
	return repository.templates, nil
}

func (repository *apiStackOverviewRepository) ListTemplateRunsByStatus(_ context.Context, _ domain.TenantID, status domain.TemplateRunStatus) ([]domain.TemplateRun, error) {
	if status != domain.TemplateRunWaitingApproval {
		return nil, nil
	}
	return repository.waiting, nil
}

// apiUnitOfWork applies writes immediately; transactional behaviour is proven
// against a real database in internal/postgres/unitofwork_test.go.
type apiUnitOfWork struct {
	stacks                app.StackRepository
	templateRuns          app.TemplateRunRepository
	templateRegistrations app.TemplateRegistrationRepository
	requests              []queue.Request
	audits                []domain.SecurityAuditEvent
	err                   error
}

func (unit *apiUnitOfWork) InTx(ctx context.Context, fn func(context.Context, app.TxRepo, queue.Enqueuer) error) error {
	if unit.err != nil {
		return unit.err
	}
	return fn(ctx, unit, unit)
}

func (unit *apiUnitOfWork) CreateStack(ctx context.Context, stack domain.Stack) error {
	if unit.stacks == nil {
		return nil
	}
	return unit.stacks.CreateStack(ctx, stack)
}

func (unit *apiUnitOfWork) AppendAuditEvent(_ context.Context, event domain.SecurityAuditEvent) error {
	unit.audits = append(unit.audits, event)
	return nil
}

func (unit *apiUnitOfWork) CreateTemplateRun(ctx context.Context, run domain.TemplateRun) (int, error) {
	if repository, ok := unit.templateRuns.(interface {
		CreateTemplateRun(context.Context, domain.TemplateRun) (int, error)
	}); ok {
		return repository.CreateTemplateRun(ctx, run)
	}
	return 0, nil
}

func (unit *apiUnitOfWork) CreateTemplateRegistration(ctx context.Context, registration domain.TemplateRegistration) error {
	if repository, ok := unit.templateRegistrations.(interface {
		CreateTemplateRegistration(context.Context, domain.TemplateRegistration) error
	}); ok {
		return repository.CreateTemplateRegistration(ctx, registration)
	}
	return nil
}

func (unit *apiUnitOfWork) ApproveTemplateRun(ctx context.Context, approval domain.TemplateRunApproval) error {
	if repository, ok := unit.templateRuns.(interface {
		ApproveTemplateRun(context.Context, domain.TemplateRunApproval) error
	}); ok {
		return repository.ApproveTemplateRun(ctx, approval)
	}
	return nil
}

func (unit *apiUnitOfWork) DiscardTemplateRun(ctx context.Context, discard domain.TemplateRunDiscard) (bool, error) {
	if repository, ok := unit.templateRuns.(interface {
		DiscardTemplateRun(context.Context, domain.TemplateRunDiscard) (bool, error)
	}); ok {
		return repository.DiscardTemplateRun(ctx, discard)
	}
	return false, nil
}

func (unit *apiUnitOfWork) Enqueue(_ context.Context, requests ...queue.Request) error {
	unit.requests = append(unit.requests, requests...)
	return nil
}

type recordingStackRepository struct {
	created         domain.Stack
	stack           domain.Stack
	list            []domain.Stack
	view            app.StackView
	gotTenantID     domain.TenantID
	gotStackID      domain.StackID
	gotListTenantID domain.TenantID
	createErr       error
	getErr          error
	listErr         error
	getViewErr      error
}

func (repository *recordingStackRepository) CreateStack(_ context.Context, stack domain.Stack) error {
	if repository.createErr != nil {
		return repository.createErr
	}
	repository.created = stack
	return nil
}

func (repository *recordingStackRepository) GetStack(_ context.Context, tenantID domain.TenantID, stackID domain.StackID) (domain.Stack, error) {
	repository.gotTenantID = tenantID
	repository.gotStackID = stackID
	if repository.getErr != nil {
		return domain.Stack{}, repository.getErr
	}
	return repository.stack, nil
}

func (repository *recordingStackRepository) GetStackWithTemplates(_ context.Context, tenantID domain.TenantID, stackID domain.StackID) (app.StackView, error) {
	repository.gotTenantID = tenantID
	repository.gotStackID = stackID
	if repository.getViewErr != nil {
		return app.StackView{}, repository.getViewErr
	}
	return repository.view, nil
}

func (repository *recordingStackRepository) ListStacks(_ context.Context, tenantID domain.TenantID) ([]domain.Stack, error) {
	repository.gotListTenantID = tenantID
	if repository.listErr != nil {
		return nil, repository.listErr
	}
	return repository.list, nil
}

func (repository *recordingStackRepository) ListStacksPage(_ context.Context, tenantID domain.TenantID, after *app.StackPageCursor, limit int) ([]domain.Stack, error) {
	repository.gotListTenantID = tenantID
	if repository.listErr != nil {
		return nil, repository.listErr
	}
	start := 0
	if after != nil {
		for i, stack := range repository.list {
			if stack.ID == after.ID && stack.CreatedAt.Equal(after.CreatedAt) {
				start = i + 1
				break
			}
		}
	}
	end := min(start+limit, len(repository.list))
	return append([]domain.Stack(nil), repository.list[start:end]...), nil
}

type recordingStackTemplateRepository struct {
	stackTemplate                domain.StackTemplate
	gotTenantID                  domain.TenantID
	gotID                        domain.StackTemplateID
	gotConfigJSON                json.RawMessage
	gotDesiredTemplateRevisionID domain.TemplateRevisionID
	getErr                       error
}

func (repository *recordingStackTemplateRepository) GetStackTemplate(_ context.Context, tenantID domain.TenantID, id domain.StackTemplateID) (domain.StackTemplate, error) {
	repository.gotTenantID = tenantID
	repository.gotID = id
	if repository.getErr != nil {
		return domain.StackTemplate{}, repository.getErr
	}
	stackTemplate := repository.stackTemplate
	if stackTemplate.StackID == "" {
		stackTemplate.StackID = "stack_123"
	}
	return stackTemplate, nil
}

func (repository *recordingStackTemplateRepository) UpdateStackTemplateConfig(_ context.Context, tenantID domain.TenantID, id domain.StackTemplateID, configJSON json.RawMessage) (domain.StackTemplate, error) {
	repository.gotTenantID = tenantID
	repository.gotID = id
	repository.gotConfigJSON = configJSON
	updated := repository.stackTemplate
	updated.DesiredConfigJSON = configJSON
	return updated, nil
}

func (repository *recordingStackTemplateRepository) UpdateStackTemplateDesiredRevision(_ context.Context, tenantID domain.TenantID, id domain.StackTemplateID, templateID domain.TemplateRevisionID, configJSON json.RawMessage) (domain.StackTemplate, error) {
	repository.gotTenantID = tenantID
	repository.gotID = id
	repository.gotDesiredTemplateRevisionID = templateID
	repository.gotConfigJSON = configJSON
	updated := repository.stackTemplate
	updated.DesiredTemplateRevisionID = templateID
	updated.DesiredConfigJSON = configJSON
	return updated, nil
}

type recordingStackTemplateInstaller struct {
	created   domain.StackTemplate
	createErr error
}

func (installer *recordingStackTemplateInstaller) CreateStackTemplate(_ context.Context, stackTemplate domain.StackTemplate) error {
	if installer.createErr != nil {
		return installer.createErr
	}
	installer.created = stackTemplate
	return nil
}

type recordingTemplateRunRepository struct {
	created                domain.TemplateRun
	run                    domain.TemplateRun
	list                   []domain.TemplateRun
	executions             []domain.TemplateRunWorkflowExecution
	approval               domain.TemplateRunApproval
	discarded              domain.TemplateRunDiscard
	gotGetTenantID         domain.TenantID
	gotGetRunID            domain.TemplateRunID
	gotListTenantID        domain.TenantID
	gotListStackTemplateID domain.StackTemplateID
	getErr                 error
	createErr              error
	approvalErr            error
	// notDiscardable makes DiscardTemplateRun find no plan to discard.
	notDiscardable bool
}

func (repository *recordingTemplateRunRepository) CreateTemplateRun(_ context.Context, run domain.TemplateRun) (int, error) {
	if repository.createErr != nil {
		return 0, repository.createErr
	}
	repository.created = run
	return 1, nil
}

func (repository *recordingTemplateRunRepository) ListTemplateRunExecutions(context.Context, domain.TenantID, domain.TemplateRunID) ([]domain.TemplateRunWorkflowExecution, error) {
	return repository.executions, nil
}

func (repository *recordingTemplateRunRepository) ListTemplateRuns(_ context.Context, tenantID domain.TenantID, stackTemplateID domain.StackTemplateID) ([]domain.TemplateRun, error) {
	repository.gotListTenantID = tenantID
	repository.gotListStackTemplateID = stackTemplateID
	return repository.list, nil
}

func (repository *recordingTemplateRunRepository) GetTemplateRun(_ context.Context, tenantID domain.TenantID, runID domain.TemplateRunID) (domain.TemplateRun, error) {
	repository.gotGetTenantID = tenantID
	repository.gotGetRunID = runID
	if repository.getErr != nil {
		return domain.TemplateRun{}, repository.getErr
	}
	return repository.run, nil
}

func (repository *recordingTemplateRunRepository) ApproveTemplateRun(_ context.Context, approval domain.TemplateRunApproval) error {
	if repository.approvalErr != nil {
		return repository.approvalErr
	}
	repository.approval = approval
	return nil
}

func (repository *recordingTemplateRunRepository) DiscardTemplateRun(_ context.Context, discard domain.TemplateRunDiscard) (bool, error) {
	if repository.notDiscardable {
		return false, nil
	}
	repository.discarded = discard
	return true, nil
}

type recordingTemplateRunLogReader struct {
	content     []byte
	err         error
	gotTenantID domain.TenantID
	gotRunID    domain.TemplateRunID
	gotPhase    string
}

func (reader *recordingTemplateRunLogReader) ReadTemplateRunLog(_ context.Context, log domain.TemplateRunLog) ([]byte, error) {
	reader.gotTenantID = log.TenantID
	reader.gotRunID = log.RunID
	reader.gotPhase = log.Phase
	if reader.err != nil {
		return nil, reader.err
	}
	return reader.content, nil
}

type recordingTemplateRunLogRepository struct {
	log             domain.TemplateRunLog
	logs            []domain.TemplateRunLog
	gotGetTenantID  domain.TenantID
	gotGetRunID     domain.TemplateRunID
	gotGetPhase     string
	gotListTenantID domain.TenantID
	gotListRunID    domain.TemplateRunID
	getErr          error
	listErr         error
}

func (repository *recordingTemplateRunLogRepository) GetTemplateRunLog(_ context.Context, tenantID domain.TenantID, runID domain.TemplateRunID, phase string) (domain.TemplateRunLog, error) {
	repository.gotGetTenantID = tenantID
	repository.gotGetRunID = runID
	repository.gotGetPhase = phase
	if repository.getErr != nil {
		return domain.TemplateRunLog{}, repository.getErr
	}
	return repository.log, nil
}

func (repository *recordingTemplateRunLogRepository) ListTemplateRunLogs(_ context.Context, tenantID domain.TenantID, runID domain.TemplateRunID) ([]domain.TemplateRunLog, error) {
	repository.gotListTenantID = tenantID
	repository.gotListRunID = runID
	if repository.listErr != nil {
		return nil, repository.listErr
	}
	return repository.logs, nil
}

type recordingTemplateRegistrationRepository struct {
	created        domain.TemplateRegistration
	registration   domain.TemplateRegistration
	gotGetTenantID domain.TenantID
	gotGetID       domain.TemplateRegistrationID
	createErr      error
	getErr         error
	statusInput    domain.TemplateRegistrationStatusActivityInput
	statusErr      error
}

func (repository *recordingTemplateRegistrationRepository) CreateTemplateRegistration(_ context.Context, registration domain.TemplateRegistration) error {
	if repository.createErr != nil {
		return repository.createErr
	}
	repository.created = registration
	return nil
}

func (repository *recordingTemplateRegistrationRepository) GetTemplateRegistration(_ context.Context, tenantID domain.TenantID, id domain.TemplateRegistrationID) (domain.TemplateRegistration, error) {
	repository.gotGetTenantID = tenantID
	repository.gotGetID = id
	if repository.getErr != nil {
		return domain.TemplateRegistration{}, repository.getErr
	}
	return repository.registration, nil
}

func (repository *recordingTemplateRegistrationRepository) RecordTemplateRegistrationStatus(_ context.Context, input domain.TemplateRegistrationStatusActivityInput) error {
	if repository.statusErr != nil {
		return repository.statusErr
	}
	repository.statusInput = input
	return nil
}

type recordingTemplateRepository struct {
	template                       domain.TemplateRevision
	templates                      []domain.TemplateRevision
	variables                      []domain.TemplateVariable
	gotTemplate                    domain.TemplateRevision
	gotVariables                   []domain.TemplateVariable
	gotListTenantID                domain.TenantID
	gotGetTemplateTenantID         domain.TenantID
	gotGetTemplateRevisionID       domain.TemplateRevisionID
	gotVariablesTenantID           domain.TenantID
	gotVariablesTemplateRevisionID domain.TemplateRevisionID
	getTemplateErr                 error
	listErr                        error
	upsertErr                      error
	variablesErr                   error
}

func (repository *recordingTemplateRepository) UpsertTemplateRevisionWithVariables(_ context.Context, template domain.TemplateRevision, variables []domain.TemplateVariable) (domain.TemplateRevision, error) {
	repository.gotTemplate = template
	repository.gotVariables = variables
	if repository.upsertErr != nil {
		return domain.TemplateRevision{}, repository.upsertErr
	}
	if repository.template.ID != "" {
		return repository.template, nil
	}
	return template, nil
}

func (repository *recordingTemplateRepository) GetTemplateRevision(_ context.Context, tenantID domain.TenantID, templateID domain.TemplateRevisionID) (domain.TemplateRevision, error) {
	repository.gotGetTemplateTenantID = tenantID
	repository.gotGetTemplateRevisionID = templateID
	if repository.getTemplateErr != nil {
		return domain.TemplateRevision{}, repository.getTemplateErr
	}
	return repository.template, nil
}

func (repository *recordingTemplateRepository) ListTemplateRevisions(_ context.Context, tenantID domain.TenantID) ([]domain.TemplateRevision, error) {
	repository.gotListTenantID = tenantID
	if repository.listErr != nil {
		return nil, repository.listErr
	}
	return repository.templates, nil
}

func (repository *recordingTemplateRepository) GetTemplateRevisionVariables(_ context.Context, tenantID domain.TenantID, templateID domain.TemplateRevisionID) ([]domain.TemplateVariable, error) {
	repository.gotVariablesTenantID = tenantID
	repository.gotVariablesTemplateRevisionID = templateID
	if repository.variablesErr != nil {
		return nil, repository.variablesErr
	}
	return repository.variables, nil
}

type recordingWorkflowDispatcher struct {
	input         domain.TemplateRunWorkflowInput
	syncInput     domain.TemplateSyncWorkflowInput
	approvalRunID domain.TemplateRunID
}

func (dispatcher *recordingWorkflowDispatcher) StartTemplateRun(_ context.Context, input domain.TemplateRunWorkflowInput) error {
	dispatcher.input = input
	return nil
}

func (dispatcher *recordingWorkflowDispatcher) StartTemplateSync(_ context.Context, input domain.TemplateSyncWorkflowInput) error {
	dispatcher.syncInput = input
	return nil
}

func (dispatcher *recordingWorkflowDispatcher) StartTemplateApply(_ context.Context, input domain.TemplateRunWorkflowInput) error {
	dispatcher.approvalRunID = input.RunID
	return nil
}

type fixedStackIDGenerator struct {
	id domain.StackID
}

func (generator fixedStackIDGenerator) NewStackID() domain.StackID {
	return generator.id
}

type fixedStackTemplateIDGenerator struct {
	id domain.StackTemplateID
}

func (generator fixedStackTemplateIDGenerator) NewStackTemplateID() domain.StackTemplateID {
	return generator.id
}

type fixedTemplateRunIDGenerator struct {
	runID domain.TemplateRunID
}

func (generator fixedTemplateRunIDGenerator) NewTemplateRunID() domain.TemplateRunID {
	return generator.runID
}

type fixedTemplateRegistrationIDGenerator struct {
	id domain.TemplateRegistrationID
}

func (generator fixedTemplateRegistrationIDGenerator) NewTemplateRegistrationID() domain.TemplateRegistrationID {
	return generator.id
}

type fixedClock struct {
	now time.Time
}

func (clock fixedClock) Now() time.Time {
	return clock.now
}

type apiFakeUserRepository struct {
	users     []app.UserProfile
	searchErr error
	lookupErr error
	upsertErr error
}

func (f *apiFakeUserRepository) UpsertUser(_ context.Context, profile app.UserProfile, _ time.Time) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.users = append(f.users, profile)
	return nil
}

func (f *apiFakeUserRepository) SearchUsers(_ context.Context, _ string, _, _ int) ([]app.UserProfile, error) {
	return f.users, f.searchErr
}

func (f *apiFakeUserRepository) UsersBySubs(_ context.Context, subs []string) (map[string]app.UserProfile, error) {
	if f.lookupErr != nil {
		return nil, f.lookupErr
	}
	wanted := make(map[string]struct{}, len(subs))
	for _, sub := range subs {
		wanted[sub] = struct{}{}
	}
	found := make(map[string]app.UserProfile)
	for _, user := range f.users {
		if _, ok := wanted[user.Sub]; ok {
			found[user.Sub] = user
		}
	}
	return found, nil
}

func TestMeReturnsIdentityWithGlobalCapabilities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		tier        string
		wantCreate  bool
		wantPublish bool
	}{
		// An administrator holds can_create_stack and can_publish_template
		// too: the model derives both from can_edit, which can_administer
		// satisfies.
		{name: "platform admin", tier: "admin", wantCreate: true, wantPublish: true},
		{name: "editor", tier: "editor", wantCreate: true, wantPublish: true},
		{name: "no tier", tier: "", wantCreate: false, wantPublish: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			deps := newAPITestDependencies(t).withPlatformTier(test.tier)
			server := NewServer(deps.service(), configuredTenantID)
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
			ctx := authentication.ContextWithPrincipal(request.Context(), authentication.Principal{
				Subject: apiOIDCSubject,
				Name:    "Test User",
				Email:   "test@example.com",
			})
			request = request.WithContext(ctx)

			server.ServeHTTP(response, request)

			// 200 with the identity, not 204: every assertion below reads the
			// body, and the web client calls this endpoint expecting one.
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
			}

			var body struct {
				Sub                string `json:"sub"`
				DisplayName        string `json:"display_name"`
				Email              string `json:"email"`
				GlobalCapabilities struct {
					CanCreateStack     bool `json:"can_create_stack"`
					CanPublishTemplate bool `json:"can_publish_template"`
				} `json:"global_capabilities"`
				TenantID string `json:"tenant_id"`
			}
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatalf("decode response: %v", err)
			}

			if body.Sub != apiOIDCSubject {
				t.Errorf("sub = %q, want %q", body.Sub, apiOIDCSubject)
			}
			if body.DisplayName != "Test User" {
				t.Errorf("display_name = %q, want %q", body.DisplayName, "Test User")
			}
			if body.Email != "test@example.com" {
				t.Errorf("email = %q, want %q", body.Email, "test@example.com")
			}
			if body.GlobalCapabilities.CanCreateStack != test.wantCreate {
				t.Errorf("can_create_stack = %t, want %t", body.GlobalCapabilities.CanCreateStack, test.wantCreate)
			}
			// The Sync button on the template detail screen is gated on this,
			// and RegisterTemplate is the route it would call.
			if body.GlobalCapabilities.CanPublishTemplate != test.wantPublish {
				t.Errorf("can_publish_template = %t, want %t", body.GlobalCapabilities.CanPublishTemplate, test.wantPublish)
			}
			if body.TenantID != string(configuredTenantID) {
				t.Errorf("tenant_id = %q, want %q", body.TenantID, string(configuredTenantID))
			}
		})
	}
}

func TestMeReturnsUnauthorizedWithoutPrincipal(t *testing.T) {
	t.Parallel()

	server := NewServer(app.NewService(app.Service{}), configuredTenantID)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/me", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusUnauthorized, response.Body.String())
	}
}

func testGrant(t *testing.T, sub, stackID, role string) authorization.Grant {
	t.Helper()
	subject, err := authorization.SubjectFromOIDCSub(sub)
	if err != nil {
		t.Fatalf("subject from sub: %v", err)
	}
	stack, err := authorization.ObjectFromID(authorization.TypeStack, stackID)
	if err != nil {
		t.Fatalf("stack from id: %v", err)
	}
	r, err := authorization.GrantRelation(role)
	if err != nil {
		t.Fatalf("role from relation: %v", err)
	}
	grant, err := authorization.NewGrant(subject, stack, r)
	if err != nil {
		t.Fatalf("new grant: %v", err)
	}
	return grant
}

func TestListStackGrantsListsGrants(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.users = apiFakeUserRepository{
		users: []app.UserProfile{{Sub: "user-1", DisplayName: "Alice", Email: "alice@example.com"}},
	}
	deps.withGrants(testGrant(t, "user-1", "stack_123", "owner"))
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/stacks/stack_123/grants", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	var body listStackGrantsResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Grants) != 1 {
		t.Fatalf("len(grants) = %d, want 1", len(body.Grants))
	}
	if body.Grants[0].UserSub != "user-1" || body.Grants[0].Role != "owner" {
		t.Fatalf("grant = %#v", body.Grants[0])
	}
	if body.Grants[0].DisplayName != "Alice" {
		t.Fatalf("display name = %q, want Alice", body.Grants[0].DisplayName)
	}
}

func TestListStackGrantsReturnsEmptyOnNoGrants(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/stacks/stack_123/grants", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	var body listStackGrantsResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Grants) != 0 {
		t.Fatalf("len(grants) = %d, want 0", len(body.Grants))
	}
}

func TestAssignStackRoleAssignsRoleAndReturnsGrantView(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.users = apiFakeUserRepository{
		users: []app.UserProfile{{Sub: "user-2", DisplayName: "bob", Email: "bob@example.com"}},
	}
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(
		http.MethodPost,
		"/v1/tenants/tenant_123/stacks/stack_123/grants",
		strings.NewReader(`{"user_sub":"user-2","role":"operator"}`),
	)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	var body app.GrantView
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.UserSub != "user-2" || body.Role != "operator" {
		t.Fatalf("grant view = %#v", body)
	}
}

func TestAssignStackRoleRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(
		http.MethodPost,
		"/v1/tenants/tenant_123/stacks/stack_123/grants",
		strings.NewReader(`{"user_sub":`),
	)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusBadRequest, response.Body.String())
	}
}

func TestAssignStackRoleRequiresManageAccess(t *testing.T) {
	t.Parallel()

	// An operator can see the stack and act on it, but not manage who else can.
	deps := newBareAPITestDependencies(t).withGrants(mustAPIGrant(t, apiOIDCSubject, "stack_123", authorization.RelationOperator))
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := ordinaryAuthenticatedRequest(
		http.MethodPost,
		"/v1/tenants/tenant_123/stacks/stack_123/grants",
		strings.NewReader(`{"user_sub":"user-2","role":"viewer"}`),
	)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusForbidden, response.Body.String())
	}
}

func TestRevokeStackRoleRemovesGrant(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.withGrants(testGrant(t, "user-3", "stack_123", "viewer"))
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(http.MethodDelete, "/v1/tenants/tenant_123/stacks/stack_123/grants/user-3", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusNoContent, response.Body.String())
	}
}

func TestRevokeStackRoleLastOwnerReturnsConflict(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.withGrants(testGrant(t, apiOIDCSubject, "stack_123", "owner"))
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(http.MethodDelete, "/v1/tenants/tenant_123/stacks/stack_123/grants/"+apiOIDCSubject, nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusConflict, response.Body.String())
	}
}

func TestAssignStackRoleLastOwnerDemotionReturnsConflict(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.users = apiFakeUserRepository{
		users: []app.UserProfile{{Sub: apiOIDCSubject, DisplayName: "admin", Email: "admin@example.com"}},
	}
	deps.withGrants(testGrant(t, apiOIDCSubject, "stack_123", "owner"))
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()
	request := authenticatedRequest(
		http.MethodPost,
		"/v1/tenants/tenant_123/stacks/stack_123/grants",
		strings.NewReader(`{"user_sub":"`+apiOIDCSubject+`","role":"viewer"}`),
	)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusConflict, response.Body.String())
	}
}

func TestAuthenticatedServerProtectsMeRoute(t *testing.T) {
	server := NewAuthenticatedServer(app.NewService(app.Service{}), configuredTenantID, false)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/me", nil)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusUnauthorized, response.Body.String())
	}
}

type stubQueueReader struct {
	statuses     []queue.Status
	tenantID     string
	actorSubject string
	err          error
}

func (reader *stubQueueReader) ListByActor(_ context.Context, tenantID, actorSubject string, _ int) ([]queue.Status, error) {
	reader.tenantID = tenantID
	reader.actorSubject = actorSubject
	return reader.statuses, reader.err
}

func TestListQueueReturnsOnlyCallerItems(t *testing.T) {
	t.Parallel()

	reader := &stubQueueReader{statuses: []queue.Status{{
		Kind:      "reconcile_stack_grant",
		State:     queue.StatePending,
		Attempts:  3,
		LastError: "openfga unavailable",
		CreatedAt: time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC),
	}}}
	deps := newAPITestDependencies(t)
	server := NewServer(deps.service(), configuredTenantID, WithQueueReader(reader))
	response := httptest.NewRecorder()

	server.ServeHTTP(response, authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/queue", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}

	var body struct {
		Items []struct {
			Kind      string `json:"kind"`
			State     string `json:"state"`
			Summary   string `json:"summary"`
			Attempts  int    `json:"attempts"`
			LastError string `json:"last_error"`
		} `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(body.Items))
	}
	// Retry is forever, so a stuck item stays pending; attempts and last_error
	// are the only way to tell "just queued" from "failing for an hour".
	if body.Items[0].Attempts != 3 || body.Items[0].LastError != "openfga unavailable" {
		t.Fatalf("item = %+v, want attempts and last_error surfaced", body.Items[0])
	}
	if body.Items[0].Summary != "reconcile_stack_grant" {
		t.Fatalf("summary = %q, want the kind as fallback", body.Items[0].Summary)
	}
	if reader.tenantID != "tenant_123" || reader.actorSubject != apiOIDCSubject {
		t.Fatalf("reader called with tenant %q actor %q, want the authenticated caller", reader.tenantID, reader.actorSubject)
	}
}

// A failing queue read answers like every other failure: the generic 500
// envelope, with nothing of the underlying error in it.
func TestListQueueFailureUsesTheGenericInternalError(t *testing.T) {
	t.Parallel()

	reader := &stubQueueReader{err: errors.New("pq: connection refused")}
	deps := newAPITestDependencies(t)
	server := NewServer(deps.service(), configuredTenantID, WithQueueReader(reader))
	response := httptest.NewRecorder()

	server.ServeHTTP(response, authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/queue", nil))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	body := decodeProblem(t, response)
	if body.Type != blankType || body.Title != "Internal Server Error" {
		t.Fatalf("body = %+v, want the generic internal error", body)
	}
}

// Still 503: an unconfigured queue reader is a genuinely unavailable
// dependency, unlike authorization, which is now in-process.
func TestListQueueWithoutReaderReturnsServiceUnavailable(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/queue", nil))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestListQueueRequiresAuthentication(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	server := NewServer(deps.service(), configuredTenantID, WithQueueReader(&stubQueueReader{}))
	response := httptest.NewRecorder()

	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant_123/queue", nil))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

// mustAPIGrant builds one stack grant for the API test harness.
func mustAPIGrant(t *testing.T, sub, stackID string, relation authorization.Relation) authorization.Grant {
	t.Helper()
	subject, err := authorization.SubjectFromOIDCSub(sub)
	if err != nil {
		t.Fatalf("SubjectFromOIDCSub: %v", err)
	}
	object, err := authorization.ObjectFromID(authorization.TypeStack, stackID)
	if err != nil {
		t.Fatalf("ObjectFromID: %v", err)
	}
	grant, err := authorization.NewGrant(subject, object, relation)
	if err != nil {
		t.Fatalf("NewGrant: %v", err)
	}
	return grant
}

// failAfterBootstrap wraps a working datastore and starts failing every tuple
// read and write once bootstrap has finished. The store and the model have to
// be resolvable for the Authorization to exist at all, so the failure cannot be
// present from the start.
type failAfterBootstrap struct {
	storage.OpenFGADatastore
	failing bool
}

var errAuthorizationOutage = errors.New("authorization datastore is unreachable")

func (d *failAfterBootstrap) ReadUserTuple(ctx context.Context, store string, filter storage.ReadUserTupleFilter, options storage.ReadUserTupleOptions) (*openfgav1.Tuple, error) {
	if d.failing {
		return nil, errAuthorizationOutage
	}
	return d.OpenFGADatastore.ReadUserTuple(ctx, store, filter, options)
}

func (d *failAfterBootstrap) ReadUsersetTuples(ctx context.Context, store string, filter storage.ReadUsersetTuplesFilter, options storage.ReadUsersetTuplesOptions) (storage.TupleIterator, error) {
	if d.failing {
		return nil, errAuthorizationOutage
	}
	return d.OpenFGADatastore.ReadUsersetTuples(ctx, store, filter, options)
}

func (d *failAfterBootstrap) ReadStartingWithUser(ctx context.Context, store string, filter storage.ReadStartingWithUserFilter, options storage.ReadStartingWithUserOptions) (storage.TupleIterator, error) {
	if d.failing {
		return nil, errAuthorizationOutage
	}
	return d.OpenFGADatastore.ReadStartingWithUser(ctx, store, filter, options)
}

// newFailingAPIAuthorization boots normally and then fails every tuple read, so
// a route's behaviour under an authorization outage can be asserted.
func newFailingAPIAuthorization(t *testing.T) *authorization.Authorization {
	t.Helper()
	datastore := &failAfterBootstrap{OpenFGADatastore: memory.New()}
	auth, err := authorization.NewWithDatastore(context.Background(), datastore, "openplan-test")
	if err != nil {
		t.Fatalf("build authorization: %v", err)
	}
	t.Cleanup(auth.Close)
	datastore.failing = true
	return auth
}

// decodeProblem fails unless response is an RFC 9457 problem document with
// exactly the members the API writes, and its status matches the response's.
func decodeProblem(t *testing.T, response *httptest.ResponseRecorder) problemDocument {
	t.Helper()
	if contentType := response.Header().Get("Content-Type"); contentType != problemContentType {
		t.Fatalf("Content-Type = %q, want %s; body = %s", contentType, problemContentType, response.Body.String())
	}
	decoder := json.NewDecoder(bytes.NewReader(response.Body.Bytes()))
	decoder.DisallowUnknownFields()
	var body problemDocument
	if err := decoder.Decode(&body); err != nil {
		t.Fatalf("body is not a problem document: %v; body = %s", err, response.Body.String())
	}
	if body.Type == "" || body.Title == "" || body.Status != response.Code {
		t.Fatalf("problem = %+v, want type, title and status %d", body, response.Code)
	}
	return body
}

// The middleware answers 401 before any handler runs, so it is the one error
// that never passes through writeProblem. It must still match it, content
// type included.
func TestUnauthenticatedRequestGetsTheErrorEnvelope(t *testing.T) {
	server := NewAuthenticatedServer(app.NewService(app.Service{}), configuredTenantID, false)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant_123/stacks", nil))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	requestID := response.Header().Get(RequestIDHeader)
	if requestID == "" {
		t.Fatal("middleware 401 has no request id")
	}
	rendered := httptest.NewRecorder()
	rendered.Header().Set(RequestIDHeader, requestID)
	writeAppError(rendered, app.ErrUnauthenticated)
	if got, want := decodeProblem(t, response), decodeProblem(t, rendered); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("middleware 401 = %+v, want %+v, what writeAppError renders", got, want)
	}
}

// Every response carries a request id, and an error body repeats it, so a
// person can quote it and an operator can find the matching log line.
func TestResponsesCarryARequestID(t *testing.T) {
	t.Parallel()

	server := NewServer(newAPITestDependencies(t).service(), configuredTenantID)

	ok := httptest.NewRecorder()
	server.ServeHTTP(ok, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if ok.Header().Get(RequestIDHeader) == "" {
		t.Fatal("200 has no request id")
	}

	failed := httptest.NewRecorder()
	server.ServeHTTP(failed, authenticatedRequest(http.MethodGet, "/v1/tenants/other_tenant/stacks", nil))
	if failed.Code < 400 {
		t.Fatalf("status = %d, want an error", failed.Code)
	}
	header := failed.Header().Get(RequestIDHeader)
	if body := decodeProblem(t, failed); header == "" || body.RequestID != header {
		t.Fatalf("header request id = %q, body request_id = %q, want the same non-empty id", header, body.RequestID)
	}
	if header == ok.Header().Get(RequestIDHeader) {
		t.Fatal("two requests got the same id")
	}
}

// An id a proxy already assigned is kept, so its logs and ours line up; one
// that could forge a log line is replaced.
func TestInboundRequestIDIsReusedOnlyWhenPlain(t *testing.T) {
	t.Parallel()

	server := NewServer(newAPITestDependencies(t).service(), configuredTenantID)
	for _, test := range []struct {
		inbound string
		kept    bool
	}{
		{inbound: "edge-7f3a.42_b", kept: true},
		{inbound: "spoof\nlevel=error msg=forged", kept: false},
		{inbound: "has space", kept: false},
		{inbound: strings.Repeat("a", 129), kept: false},
	} {
		request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		request.Header.Set(RequestIDHeader, test.inbound)
		response := httptest.NewRecorder()

		server.ServeHTTP(response, request)

		got := response.Header().Get(RequestIDHeader)
		if kept := got == test.inbound; kept != test.kept {
			t.Fatalf("inbound %q: response id = %q, kept = %t, want %t", test.inbound, got, kept, test.kept)
		}
		if got == "" {
			t.Fatalf("inbound %q: no request id", test.inbound)
		}
	}
}

// A request no route matches would otherwise get the mux's plain-text "404
// page not found" or "Method Not Allowed".
func TestUnroutedRequestsAreProblems(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		method    string
		path      string
		status    int
		wantAllow string
	}{
		{name: "unknown path", method: http.MethodGet, path: "/v1/no-such-route", status: http.StatusNotFound},
		{name: "unknown path outside /v1", method: http.MethodGet, path: "/nothing-here", status: http.StatusNotFound},
		// The mux's 405 names the allowed methods; that header survives.
		{name: "wrong method", method: http.MethodDelete, path: "/v1/me", status: http.StatusMethodNotAllowed, wantAllow: http.MethodGet},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			server := NewServer(nil, configuredTenantID)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, authenticatedRequest(test.method, test.path, nil))

			if response.Code != test.status {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.status, response.Body.String())
			}
			if body := decodeProblem(t, response); body.Type != blankType || body.Title != http.StatusText(test.status) {
				t.Fatalf("problem = %+v, want about:blank with the status text", body)
			}
			if test.wantAllow != "" && !strings.Contains(response.Header().Get("Allow"), test.wantAllow) {
				t.Fatalf("Allow = %q, want it to include %s", response.Header().Get("Allow"), test.wantAllow)
			}
		})
	}
}

// A path the mux redirects to its canonical form is still routed, so it keeps
// the mux's redirect rather than becoming a 404. Which redirect status is the
// mux's call, not ours: Go 1.26 moved it from 301 to 307.
func TestNonCanonicalPathStillRedirects(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, configuredTenantID)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedRequest(http.MethodGet, "/v1//me", nil))

	if response.Code < 300 || response.Code > 399 {
		t.Fatalf("status = %d, want a redirect", response.Code)
	}
	if location := response.Header().Get("Location"); location != "/v1/me" {
		t.Fatalf("Location = %q, want /v1/me", location)
	}
}
