package app

import (
	"errors"
	"testing"

	"github.com/vishu42/openplan/internal/authorization"
	"github.com/vishu42/openplan/internal/domain"
)

// lastOwnerFixture is a stack whose only owner is the caller, so any change to
// their own role is rejected. Permission passes: this is the state saying no.
func lastOwnerFixture(t *testing.T) (*Service, *recordingUnitOfWork, *recordingAuditRepository) {
	t.Helper()
	auth := newTestAuthorization(t)
	grantStack(t, auth, oidcSubject, "stack_123", authorization.RelationOwner)
	work := &recordingUnitOfWork{}
	audit := &recordingAuditRepository{}
	service := NewService(Service{
		Authorization: auth,
		Work:          work,
		Users:         &fakeUserRepository{users: []UserProfile{{Sub: oidcSubject}}},
		Audit:         audit,
	})
	return service, work, audit
}

func assertLastOwnerRejection(t *testing.T, work *recordingUnitOfWork, audit *recordingAuditRepository, want domain.SecurityAuditEvent) {
	t.Helper()
	// The transaction rolls back, so an event written through it would be erased.
	if len(work.audits) != 0 {
		t.Fatalf("transaction audit events = %+v, want none", work.audits)
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %+v, want one", audit.events)
	}
	if got := audit.events[0]; got != want {
		t.Fatalf("audit event =\n%+v, want\n%+v", got, want)
	}
}

func TestAssignStackRoleAuditsALastOwnerRejection(t *testing.T) {
	t.Parallel()

	service, work, audit := lastOwnerFixture(t)
	_, err := service.AssignStackRole(authenticatedContext(), AssignStackRoleCommand{
		TenantID: "tenant_123", StackID: "stack_123", UserSub: oidcSubject, Role: "viewer",
	})
	if !errors.Is(err, ErrLastOwner) {
		t.Fatalf("error = %v, want ErrLastOwner", err)
	}
	assertLastOwnerRejection(t, work, audit, domain.SecurityAuditEvent{
		ActorSubject: oidcSubject,
		Action:       domain.AuditActionGrant,
		TargetUser:   oidcSubject,
		TenantID:     "tenant_123",
		StackID:      "stack_123",
		OldRole:      "owner",
		NewRole:      "viewer",
		Outcome:      domain.AuditOutcomeFailure,
		Operation:    domain.AuditOperationAssignStackRole,
		Reason:       domain.AuditReasonLastOwner,
		TargetID:     oidcSubject,
	})
}

func TestRevokeStackRoleAuditsALastOwnerRejection(t *testing.T) {
	t.Parallel()

	service, work, audit := lastOwnerFixture(t)
	err := service.RevokeStackRole(authenticatedContext(), RevokeStackRoleCommand{
		TenantID: "tenant_123", StackID: "stack_123", UserSub: oidcSubject,
	})
	if !errors.Is(err, ErrLastOwner) {
		t.Fatalf("error = %v, want ErrLastOwner", err)
	}
	assertLastOwnerRejection(t, work, audit, domain.SecurityAuditEvent{
		ActorSubject: oidcSubject,
		Action:       domain.AuditActionRevoke,
		TargetUser:   oidcSubject,
		TenantID:     "tenant_123",
		StackID:      "stack_123",
		OldRole:      "owner",
		Outcome:      domain.AuditOutcomeFailure,
		Operation:    domain.AuditOperationRevokeStackRole,
		Reason:       domain.AuditReasonLastOwner,
		TargetID:     oidcSubject,
	})
}

// A role change that goes through writes its success event, and nothing else.
func TestAnAllowedRoleChangeWritesNoRejection(t *testing.T) {
	t.Parallel()

	service, work, audit := lastOwnerFixture(t)
	if _, err := service.AssignStackRole(authenticatedContext(), AssignStackRoleCommand{
		TenantID: "tenant_123", StackID: "stack_123", UserSub: oidcSubject, Role: "owner",
	}); err != nil {
		t.Fatalf("AssignStackRole returned error: %v", err)
	}
	if len(audit.events) != 0 {
		t.Fatalf("audit events = %+v, want none", audit.events)
	}
	if len(work.audits) != 1 || work.audits[0].Outcome != domain.AuditOutcomeSuccess {
		t.Fatalf("transaction audit events = %+v, want one success", work.audits)
	}
}

