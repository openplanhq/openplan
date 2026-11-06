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

// approvalFixture is a run waiting for approval, by a caller allowed to approve
// it. Each case changes one thing so that the state says no.
func approvalFixture(t *testing.T) (*Service, *recordingTemplateRunRepository, *recordingStackTemplateRepository, *recordingUnitOfWork, *recordingAuditRepository) {
	t.Helper()
	runs := &recordingTemplateRunRepository{run: domain.TemplateRun{
		ID:              "run_123",
		TenantID:        "tenant_123",
		StackTemplateID: "stack_template_123",
		Status:          domain.TemplateRunWaitingApproval,
	}}
	templates := approvableStackTemplates()
	work := &recordingUnitOfWork{templateRuns: runs}
	audit := &recordingAuditRepository{}
	service := NewService(Service{
		Users:                    &fakeUserRepository{},
		Authorization:            testPlatformAuthorizer(t),
		Work:                     work,
		TemplateRuns:             runs,
		StackTemplates:           templates,
		TemplateRevisionMetadata: &recordingTemplateRevisionRepository{},
		Audit:                    audit,
	})
	return service, runs, templates, work, audit
}

func TestApproveRunAuditsARejection(t *testing.T) {
	t.Parallel()

	var stale *PlanStaleError
	for _, test := range []struct {
		name   string
		reject func(*recordingTemplateRunRepository, *recordingStackTemplateRepository)
		is     func(error) bool
		reason domain.AuditReason
	}{
		{
			"run isn't waiting",
			func(runs *recordingTemplateRunRepository, _ *recordingStackTemplateRepository) {
				runs.run.Status = domain.TemplateRunCompleted
			},
			func(err error) bool { return errors.Is(err, ErrRunNotApprovable) },
			domain.AuditReasonRunNotApprovable,
		},
		{
			"plan is stale",
			func(_ *recordingTemplateRunRepository, templates *recordingStackTemplateRepository) {
				templates.stackTemplate.PendingPlanRunID = "run_other"
			},
			func(err error) bool { return errors.As(err, &stale) },
			domain.AuditReasonPlanStale,
		},
		{
			// Two approvers both saw it waiting; the store lets one through and
			// fails the other inside the transaction, which rolls back.
			"approval raced another",
			func(runs *recordingTemplateRunRepository, _ *recordingStackTemplateRepository) {
				runs.approvalErr = ErrRunNotApprovable
			},
			func(err error) bool { return errors.Is(err, ErrRunNotApprovable) },
			domain.AuditReasonRunNotApprovable,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, runs, templates, work, audit := approvalFixture(t)
			test.reject(runs, templates)

			_, err := service.ApproveRun(authenticatedContext(), ApproveRunCommand{TenantID: "tenant_123", RunID: "run_123"})
			if !test.is(err) {
				t.Fatalf("error = %v", err)
			}
			if len(work.audits) != 0 {
				t.Fatalf("transaction audit events = %+v, want none", work.audits)
			}
			if len(audit.events) != 1 {
				t.Fatalf("audit events = %+v, want one", audit.events)
			}
			want := domain.SecurityAuditEvent{
				ActorSubject: oidcSubject,
				Action:       domain.AuditActionApprovalRejected,
				TenantID:     "tenant_123",
				StackID:      "stack_123",
				Outcome:      domain.AuditOutcomeFailure,
				Operation:    domain.AuditOperationApproveRun,
				Reason:       test.reason,
				TargetID:     "run_123",
			}
			if got := audit.events[0]; got != want {
				t.Fatalf("audit event =\n%+v, want\n%+v", got, want)
			}
		})
	}
}

// A failure that isn't about the run's state, like the store being down, says
// nothing about the approval, so it isn't recorded as rejected.
func TestApproveRunRecordsNoRejectionForAnOutage(t *testing.T) {
	t.Parallel()

	service, runs, _, _, audit := approvalFixture(t)
	runs.approvalErr = errors.New("database down")

	if _, err := service.ApproveRun(authenticatedContext(), ApproveRunCommand{TenantID: "tenant_123", RunID: "run_123"}); err == nil {
		t.Fatal("ApproveRun returned no error")
	}
	if len(audit.events) != 0 {
		t.Fatalf("audit events = %+v, want none", audit.events)
	}
}
