package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vishu42/openplan/internal/authentication"
	"github.com/vishu42/openplan/internal/authorization"
	"github.com/vishu42/openplan/internal/domain"
)

var guardAttempt = writeAttempt{
	Operation: domain.AuditOperationDiscardRun,
	TenantID:  "tenant_123",
	TargetID:  "target_123",
}

// guardCall runs one guard the way a write would. Every guard but the platform
// one is reached through stack_123, which newWriteFixture's rows belong to.
type guardCall struct {
	name string
	call func(service *Service, ctx context.Context) (authentication.Principal, error)
}

var stackGuards = []guardCall{
	{"guardStackWrite", func(service *Service, ctx context.Context) (authentication.Principal, error) {
		return service.guardStackWrite(ctx, guardAttempt, "stack_123", authorization.RelationCanOperate)
	}},
	{"guardStackTemplateWrite", func(service *Service, ctx context.Context) (authentication.Principal, error) {
		principal, _, err := service.guardStackTemplateWrite(ctx, guardAttempt, "stack_template_123", authorization.RelationCanOperate)
		return principal, err
	}},
	{"guardRunWrite", func(service *Service, ctx context.Context) (authentication.Principal, error) {
		principal, _, err := service.guardRunWrite(ctx, guardAttempt, "run_123", authorization.RelationCanOperate)
		return principal, err
	}},
}

var platformGuard = guardCall{"guardPlatformWrite", func(service *Service, ctx context.Context) (authentication.Principal, error) {
	return service.guardPlatformWrite(ctx, guardAttempt, authorization.RelationCanManageTemplates)
}}

func withRecordedAudit(fixture writeFixture) (writeFixture, *recordingAuditRepository) {
	audit := &recordingAuditRepository{}
	fixture.service.Audit = audit
	return fixture, audit
}

func assertRefusalEvent(t *testing.T, audit *recordingAuditRepository, reason domain.AuditReason, stackID domain.StackID) {
	t.Helper()
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %+v, want one", audit.events)
	}
	event := audit.events[0]
	if event.Action != domain.AuditActionFailedAccessAttempt || event.Outcome != domain.AuditOutcomeFailure {
		t.Fatalf("action, outcome = %q, %q", event.Action, event.Outcome)
	}
	if event.ActorSubject != oidcSubject || event.TenantID != guardAttempt.TenantID {
		t.Fatalf("actor, tenant = %q, %q", event.ActorSubject, event.TenantID)
	}
	if event.Operation != guardAttempt.Operation || event.TargetID != guardAttempt.TargetID {
		t.Fatalf("operation, target = %q, %q", event.Operation, event.TargetID)
	}
	if event.Reason != reason || event.StackID != stackID {
		t.Fatalf("reason, stack = %q, %q, want %q, %q", event.Reason, event.StackID, reason, stackID)
	}
}

func TestGuardsAuditARefusal(t *testing.T) {
	t.Parallel()

	reasons := map[error]domain.AuditReason{
		ErrNotFound:  domain.AuditReasonNotFound,
		ErrForbidden: domain.AuditReasonForbidden,
	}
	for _, guard := range stackGuards {
		for _, refusal := range writeRefusals {
			t.Run(guard.name+"/"+refusal.name, func(t *testing.T) {
				t.Parallel()

				auth := newTestAuthorization(t)
				refusal.grant(t, auth, oidcSubject, "stack_123")
				fixture, audit := withRecordedAudit(newWriteFixture(auth))

				_, err := guard.call(fixture.service, authenticatedContext())
				if !errors.Is(err, refusal.want) {
					t.Fatalf("error = %v, want %v", err, refusal.want)
				}
				// The row loaded, so the event can name the stack even where
				// the caller is only told it isn't there.
				assertRefusalEvent(t, audit, reasons[refusal.want], "stack_123")
			})
		}
	}

	t.Run(platformGuard.name, func(t *testing.T) {
		t.Parallel()

		fixture, audit := withRecordedAudit(newWriteFixture(newPlatformAuthorizer(t, platformViewer(oidcSubject))))
		_, err := platformGuard.call(fixture.service, authenticatedContext())
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("error = %v, want ErrForbidden", err)
		}
		assertRefusalEvent(t, audit, domain.AuditReasonForbidden, "")
	})
}

func TestGuardsLeaveStackIDEmptyWhenTheRowIsMissing(t *testing.T) {
	t.Parallel()

	for name, breakRow := range map[string]func(writeFixture){
		"stack template": func(fixture writeFixture) { fixture.templates.getErr = ErrNotFound },
		"run":            func(fixture writeFixture) { fixture.runs.getErr = ErrNotFound },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fixture, audit := withRecordedAudit(newWriteFixture(newPlatformAuthorizer(t, platformAdmin(oidcSubject))))
			breakRow(fixture)

			var err error
			if name == "run" {
				_, _, err = fixture.service.guardRunWrite(authenticatedContext(), guardAttempt, "run_123", authorization.RelationCanOperate)
			} else {
				_, _, err = fixture.service.guardStackTemplateWrite(authenticatedContext(), guardAttempt, "stack_template_123", authorization.RelationCanOperate)
			}
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("error = %v, want ErrNotFound", err)
			}
			assertRefusalEvent(t, audit, domain.AuditReasonNotFound, "")
		})
	}
}

func TestGuardsPassAnAllowedCaller(t *testing.T) {
	t.Parallel()

	auth := newPlatformAuthorizer(t, platformAdmin(oidcSubject))
	grantStack(t, auth, oidcSubject, "stack_123", authorization.RelationOwner)
	fixture, audit := withRecordedAudit(newWriteFixture(auth))

	for _, guard := range append([]guardCall{platformGuard}, stackGuards...) {
		principal, err := guard.call(fixture.service, authenticatedContext())
		if err != nil {
			t.Fatalf("%s returned error: %v", guard.name, err)
		}
		if principal.Subject != oidcSubject {
			t.Fatalf("%s principal = %q", guard.name, principal.Subject)
		}
	}
	_, stackTemplate, err := fixture.service.guardStackTemplateWrite(authenticatedContext(), guardAttempt, "stack_template_123", authorization.RelationCanOperate)
	if err != nil || stackTemplate.ID != "stack_template_123" {
		t.Fatalf("stack template = %q, err = %v", stackTemplate.ID, err)
	}
	_, run, err := fixture.service.guardRunWrite(authenticatedContext(), guardAttempt, "run_123", authorization.RelationCanOperate)
	if err != nil || run.ID != "run_123" {
		t.Fatalf("run = %q, err = %v", run.ID, err)
	}
	if len(audit.events) != 0 {
		t.Fatalf("audit events = %+v, want none", audit.events)
	}
}

// An outage isn't a decision about the caller, so it must not read as one: a
// failed_access_attempt row would accuse someone who did nothing wrong.
func TestGuardsWriteNothingForAnOutage(t *testing.T) {
	t.Parallel()

	down := errors.New("database down")
	for name, breakRow := range map[string]func(writeFixture){
		"stack template": func(fixture writeFixture) { fixture.templates.getErr = down },
		"run":            func(fixture writeFixture) { fixture.runs.getErr = down },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fixture, audit := withRecordedAudit(newWriteFixture(newPlatformAuthorizer(t, platformAdmin(oidcSubject))))
			breakRow(fixture)
			var err error
			if name == "run" {
				_, _, err = fixture.service.guardRunWrite(authenticatedContext(), guardAttempt, "run_123", authorization.RelationCanOperate)
			} else {
				_, _, err = fixture.service.guardStackTemplateWrite(authenticatedContext(), guardAttempt, "stack_template_123", authorization.RelationCanOperate)
			}
			if !errors.Is(err, down) {
				t.Fatalf("error = %v, want the outage", err)
			}
			if len(audit.events) != 0 {
				t.Fatalf("audit events = %+v, want none", audit.events)
			}
		})
	}

	for _, guard := range append([]guardCall{platformGuard}, stackGuards...) {
		t.Run(guard.name+"/no authorizer", func(t *testing.T) {
			t.Parallel()

			audit := &recordingAuditRepository{}
			service := NewService(Service{Audit: audit})
			_, err := guard.call(service, authenticatedContext())
			if err == nil || errors.Is(err, ErrForbidden) || errors.Is(err, ErrNotFound) {
				t.Fatalf("error = %v, want one that isn't a refusal", err)
			}
			if len(audit.events) != 0 {
				t.Fatalf("audit events = %+v, want none", audit.events)
			}
		})
	}
}

func TestGuardsWriteNothingWhenUnauthenticated(t *testing.T) {
	t.Parallel()

	for _, guard := range append([]guardCall{platformGuard}, stackGuards...) {
		t.Run(guard.name, func(t *testing.T) {
			t.Parallel()

			fixture, audit := withRecordedAudit(newWriteFixture(newTestAuthorization(t)))
			_, err := guard.call(fixture.service, context.Background())
			if !errors.Is(err, ErrUnauthenticated) {
				t.Fatalf("error = %v, want ErrUnauthenticated", err)
			}
			if len(audit.events) != 0 {
				t.Fatalf("audit events = %+v, want none", audit.events)
			}
		})
	}
}

// A path ID reaches the app decoded, and Postgres text rejects NUL and invalid
// UTF-8. Uncleaned, %00 in a URL would fail the insert and leave no event.
func TestRefusalEventIsCleanedForPostgres(t *testing.T) {
	t.Parallel()

	for name, value := range map[string]string{
		"nul":                  "a\x00b",
		"invalid utf-8":        "a\xffb",
		"long":                 strings.Repeat("a", 1<<20),
		"long multi-byte runes": strings.Repeat("é", 1<<10),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fixture, audit := withRecordedAudit(newWriteFixture(newTestAuthorization(t)))
			attempt := guardAttempt
			attempt.TargetID = value
			_, err := fixture.service.guardStackWrite(authenticatedContext(), attempt, domain.StackID(value), authorization.RelationCanOperate)
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("error = %v, want ErrNotFound", err)
			}
			if len(audit.events) != 1 {
				t.Fatalf("audit events = %d, want 1", len(audit.events))
			}
			for field, got := range map[string]string{"target_id": audit.events[0].TargetID, "stack_id": string(audit.events[0].StackID)} {
				if strings.ContainsRune(got, 0) || !utf8.ValidString(got) || len(got) > 256 {
					t.Fatalf("%s isn't clean: %d bytes, %q", field, len(got), got[:min(len(got), 32)])
				}
			}
		})
	}
}

// contextHonouringAudit fails the way a real insert does on a dead context.
type contextHonouringAudit struct{ recordingAuditRepository }

func (audit *contextHonouringAudit) AppendAuditEvent(ctx context.Context, event domain.SecurityAuditEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return audit.recordingAuditRepository.AppendAuditEvent(ctx, event)
}

// A caller who hangs up as they're refused cancels the request's context, which
// must not erase the record of the attempt.
func TestRefusalIsAuditedAfterTheClientDisconnects(t *testing.T) {
	t.Parallel()

	audit := &contextHonouringAudit{}
	service := NewService(Service{Audit: audit})
	ctx, cancel := context.WithCancel(authenticatedContext())
	cancel()

	service.auditError(ctx, domain.SecurityAuditEvent{ActorSubject: oidcSubject, Outcome: domain.AuditOutcomeFailure})
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(audit.events))
	}
}
