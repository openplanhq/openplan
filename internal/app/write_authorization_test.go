package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/vishu42/openplan/internal/authorization"
	"github.com/vishu42/openplan/internal/domain"
)

// writeRefusal is one way a write can be refused before it does anything. A
// caller with no relation to the stack can't view it and is told it isn't
// there; a viewer can see it and is told they may not act. Answering 403 to
// the first would confirm that a stack the caller can't see exists (#387).
type writeRefusal struct {
	name  string
	grant func(t *testing.T, auth *authorization.Authorization, sub, stackID string)
	want  error
}

var writeRefusals = []writeRefusal{
	{
		name:  "no view",
		grant: func(*testing.T, *authorization.Authorization, string, string) {},
		want:  ErrNotFound,
	},
	{
		// viewer satisfies can_view and nothing else, so it is the one role
		// that can see a stack without being able to write to it.
		name: "view only",
		grant: func(t *testing.T, auth *authorization.Authorization, sub, stackID string) {
			grantStack(t, auth, sub, stackID, authorization.RelationViewer)
		},
		want: ErrForbidden,
	},
}

// writeTarget says how a write path reaches its stack, which decides what
// "this object doesn't exist" means for it.
type writeTarget int

const (
	viaStack writeTarget = iota
	viaStackTemplate
	viaRun
)

type writePath struct {
	name   string
	target writeTarget
	call   func(service *Service, ctx context.Context, stackID domain.StackID) error
}

// writePaths is every stack-scoped write, plus the credential and grant reads
// that need more than can_view: those refuse a viewer with 403 too.
var writePaths = []writePath{
	{"AddTemplateToStack", viaStack, func(service *Service, ctx context.Context, stackID domain.StackID) error {
		_, err := service.AddTemplateToStack(ctx, AddTemplateToStackCommand{
			TenantID: "tenant_123", StackID: stackID, TemplateRevisionID: "template_123",
			ConfigJSON: json.RawMessage(`{}`),
		})
		return err
	}},
	{"StartTemplateRun", viaStackTemplate, func(service *Service, ctx context.Context, _ domain.StackID) error {
		_, err := service.StartTemplateRun(ctx, StartTemplateRunCommand{
			TenantID: "tenant_123", StackTemplateID: "stack_template_123", Operation: domain.OperationPlan,
		})
		return err
	}},
	{"UpdateStackTemplateConfig", viaStackTemplate, func(service *Service, ctx context.Context, _ domain.StackID) error {
		_, err := service.UpdateStackTemplateConfig(ctx, UpdateStackTemplateConfigCommand{
			TenantID: "tenant_123", StackTemplateID: "stack_template_123", ConfigJSON: json.RawMessage(`{}`),
		})
		return err
	}},
	{"UpgradeStackTemplate", viaStackTemplate, func(service *Service, ctx context.Context, _ domain.StackID) error {
		_, err := service.UpgradeStackTemplate(ctx, UpgradeStackTemplateCommand{
			TenantID: "tenant_123", StackTemplateID: "stack_template_123", TargetTemplateRevisionID: "template_456",
		})
		return err
	}},
	{"CreateCredential/stack", viaStack, func(service *Service, ctx context.Context, stackID domain.StackID) error {
		_, err := service.CreateCredential(ctx, CreateCredentialCommand{
			TenantID: "tenant_123", Scope: domain.CredentialScopeStack, StackID: stackID, Name: "AWS_REGION", Value: "us-east-1",
		})
		return err
	}},
	{"CreateCredential/stack template", viaStackTemplate, func(service *Service, ctx context.Context, _ domain.StackID) error {
		_, err := service.CreateCredential(ctx, CreateCredentialCommand{
			TenantID: "tenant_123", Scope: domain.CredentialScopeStackTemplate, StackTemplateID: "stack_template_123", Name: "AWS_REGION", Value: "us-east-1",
		})
		return err
	}},
	{"ListCredentials/stack", viaStack, func(service *Service, ctx context.Context, stackID domain.StackID) error {
		_, err := service.ListCredentials(ctx, ListCredentialsCommand{
			TenantID: "tenant_123", Scope: domain.CredentialScopeStack, StackID: stackID,
		})
		return err
	}},
	{"ListCredentials/stack template", viaStackTemplate, func(service *Service, ctx context.Context, _ domain.StackID) error {
		_, err := service.ListCredentials(ctx, ListCredentialsCommand{
			TenantID: "tenant_123", Scope: domain.CredentialScopeStackTemplate, StackTemplateID: "stack_template_123",
		})
		return err
	}},
	{"DeleteCredential/stack", viaStack, func(service *Service, ctx context.Context, stackID domain.StackID) error {
		return service.DeleteCredential(ctx, DeleteCredentialCommand{
			TenantID: "tenant_123", StackID: stackID, CredentialID: "credential_123",
		})
	}},
	{"DeleteCredential/stack template", viaStackTemplate, func(service *Service, ctx context.Context, _ domain.StackID) error {
		return service.DeleteCredential(ctx, DeleteCredentialCommand{
			TenantID: "tenant_123", StackTemplateID: "stack_template_123", CredentialID: "credential_123",
		})
	}},
	{"ListStackGrants", viaStack, func(service *Service, ctx context.Context, stackID domain.StackID) error {
		_, err := service.ListStackGrants(ctx, ListStackGrantsCommand{TenantID: "tenant_123", StackID: stackID})
		return err
	}},
	{"AssignStackRole", viaStack, func(service *Service, ctx context.Context, stackID domain.StackID) error {
		_, err := service.AssignStackRole(ctx, AssignStackRoleCommand{
			TenantID: "tenant_123", StackID: stackID, UserSub: "user_456", Role: "viewer",
		})
		return err
	}},
	{"RevokeStackRole", viaStack, func(service *Service, ctx context.Context, stackID domain.StackID) error {
		return service.RevokeStackRole(ctx, RevokeStackRoleCommand{TenantID: "tenant_123", StackID: stackID, UserSub: "user_456"})
	}},
	{"ApproveRun", viaRun, func(service *Service, ctx context.Context, _ domain.StackID) error {
		_, err := service.ApproveRun(ctx, ApproveRunCommand{TenantID: "tenant_123", RunID: "run_123"})
		return err
	}},
	{"DiscardRun", viaRun, func(service *Service, ctx context.Context, _ domain.StackID) error {
		return service.DiscardRun(ctx, DiscardRunCommand{TenantID: "tenant_123", RunID: "run_123"})
	}},
}

// writeFixture is a service whose stack template and run both belong to
// stack_123, with every dependency a refused write touches before it stops.
type writeFixture struct {
	service   *Service
	templates *recordingStackTemplateRepository
	runs      *recordingTemplateRunRepository
}

func newWriteFixture(auth *authorization.Authorization) writeFixture {
	templates := &recordingStackTemplateRepository{stackTemplate: domain.StackTemplate{
		ID:                        "stack_template_123",
		TenantID:                  "tenant_123",
		StackID:                   "stack_123",
		Lifecycle:                 domain.StackTemplateActive,
		DesiredTemplateRevisionID: "template_123",
	}}
	runs := &recordingTemplateRunRepository{run: domain.TemplateRun{
		ID:              "run_123",
		TenantID:        "tenant_123",
		StackTemplateID: "stack_template_123",
		Status:          domain.TemplateRunWaitingApproval,
	}}
	service := NewService(Service{
		Authorization:    auth,
		StackTemplates:   templates,
		TemplateRuns:     runs,
		Credentials:      refusingCredentialRepository{},
		CredentialCipher: refusingCredentialCipher{},
		Audit:            &recordingAuditRepository{},
		Clock:            fixedClock{now: time.Now()},
	})
	return writeFixture{service: service, templates: templates, runs: runs}
}

func TestWritesHideUnseenObjects(t *testing.T) {
	t.Parallel()

	for _, path := range writePaths {
		for _, refusal := range writeRefusals {
			t.Run(path.name+"/"+refusal.name, func(t *testing.T) {
				t.Parallel()

				auth := newTestAuthorization(t)
				refusal.grant(t, auth, oidcSubject, "stack_123")
				fixture := newWriteFixture(auth)

				err := path.call(fixture.service, authenticatedContext(), "stack_123")
				if !errors.Is(err, refusal.want) {
					t.Fatalf("error = %v, want %v", err, refusal.want)
				}
			})
		}

		// A platform administrator can view every stack that exists, so a
		// 404 here can only mean the object isn't there, never a denial.
		t.Run(path.name+"/missing", func(t *testing.T) {
			t.Parallel()

			fixture := newWriteFixture(newPlatformAuthorizer(t, platformAdmin(oidcSubject)))
			stackID := domain.StackID("stack_123")
			switch path.target {
			case viaStack:
				// Never created, so it has no parent edge for the
				// administrator to reach it through.
				stackID = "stack_missing"
			case viaStackTemplate:
				fixture.templates.getErr = ErrNotFound
			case viaRun:
				fixture.runs.getErr = ErrNotFound
			}

			err := path.call(fixture.service, authenticatedContext(), stackID)
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("error = %v, want ErrNotFound", err)
			}
		})
	}
}

// Auto-approve asks for can_approve after operate has already passed, so the
// caller can see the stack and the refusal stays ErrForbidden. It is audited
// like every other refused operable write.
func TestStartTemplateRunAutoApproveWithoutApproveIsForbidden(t *testing.T) {
	t.Parallel()

	auth := newTestAuthorization(t)
	grantStack(t, auth, oidcSubject, "stack_123", authorization.RelationOperator)
	fixture := newWriteFixture(auth)
	audit := &recordingAuditRepository{}
	fixture.service.Audit = audit

	_, err := fixture.service.StartTemplateRun(authenticatedContext(), StartTemplateRunCommand{
		TenantID:        "tenant_123",
		StackTemplateID: "stack_template_123",
		Operation:       domain.OperationApply,
		AutoApprove:     true,
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("error = %v, want ErrForbidden", err)
	}
	if len(audit.events) != 1 || audit.events[0].Action != domain.AuditActionFailedAccessAttempt {
		t.Fatalf("audit events = %+v, want one failed access attempt", audit.events)
	}
}

// refusingCredentialRepository and refusingCredentialCipher exist only so the
// credential commands get past their wiring checks to authorization. Every
// test that uses them is refused before either is called, so a call fails.
type refusingCredentialRepository struct{}

var errCredentialCalled = errors.New("credential dependency called before authorization refused")

func (refusingCredentialRepository) CreateCredential(context.Context, domain.CredentialSet) error {
	return errCredentialCalled
}

func (refusingCredentialRepository) ListCredentials(context.Context, domain.TenantID, domain.CredentialScope, string) ([]domain.CredentialSet, error) {
	return nil, errCredentialCalled
}

func (refusingCredentialRepository) DeleteCredential(context.Context, domain.TenantID, domain.CredentialSetID) error {
	return errCredentialCalled
}

func (refusingCredentialRepository) ListCredentialsForStackTemplate(context.Context, domain.TenantID, domain.StackTemplateID) ([]domain.CredentialSet, error) {
	return nil, errCredentialCalled
}

type refusingCredentialCipher struct{}

func (refusingCredentialCipher) Encrypt(string) (string, error) {
	return "", errCredentialCalled
}

func (refusingCredentialCipher) Decrypt(string) (string, error) {
	return "", errCredentialCalled
}
