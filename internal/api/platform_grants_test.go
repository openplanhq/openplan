package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vishu42/openplan/internal/app"
	"github.com/vishu42/openplan/internal/authorization"
)

func testPlatformGrant(t *testing.T, sub string, relation authorization.Relation) authorization.Grant {
	t.Helper()
	subject, err := authorization.SubjectFromOIDCSub(sub)
	if err != nil {
		t.Fatalf("subject from sub: %v", err)
	}
	grant, err := authorization.NewGrant(subject, authorization.Platform, relation)
	if err != nil {
		t.Fatalf("new grant: %v", err)
	}
	return grant
}

const platformGrantsPath = "/v1/tenants/tenant_123/platform/grants"

func TestListPlatformGrantsListsGrants(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.users = apiFakeUserRepository{
		users: []app.UserProfile{{Sub: "user-1", DisplayName: "Alice", Email: "alice@example.com"}},
	}
	deps.withGrants(testPlatformGrant(t, "user-1", authorization.RelationEditor))
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, authenticatedRequest(http.MethodGet, platformGrantsPath, nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	var body listResponse[app.GrantView]
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	var alice *app.GrantView
	for i := range body.Items {
		if body.Items[i].UserSub == "user-1" {
			alice = &body.Items[i]
		}
	}
	if alice == nil || alice.Role != "editor" || alice.DisplayName != "Alice" {
		t.Fatalf("grants = %#v, want user-1 as editor named Alice", body.Items)
	}
}

func TestAssignPlatformRoleAnswers201ThenOK(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.users = apiFakeUserRepository{
		users: []app.UserProfile{{Sub: "user-2", DisplayName: "bob", Email: "bob@example.com"}},
	}
	server := NewServer(deps.service(), configuredTenantID)

	for _, step := range []struct {
		role   string
		status int
	}{
		{role: "viewer", status: http.StatusCreated},
		{role: "admin", status: http.StatusOK},
		{role: "admin", status: http.StatusOK},
	} {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, authenticatedRequest(http.MethodPut, platformGrantsPath+"/user-2", strings.NewReader(`{"role":"`+step.role+`"}`)))

		if response.Code != step.status {
			t.Fatalf("PUT role %s: status = %d, want %d; body = %s", step.role, response.Code, step.status, response.Body.String())
		}
		var body app.GrantView
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if body.UserSub != "user-2" || body.Role != step.role {
			t.Fatalf("PUT role %s: grant view = %#v", step.role, body)
		}
	}
}

// The person comes from the path, as on a stack, so a body naming one too is
// refused rather than read.
func TestAssignPlatformRoleRejectsUserSubInBody(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.users = apiFakeUserRepository{users: []app.UserProfile{{Sub: "user-2", DisplayName: "bob"}}}
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, authenticatedRequest(http.MethodPut, platformGrantsPath+"/user-2", strings.NewReader(`{"role":"viewer","user_sub":"someone-else"}`)))

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusBadRequest, response.Body.String())
	}
}

func TestAssignPlatformRoleRejectsAStackRole(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.users = apiFakeUserRepository{users: []app.UserProfile{{Sub: "user-2", DisplayName: "bob"}}}
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, authenticatedRequest(http.MethodPut, platformGrantsPath+"/user-2", strings.NewReader(`{"role":"owner"}`)))

	// The caller's mistake, named on its field: a validation failure, not the
	// 500 OpenFGA's refusal would have become.
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusUnprocessableEntity, response.Body.String())
	}
	body := decodeProblem(t, response)
	if len(body.Errors) != 1 || body.Errors[0].Pointer != "#/role" || body.Errors[0].Issue != app.IssueUnsupported {
		t.Fatalf("errors = %+v, want one unsupported #/role", body.Errors)
	}
}

// An editor may create stacks and publish templates, and gets no further
// here: every verb is refused, the read included.
func TestPlatformGrantRoutesRequireManagePlatformAccess(t *testing.T) {
	t.Parallel()

	for _, request := range []struct {
		method, path, body string
	}{
		{http.MethodGet, platformGrantsPath, ""},
		{http.MethodPut, platformGrantsPath + "/user-2", `{"role":"admin"}`},
		{http.MethodDelete, platformGrantsPath + "/user-2", ""},
	} {
		deps := newBareAPITestDependencies(t).withPlatformTier("editor")
		deps.users = apiFakeUserRepository{users: []app.UserProfile{{Sub: "user-2", DisplayName: "bob"}}}
		server := NewServer(deps.service(), configuredTenantID)
		response := httptest.NewRecorder()

		server.ServeHTTP(response, authenticatedRequest(request.method, request.path, strings.NewReader(request.body)))

		if response.Code != http.StatusForbidden {
			t.Fatalf("%s %s: status = %d, want %d; body = %s", request.method, request.path, response.Code, http.StatusForbidden, response.Body.String())
		}
	}
}

func TestRevokePlatformRoleAnswers204(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.withGrants(testPlatformGrant(t, "user-3", authorization.RelationViewer))
	server := NewServer(deps.service(), configuredTenantID)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, authenticatedRequest(http.MethodDelete, platformGrantsPath+"/user-3", nil))

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusNoContent, response.Body.String())
	}
}
