package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vishu42/openplan/internal/authorization"
	"github.com/vishu42/openplan/internal/domain"
)

func mustPlatformGrant(t *testing.T, sub string, relation authorization.Relation) authorization.Grant {
	t.Helper()
	subject, err := authorization.SubjectFromOIDCSub(sub)
	if err != nil {
		t.Fatalf("SubjectFromOIDCSub: %v", err)
	}
	grant, err := authorization.NewGrant(subject, authorization.Platform, relation)
	if err != nil {
		t.Fatalf("NewGrant: %v", err)
	}
	return grant
}

// heldPlatformRoles is what the platform grants say sub holds, sorted, read
// back from the authorizer rather than from the service under test.
func heldPlatformRoles(t *testing.T, auth *authorization.Authorization, sub string) []string {
	t.Helper()
	grants, err := auth.ListGrants(context.Background(), authorization.Platform)
	if err != nil {
		t.Fatalf("ListGrants: %v", err)
	}
	var roles []string
	for _, grant := range grants {
		if grant.Subject().ID() == sub {
			roles = append(roles, grant.Relation().String())
		}
	}
	slices.Sort(roles)
	return roles
}

func newPlatformGrantService(auth *authorization.Authorization, work *recordingUnitOfWork, profiles ...UserProfile) *Service {
	return NewService(Service{Work: work, Authorization: auth, Users: &fakeUserRepository{users: profiles}, Clock: fixedClock{now: time.Now()}})
}

var casey = UserProfile{Sub: "user_456", DisplayName: "Casey Jones", Email: "casey@example.com"}

func TestAssignPlatformRoleWritesTheGrantInTheSameTransaction(t *testing.T) {
	t.Parallel()

	authorizer := testPlatformAuthorizer(t)
	work := newRecordingWork(nil)
	service := newPlatformGrantService(authorizer, work, casey)

	result, err := service.AssignPlatformRole(adminContext(), AssignPlatformRoleCommand{TenantID: "tenant_123", UserSub: "user_456", Role: "editor"})
	if err != nil {
		t.Fatalf("AssignPlatformRole returned error: %v", err)
	}
	if view := result.Grant; view.Role != "editor" || view.UserSub != "user_456" || view.DisplayName != "Casey Jones" || view.Email != "casey@example.com" {
		t.Fatalf("view = %#v", view)
	}
	if !result.Created {
		t.Fatal("Created = false for a person who held no platform role")
	}
	if len(work.requests) != 0 {
		t.Fatalf("enqueued %d requests, want 0 -- the grant is part of the commit", len(work.requests))
	}
	allowed, err := authorizer.Can(context.Background(), "user_456", authorization.RelationCanCreateStack, authorization.Platform)
	if err != nil {
		t.Fatalf("Can() error = %v", err)
	}
	if !allowed {
		t.Fatal("the assigned role is not in effect when AssignPlatformRole returns")
	}
}

func TestAssignPlatformRoleIsIdempotentForAHeldRole(t *testing.T) {
	t.Parallel()

	authorizer := seedGrants(t, testPlatformAuthorizer(t), mustPlatformGrant(t, "user_456", authorization.RelationEditor))
	service := newPlatformGrantService(authorizer, newRecordingWork(nil), casey)

	result, err := service.AssignPlatformRole(adminContext(), AssignPlatformRoleCommand{TenantID: "tenant_123", UserSub: "user_456", Role: "editor"})
	if err != nil {
		t.Fatalf("AssignPlatformRole returned error: %v", err)
	}
	if result.Created {
		t.Fatal("Created = true for a role the person already held")
	}
	if got := heldPlatformRoles(t, authorizer, "user_456"); !slices.Equal(got, []string{"editor"}) {
		t.Fatalf("held = %v, want [editor]", got)
	}
}

func TestAssignPlatformRoleReplacesAStaleRole(t *testing.T) {
	t.Parallel()

	authorizer := seedGrants(t, testPlatformAuthorizer(t), mustPlatformGrant(t, "user_456", authorization.RelationEditor))
	service := newPlatformGrantService(authorizer, newRecordingWork(nil), casey)

	result, err := service.AssignPlatformRole(adminContext(), AssignPlatformRoleCommand{TenantID: "tenant_123", UserSub: "user_456", Role: "admin"})
	if err != nil {
		t.Fatalf("AssignPlatformRole returned error: %v", err)
	}
	if result.Created {
		t.Fatal("Created = true for a person who already held a platform role")
	}
	if got := heldPlatformRoles(t, authorizer, "user_456"); !slices.Equal(got, []string{"admin"}) {
		t.Fatalf("held = %v, want [admin]", got)
	}
}

// Two roles can be held at once until #422 serializes grant writes; a PUT
// must still leave exactly the one it asked for.
func TestAssignPlatformRoleConvergesTwoHeldRoles(t *testing.T) {
	t.Parallel()

	authorizer := seedGrants(t, testPlatformAuthorizer(t),
		mustPlatformGrant(t, "user_456", authorization.RelationEditor),
		mustPlatformGrant(t, "user_456", authorization.RelationViewer))
	service := newPlatformGrantService(authorizer, newRecordingWork(nil), casey)

	if _, err := service.AssignPlatformRole(adminContext(), AssignPlatformRoleCommand{TenantID: "tenant_123", UserSub: "user_456", Role: "admin"}); err != nil {
		t.Fatalf("AssignPlatformRole returned error: %v", err)
	}
	if got := heldPlatformRoles(t, authorizer, "user_456"); !slices.Equal(got, []string{"admin"}) {
		t.Fatalf("held = %v, want [admin]", got)
	}
}

// GrantRelation admits the stack roles too. Without the platform's own list
// an owner would reach OpenFGA, which refuses it, and surface as a 500.
func TestAssignPlatformRoleRejectsAStackRole(t *testing.T) {
	t.Parallel()

	authorizer := testPlatformAuthorizer(t)
	service := newPlatformGrantService(authorizer, newRecordingWork(nil), casey)

	_, err := service.AssignPlatformRole(adminContext(), AssignPlatformRoleCommand{TenantID: "tenant_123", UserSub: "user_456", Role: "owner"})
	if !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("error = %v, want ErrInvalidCommand", err)
	}
	assertFieldIssues(t, err, "role unsupported")
	if !strings.Contains(err.Error(), "role must be one the platform can grant") {
		t.Fatalf("error = %q, want it to say which roles are grantable", err)
	}
	if got := heldPlatformRoles(t, authorizer, "user_456"); len(got) != 0 {
		t.Fatalf("held = %v, want nothing written", got)
	}
}

func TestAssignPlatformRoleReportsRoleAndSubTogether(t *testing.T) {
	t.Parallel()

	service := newPlatformGrantService(testPlatformAuthorizer(t), newRecordingWork(nil))
	_, err := service.AssignPlatformRole(adminContext(), AssignPlatformRoleCommand{TenantID: "tenant_123", UserSub: "", Role: "root"})
	if !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("error = %v, want ErrInvalidCommand", err)
	}
	assertFieldIssues(t, err, "role unsupported", "user_sub invalid_format")
}

func TestAssignPlatformRoleRejectsUserThatNeverSignedIn(t *testing.T) {
	t.Parallel()

	service := newPlatformGrantService(testPlatformAuthorizer(t), newRecordingWork(nil))
	_, err := service.AssignPlatformRole(adminContext(), AssignPlatformRoleCommand{TenantID: "tenant_123", UserSub: "never-signed-in", Role: "viewer"})
	if !errors.Is(err, ErrUserNotProvisioned) {
		t.Fatalf("error = %v, want ErrUserNotProvisioned", err)
	}
	assertFieldIssues(t, err, "user_sub not_signed_in")
}

// An editor can create stacks and publish templates, and must not hand out
// either power.
func TestPlatformGrantsRequireManagePlatformAccess(t *testing.T) {
	t.Parallel()

	service := newPlatformGrantService(testPlatformAuthorizer(t), newRecordingWork(nil), casey)
	editor := platformContext("user_123")

	if _, err := service.ListPlatformGrants(editor, ListPlatformGrantsCommand{TenantID: "tenant_123"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("ListPlatformGrants error = %v, want ErrForbidden", err)
	}
	if _, err := service.AssignPlatformRole(editor, AssignPlatformRoleCommand{TenantID: "tenant_123", UserSub: "user_456", Role: "admin"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("AssignPlatformRole error = %v, want ErrForbidden", err)
	}
	if err := service.RevokePlatformRole(editor, RevokePlatformRoleCommand{TenantID: "tenant_123", UserSub: "admin_123"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("RevokePlatformRole error = %v, want ErrForbidden", err)
	}
}

// The convergence reads one object's grants; a platform revoke must never
// reach a stack's.
func TestRevokePlatformRoleLeavesStackGrants(t *testing.T) {
	t.Parallel()

	authorizer := seedGrants(t, testPlatformAuthorizer(t),
		mustPlatformGrant(t, "user_456", authorization.RelationEditor),
		mustGrant(t, "user_456", "stack_abc", authorization.RelationOwner))
	service := newPlatformGrantService(authorizer, newRecordingWork(nil), casey)

	if err := service.RevokePlatformRole(adminContext(), RevokePlatformRoleCommand{TenantID: "tenant_123", UserSub: "user_456"}); err != nil {
		t.Fatalf("RevokePlatformRole returned error: %v", err)
	}
	if got := heldPlatformRoles(t, authorizer, "user_456"); len(got) != 0 {
		t.Fatalf("platform roles held = %v, want none", got)
	}
	owns, err := authorizer.Can(context.Background(), "user_456", authorization.RelationCanManageAccess, mustStackObject(t, "stack_abc"))
	if err != nil {
		t.Fatalf("Can() error = %v", err)
	}
	if !owns {
		t.Fatal("revoking a platform role removed a stack grant")
	}
}

func TestRevokePlatformRoleRemovesBothHeldRoles(t *testing.T) {
	t.Parallel()

	authorizer := seedGrants(t, testPlatformAuthorizer(t),
		mustPlatformGrant(t, "user_456", authorization.RelationEditor),
		mustPlatformGrant(t, "user_456", authorization.RelationViewer))
	service := newPlatformGrantService(authorizer, newRecordingWork(nil), casey)

	if err := service.RevokePlatformRole(adminContext(), RevokePlatformRoleCommand{TenantID: "tenant_123", UserSub: "user_456"}); err != nil {
		t.Fatalf("RevokePlatformRole returned error: %v", err)
	}
	if got := heldPlatformRoles(t, authorizer, "user_456"); len(got) != 0 {
		t.Fatalf("held = %v, want none", got)
	}
}

func TestRevokePlatformRoleIsANoOpWhenNothingIsHeld(t *testing.T) {
	t.Parallel()

	service := newPlatformGrantService(testPlatformAuthorizer(t), newRecordingWork(nil), casey)
	if err := service.RevokePlatformRole(adminContext(), RevokePlatformRoleCommand{TenantID: "tenant_123", UserSub: "user_456"}); err != nil {
		t.Fatalf("RevokePlatformRole returned error: %v", err)
	}
}

// Root can't be revoked here, so the platform stays administrable whoever
// gives up admin, the last one included.
func TestRevokePlatformRoleAllowsTheLastAdmin(t *testing.T) {
	t.Parallel()

	authorizer := newPlatformAuthorizer(t, platformAdmin("admin_123"))
	service := newPlatformGrantService(authorizer, newRecordingWork(nil))

	if err := service.RevokePlatformRole(adminContext(), RevokePlatformRoleCommand{TenantID: "tenant_123", UserSub: "admin_123"}); err != nil {
		t.Fatalf("RevokePlatformRole returned error: %v", err)
	}
	allowed, err := authorizer.Can(context.Background(), "admin_123", authorization.RelationCanManagePlatformAccess, authorization.Platform)
	if err != nil {
		t.Fatalf("Can() error = %v", err)
	}
	if allowed {
		t.Fatal("the revoked admin can still manage platform access")
	}
}

func TestListPlatformGrantsFallsBackToTheSub(t *testing.T) {
	t.Parallel()

	authorizer := newPlatformAuthorizer(t, platformAdmin("admin_123"), platformEditor("ghost"))
	service := newPlatformGrantService(authorizer, newRecordingWork(nil), UserProfile{Sub: "admin_123", DisplayName: "Ada Admin", Email: "ada@example.com"})

	result, err := service.ListPlatformGrants(adminContext(), ListPlatformGrantsCommand{TenantID: "tenant_123"})
	if err != nil {
		t.Fatalf("ListPlatformGrants returned error: %v", err)
	}
	got := map[string]GrantView{}
	for _, grant := range result.Grants {
		got[grant.UserSub] = grant
	}
	if len(got) != 2 {
		t.Fatalf("grants = %#v, want admin_123 and ghost", result.Grants)
	}
	if view := got["admin_123"]; view.Role != "admin" || view.DisplayName != "Ada Admin" || view.Email != "ada@example.com" {
		t.Fatalf("admin_123 = %#v", view)
	}
	if view := got["ghost"]; view.Role != "editor" || view.DisplayName != "ghost" {
		t.Fatalf("ghost = %#v, want the sub as its display name", view)
	}
}

func TestPlatformGrantsAppendAnAuditEvent(t *testing.T) {
	t.Parallel()

	authorizer := seedGrants(t, testPlatformAuthorizer(t), mustPlatformGrant(t, "user_456", authorization.RelationViewer))
	work := newRecordingWork(nil)
	service := newPlatformGrantService(authorizer, work, casey)

	if _, err := service.AssignPlatformRole(adminContext(), AssignPlatformRoleCommand{TenantID: "tenant_123", UserSub: "user_456", Role: "editor"}); err != nil {
		t.Fatalf("AssignPlatformRole returned error: %v", err)
	}
	if err := service.RevokePlatformRole(adminContext(), RevokePlatformRoleCommand{TenantID: "tenant_123", UserSub: "user_456"}); err != nil {
		t.Fatalf("RevokePlatformRole returned error: %v", err)
	}
	want := []domain.SecurityAuditEvent{
		{ActorSubject: "admin_123", Action: domain.AuditActionGrant, TargetUser: "user_456", TenantID: "tenant_123", OldRole: "viewer", NewRole: "editor", Outcome: domain.AuditOutcomeSuccess},
		{ActorSubject: "admin_123", Action: domain.AuditActionRevoke, TargetUser: "user_456", TenantID: "tenant_123", OldRole: "editor", Outcome: domain.AuditOutcomeSuccess},
	}
	if !slices.Equal(work.audits, want) {
		t.Fatalf("audits = %#v, want %#v", work.audits, want)
	}
}
