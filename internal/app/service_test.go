package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vishu42/openplan/internal/authentication"
	"github.com/vishu42/openplan/internal/authorization"
	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/queue"
)

const oidcSubject = "6fdb4b4c-2a8f-4cf7-945f-38f67f6a0e91"

func authenticatedContext() context.Context {
	return authentication.ContextWithPrincipal(context.Background(), authentication.Principal{Subject: oidcSubject})
}

func TestActorMutationsRejectMissingPrincipal(t *testing.T) {
	t.Parallel()

	service := NewService(Service{
		TemplateRuns: &recordingTemplateRunRepository{}})
	tests := []struct {
		name string
		call func() error
	}{
		{name: "register template", call: func() error {
			_, err := service.RegisterTemplate(context.Background(), RegisterTemplateCommand{})
			return err
		}},
		{name: "sync template", call: func() error {
			_, err := service.StartTemplateSync(context.Background(), StartTemplateSyncCommand{})
			return err
		}},
		{name: "create stack", call: func() error {
			_, err := service.CreateStack(context.Background(), CreateStackCommand{})
			return err
		}},
		{name: "add template", call: func() error {
			_, err := service.AddTemplateToStack(context.Background(), AddTemplateToStackCommand{})
			return err
		}},
		{name: "update config", call: func() error {
			_, err := service.UpdateStackTemplateConfig(context.Background(), UpdateStackTemplateConfigCommand{})
			return err
		}},
		{name: "upgrade template", call: func() error {
			_, err := service.UpgradeStackTemplate(context.Background(), UpgradeStackTemplateCommand{})
			return err
		}},
		{name: "start run", call: func() error {
			_, err := service.StartTemplateRun(context.Background(), StartTemplateRunCommand{})
			return err
		}},
		{name: "approve run", call: func() error {
			_, err := service.ApproveRun(context.Background(), ApproveRunCommand{})
			return err
		}},
		{name: "discard run", call: func() error {
			return service.DiscardRun(context.Background(), DiscardRunCommand{})
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call(); !errors.Is(err, ErrUnauthenticated) {
				t.Fatalf("error = %v, want ErrUnauthenticated", err)
			}
		})
	}
}

func TestAuthenticatedActorRejectsEmptySubject(t *testing.T) {
	t.Parallel()

	ctx := authentication.ContextWithPrincipal(context.Background(), authentication.Principal{})
	if _, err := authenticatedActor(ctx); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("error = %v, want ErrUnauthenticated", err)
	}
}

func TestCreateStackDerivesSlugAndPersistsStack(t *testing.T) {
	t.Parallel()

	ctx := authenticatedContext()
	now := time.Date(2026, 7, 6, 13, 30, 0, 0, time.UTC)
	stacks := &recordingStackRepository{}
	service := NewService(Service{
		Stacks:        stacks,
		Work:          newRecordingWork(stacks),
		Authorization: testPlatformAuthorizer(t),
		StackIDs:      fixedStackIDGenerator{id: domain.StackID("stack_new")},
		Clock:         fixedClock{now: now},
	})

	stack, err := service.CreateStack(ctx, CreateStackCommand{
		TenantID: domain.TenantID("tenant_123"),
		Name:     "Acme Prod",
		Tags: map[string]string{
			"env": "prod",
		},
		DefaultCredentialIDs: []domain.CredentialSetID{domain.CredentialSetID("credential_123")},
	})
	if err != nil {
		t.Fatalf("CreateStack returned error: %v", err)
	}

	if stack.ID != domain.StackID("stack_new") {
		t.Fatalf("stack ID = %q, want stack_new", stack.ID)
	}
	if stack.Slug != "acme-prod" {
		t.Fatalf("slug = %q, want acme-prod", stack.Slug)
	}
	if stack.CreatedBy != domain.UserID(oidcSubject) {
		t.Fatalf("created by = %q, want %q", stack.CreatedBy, oidcSubject)
	}
	if !stack.CreatedAt.Equal(now) {
		t.Fatalf("created at = %v, want %v", stack.CreatedAt, now)
	}
	if stacks.created.ID != stack.ID {
		t.Fatalf("persisted stack ID = %q, want %q", stacks.created.ID, stack.ID)
	}
	if stacks.created.Tags["env"] != "prod" {
		t.Fatalf("persisted tags = %#v", stacks.created.Tags)
	}
	if len(stacks.created.DefaultCredentialIDs) != 1 || stacks.created.DefaultCredentialIDs[0] != domain.CredentialSetID("credential_123") {
		t.Fatalf("default credential IDs = %#v", stacks.created.DefaultCredentialIDs)
	}
}

func TestCreateStackReturnsDuplicateSlugConflict(t *testing.T) {
	t.Parallel()

	stacks := &recordingStackRepository{createErr: ErrDuplicateStackSlug}
	authorizer := testPlatformAuthorizer(t)
	service := NewService(Service{
		Stacks:        stacks,
		Work:          newRecordingWork(stacks),
		Authorization: authorizer,
		StackIDs:      fixedStackIDGenerator{id: domain.StackID("stack_new")},
		Clock:         fixedClock{now: time.Now()},
	})

	_, err := service.CreateStack(authenticatedContext(), CreateStackCommand{
		TenantID: domain.TenantID("tenant_123"),
		Name:     "Acme Prod",
		Slug:     "acme-prod",
	})
	if !errors.Is(err, ErrDuplicateStackSlug) {
		t.Fatalf("error = %v, want ErrDuplicateStackSlug", err)
	}
	// The slug was sent, so the clash is the slug field's.
	assertFieldIssues(t, err, "slug duplicate")
	// A rejected creation leaves no owner behind. Under a real transaction the
	// rollback guarantees it; here the in-memory engine shows the same outcome
	// because the grant is written after the row that failed.
	grants, err := authorizer.ListGrants(context.Background(), mustStackObject(t, "stack_new"))
	if err != nil {
		t.Fatalf("ListGrants() error = %v", err)
	}
	if len(grants) != 0 {
		t.Fatalf("grants = %#v, want none after a rejected creation", grants)
	}
}

// With no slug sent, the server made it from the name, so the name is the
// field the caller can change; pointing at a slug field the form doesn't
// have would leave nothing to mark.
func TestCreateStackDuplicateDerivedSlugPointsAtTheName(t *testing.T) {
	t.Parallel()

	stacks := &recordingStackRepository{createErr: ErrDuplicateStackSlug}
	service := NewService(Service{
		Stacks:        stacks,
		Work:          newRecordingWork(stacks),
		Authorization: testPlatformAuthorizer(t),
		StackIDs:      fixedStackIDGenerator{id: domain.StackID("stack_new")},
		Clock:         fixedClock{now: time.Now()},
	})

	_, err := service.CreateStack(authenticatedContext(), CreateStackCommand{TenantID: "tenant_123", Name: "Acme Prod"})
	if !errors.Is(err, ErrDuplicateStackSlug) {
		t.Fatalf("error = %v, want ErrDuplicateStackSlug", err)
	}
	assertFieldIssues(t, err, "name duplicate")
	if !strings.Contains(FieldErrors(err)[0].Message, `"acme-prod"`) {
		t.Fatalf("message = %q, want it to name the derived slug", FieldErrors(err)[0].Message)
	}
}

func TestCreateStackRejectsInvalidTagKey(t *testing.T) {
	t.Parallel()

	service := NewService(Service{
		Authorization: testPlatformAuthorizer(t),
		Stacks:        &recordingStackRepository{},
		Work:          newRecordingWork(&recordingStackRepository{}),
		StackIDs:      fixedStackIDGenerator{id: domain.StackID("stack_new")},
	})

	_, err := service.CreateStack(authenticatedContext(), CreateStackCommand{
		TenantID: domain.TenantID("tenant_123"),
		Name:     "Acme Prod",
		Tags: map[string]string{
			"bad key": "prod",
		},
	})
	if !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("error = %v, want ErrInvalidCommand", err)
	}
}

func TestCreateStackRejectsEmptyDefaultCredentialID(t *testing.T) {
	t.Parallel()

	service := NewService(Service{
		Authorization: testPlatformAuthorizer(t),
		Stacks:        &recordingStackRepository{},
		Work:          newRecordingWork(&recordingStackRepository{}),
		StackIDs:      fixedStackIDGenerator{id: domain.StackID("stack_new")},
	})

	_, err := service.CreateStack(authenticatedContext(), CreateStackCommand{
		TenantID: domain.TenantID("tenant_123"),
		Name:     "Acme Prod",
		DefaultCredentialIDs: []domain.CredentialSetID{
			domain.CredentialSetID(""),
		},
	})
	if !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("error = %v, want ErrInvalidCommand", err)
	}
}

func TestGetStackPassesTenantAndIDAndNormalizesNilTemplates(t *testing.T) {
	t.Parallel()

	stacks := &recordingStackRepository{
		view: StackView{
			Stack: domain.Stack{
				ID:       domain.StackID("stack_123"),
				TenantID: domain.TenantID("tenant_123"),
				Name:     "Acme Prod",
				Slug:     "acme-prod",
			},
			Templates: nil,
		},
	}
	service := NewService(Service{Stacks: stacks, Authorization: testPlatformAuthorizer(t)})
	ctx := authentication.ContextWithPrincipal(context.Background(), authentication.Principal{Subject: oidcSubject})

	view, err := service.GetStack(ctx, GetStackCommand{
		TenantID: domain.TenantID("tenant_123"),
		StackID:  domain.StackID("stack_123"),
	})
	if err != nil {
		t.Fatalf("GetStack returned error: %v", err)
	}

	if stacks.gotTenantID != domain.TenantID("tenant_123") {
		t.Fatalf("tenant lookup = %q, want tenant_123", stacks.gotTenantID)
	}
	if stacks.gotStackID != domain.StackID("stack_123") {
		t.Fatalf("stack lookup = %q, want stack_123", stacks.gotStackID)
	}
	if view.Templates == nil {
		t.Fatal("templates = nil, want empty slice")
	}
	if len(view.Templates) != 0 {
		t.Fatalf("len(templates) = %d, want 0", len(view.Templates))
	}
}

func TestTemplateDisplayName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		repoName string
		rootPath string
		want     string
	}{
		{name: "root path dot", repoName: "infra-templates", rootPath: ".", want: "infra-templates"},
		{name: "empty root path", repoName: "infra-templates", rootPath: "", want: "infra-templates"},
		{name: "subdirectory", repoName: "infra-templates", rootPath: "modules/vpc", want: "vpc"},
		{name: "nested subdirectory", repoName: "my-repo", rootPath: "modules/network/vpc", want: "vpc"},
		{name: "trailing slash", repoName: "my-repo", rootPath: "modules/vpc/", want: "vpc"},
		{name: "trims whitespace", repoName: "  my-repo  ", rootPath: ".", want: "my-repo"},
		{name: "trims root path whitespace", repoName: "my-repo", rootPath: "  modules/vpc  ", want: "vpc"},
		{name: "cleans path", repoName: "my-repo", rootPath: "modules/../modules/vpc", want: "vpc"},
		{name: "dot slash is the root", repoName: "my-repo", rootPath: "./", want: "my-repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := templateDisplayName(tt.repoName, tt.rootPath)
			if got != tt.want {
				t.Errorf("templateDisplayName(%q, %q) = %q, want %q", tt.repoName, tt.rootPath, got, tt.want)
			}
		})
	}
}

func TestGetStackResolvesTemplateDisplayName(t *testing.T) {
	t.Parallel()

	stacks := &recordingStackRepository{
		view: StackView{
			Stack: domain.Stack{
				ID:       domain.StackID("stack_123"),
				TenantID: domain.TenantID("tenant_123"),
				Name:     "Acme Prod",
				Slug:     "acme-prod",
			},
			Templates: []StackTemplateView{
				{StackTemplate: domain.StackTemplate{
					ID:                        domain.StackTemplateID("stack_template_1"),
					DesiredTemplateRevisionID: domain.TemplateRevisionID("rev_1"),
				}},
				{StackTemplate: domain.StackTemplate{
					ID:                        domain.StackTemplateID("stack_template_2"),
					DesiredTemplateRevisionID: domain.TemplateRevisionID("rev_2"),
				}},
				{StackTemplate: domain.StackTemplate{
					ID:                        domain.StackTemplateID("stack_template_3"),
					DesiredTemplateRevisionID: domain.TemplateRevisionID("rev_missing"),
				}},
			},
		},
	}
	revisions := &recordingTemplateRepository{
		templates: []domain.TemplateRevision{
			{ID: domain.TemplateRevisionID("rev_1"), RepoName: "infra-templates", RootPath: "modules/vpc", SourceRef: "v2.0.0"},
			{ID: domain.TemplateRevisionID("rev_2"), RepoName: "my-repo", RootPath: ".", SourceRef: "main"},
		},
	}
	service := NewService(Service{Stacks: stacks, TemplateRevisions: revisions, Authorization: testPlatformAuthorizer(t)})
	ctx := authentication.ContextWithPrincipal(context.Background(), authentication.Principal{Subject: oidcSubject})

	view, err := service.GetStack(ctx, GetStackCommand{
		TenantID: domain.TenantID("tenant_123"),
		StackID:  domain.StackID("stack_123"),
	})
	if err != nil {
		t.Fatalf("GetStack returned error: %v", err)
	}

	if len(view.Templates) != 3 {
		t.Fatalf("len(templates) = %d, want 3", len(view.Templates))
	}
	if got := view.Templates[0].DisplayName; got != "vpc" {
		t.Errorf("template[0].DisplayName = %q, want vpc", got)
	}
	// The ref is resolved from the desired revision on every read rather than
	// stored on the component, so it cannot drift from the revision in use.
	if got := view.Templates[0].SourceRef; got != "v2.0.0" {
		t.Errorf("template[0].SourceRef = %q, want v2.0.0", got)
	}
	if got := view.Templates[1].SourceRef; got != "main" {
		t.Errorf("template[1].SourceRef = %q, want main", got)
	}
	// An unresolvable revision leaves it empty rather than guessing.
	if got := view.Templates[2].SourceRef; got != "" {
		t.Errorf("template[2].SourceRef = %q, want empty", got)
	}
	if got := view.Templates[1].DisplayName; got != "my-repo" {
		t.Errorf("template[1].DisplayName = %q, want my-repo", got)
	}
	if got := view.Templates[2].DisplayName; got != "" {
		t.Errorf("template[2].DisplayName = %q, want empty (no matching revision)", got)
	}
}

func TestListStacksPassesTenantAndNormalizesNilStacks(t *testing.T) {
	t.Parallel()

	stacks := &recordingStackRepository{list: nil}
	service := NewService(Service{Stacks: stacks, Authorization: newTestAuthorization(t)})
	ctx := authentication.ContextWithPrincipal(context.Background(), authentication.Principal{Subject: oidcSubject})

	page, err := service.ListStacks(ctx, ListStacksCommand{
		TenantID: domain.TenantID("tenant_123"),
		Page:     FirstPage(),
	})
	got := page.Items
	if err != nil {
		t.Fatalf("ListStacks returned error: %v", err)
	}

	if stacks.gotListTenantID != domain.TenantID("tenant_123") {
		t.Fatalf("tenant list lookup = %q, want tenant_123", stacks.gotListTenantID)
	}
	if got == nil {
		t.Fatal("stacks = nil, want empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("len(stacks) = %d, want 0", len(got))
	}
}

func TestAddTemplateToStackValidatesVariablesAndPersistsStackTemplate(t *testing.T) {
	t.Parallel()

	stacks := &recordingStackRepository{
		stack: domain.Stack{
			ID:       domain.StackID("stack_123"),
			TenantID: domain.TenantID("tenant_123"),
			Name:     "Acme Prod",
			Slug:     "acme-prod",
		},
	}
	templates := &recordingTemplateRepository{
		template: domain.TemplateRevision{
			ID:               domain.TemplateRevisionID("template_123"),
			TenantID:         domain.TenantID("tenant_123"),
			SourceTemplateID: domain.SourceTemplateID("source_template_vpc"),
			SourceRef:        "main",
			Status:           domain.TemplateRevisionActive,
		},
		variables: []domain.TemplateVariable{
			{Name: "region", Required: true},
			{Name: "cidr", Required: false, HasDefault: true},
		},
	}
	installer := &recordingStackTemplateInstaller{}
	service := NewService(Service{
		Authorization:            testPlatformAuthorizer(t),
		Stacks:                   stacks,
		Work:                     newRecordingWork(stacks),
		TemplateRevisionMetadata: templates,
		TemplateRevisions:        templates,
		StackTemplateInstaller:   installer,
		StackTemplateIDs:         fixedStackTemplateIDGenerator{id: domain.StackTemplateID("stack_template_a1b2c3d4")},
	})

	stackTemplate, err := service.AddTemplateToStack(authenticatedContext(), AddTemplateToStackCommand{
		TenantID:           domain.TenantID("tenant_123"),
		StackID:            domain.StackID("stack_123"),
		TemplateRevisionID: domain.TemplateRevisionID("template_123"),
		ComponentKey:       "primary-vpc",
		ConfigJSON:         json.RawMessage(`{"region":"us-east-1"}`),
	})
	if err != nil {
		t.Fatalf("AddTemplateToStack returned error: %v", err)
	}

	if stackTemplate.ID != domain.StackTemplateID("stack_template_a1b2c3d4") {
		t.Fatalf("stack template revision ID = %q, want stack_template_a1b2c3d4", stackTemplate.ID)
	}
	if stackTemplate.TenantID != domain.TenantID("tenant_123") {
		t.Fatalf("tenant ID = %q, want tenant_123", stackTemplate.TenantID)
	}
	if stackTemplate.WorkspaceName != "meg_acme_prod_a1b2c3d4" {
		t.Fatalf("workspace name = %q, want meg_acme_prod_a1b2c3d4", stackTemplate.WorkspaceName)
	}
	if stackTemplate.Lifecycle != domain.StackTemplateActive {
		t.Fatalf("lifecycle = %q, want active", stackTemplate.Lifecycle)
	}
	if stackTemplate.CreatedBy != domain.UserID(oidcSubject) {
		t.Fatalf("created by = %q, want %q", stackTemplate.CreatedBy, oidcSubject)
	}
	if stackTemplate.ComponentKey != "primary-vpc" {
		t.Fatalf("component key = %q, want primary-vpc", stackTemplate.ComponentKey)
	}
	if stackTemplate.SourceTemplateID != domain.SourceTemplateID("source_template_vpc") {
		t.Fatalf("source template ID = %q, want source_template_vpc", stackTemplate.SourceTemplateID)
	}
	if stackTemplate.DesiredTemplateRevisionID != domain.TemplateRevisionID("template_123") {
		t.Fatalf("desired template revision ID = %q, want template_123", stackTemplate.DesiredTemplateRevisionID)
	}
	if string(stackTemplate.DesiredConfigJSON) != `{"region":"us-east-1"}` {
		t.Fatalf("desired config json = %s", stackTemplate.DesiredConfigJSON)
	}
	if installer.created.CreatedBy != domain.UserID(oidcSubject) {
		t.Fatalf("persisted created by = %q, want %q", installer.created.CreatedBy, oidcSubject)
	}
	if string(installer.created.InstalledConfigJSON) != `{"region":"us-east-1"}` {
		t.Fatalf("config json = %s", installer.created.InstalledConfigJSON)
	}
	if installer.created.SourceTemplateID != domain.SourceTemplateID("source_template_vpc") {
		t.Fatalf("persisted source template ID = %q, want source_template_vpc", installer.created.SourceTemplateID)
	}
	if installer.created.DesiredTemplateRevisionID != domain.TemplateRevisionID("template_123") {
		t.Fatalf("persisted desired template revision ID = %q, want template_123", installer.created.DesiredTemplateRevisionID)
	}
	if string(installer.created.DesiredConfigJSON) != `{"region":"us-east-1"}` {
		t.Fatalf("persisted desired config json = %s", installer.created.DesiredConfigJSON)
	}
}

func TestAddTemplateToStackRejectsMissingRequiredVariable(t *testing.T) {
	t.Parallel()

	service := NewService(Service{
		Authorization: testPlatformAuthorizer(t),
		Stacks: &recordingStackRepository{
			stack: domain.Stack{ID: domain.StackID("stack_123"), TenantID: domain.TenantID("tenant_123"), Slug: "acme-prod"},
		},
		TemplateRevisionMetadata: &recordingTemplateRepository{
			template: domain.TemplateRevision{ID: domain.TemplateRevisionID("template_123"), TenantID: domain.TenantID("tenant_123"), Status: domain.TemplateRevisionActive},
		},
		TemplateRevisions: &recordingTemplateRepository{
			variables: []domain.TemplateVariable{{Name: "region", Required: true}},
		},
		StackTemplateInstaller: &recordingStackTemplateInstaller{},
		StackTemplateIDs:       fixedStackTemplateIDGenerator{id: domain.StackTemplateID("stack_template_a1b2c3d4")},
	})

	_, err := service.AddTemplateToStack(authenticatedContext(), AddTemplateToStackCommand{
		TenantID:           domain.TenantID("tenant_123"),
		StackID:            domain.StackID("stack_123"),
		TemplateRevisionID: domain.TemplateRevisionID("template_123"),
		ConfigJSON:         json.RawMessage(`{}`),
	})
	if !errors.Is(err, ErrStackTemplateConfigInvalid) {
		t.Fatalf("error = %v, want ErrStackTemplateConfigInvalid", err)
	}
}

func TestAddTemplateToStackRejectsUnknownVariable(t *testing.T) {
	t.Parallel()

	service := NewService(Service{
		Authorization: testPlatformAuthorizer(t),
		Stacks: &recordingStackRepository{
			stack: domain.Stack{ID: domain.StackID("stack_123"), TenantID: domain.TenantID("tenant_123"), Slug: "acme-prod"},
		},
		TemplateRevisionMetadata: &recordingTemplateRepository{
			template: domain.TemplateRevision{ID: domain.TemplateRevisionID("template_123"), TenantID: domain.TenantID("tenant_123"), Status: domain.TemplateRevisionActive},
		},
		TemplateRevisions: &recordingTemplateRepository{
			variables: []domain.TemplateVariable{{Name: "region", Required: true}},
		},
		StackTemplateInstaller: &recordingStackTemplateInstaller{},
		StackTemplateIDs:       fixedStackTemplateIDGenerator{id: domain.StackTemplateID("stack_template_a1b2c3d4")},
	})

	_, err := service.AddTemplateToStack(authenticatedContext(), AddTemplateToStackCommand{
		TenantID:           domain.TenantID("tenant_123"),
		StackID:            domain.StackID("stack_123"),
		TemplateRevisionID: domain.TemplateRevisionID("template_123"),
		ConfigJSON:         json.RawMessage(`{"region":"us-east-1","extra":"nope"}`),
	})
	if !errors.Is(err, ErrStackTemplateConfigInvalid) {
		t.Fatalf("error = %v, want ErrStackTemplateConfigInvalid", err)
	}
}

func TestAddTemplateToStackRejectsInactiveTemplate(t *testing.T) {
	t.Parallel()

	service := NewService(Service{
		Authorization: testPlatformAuthorizer(t),
		Stacks: &recordingStackRepository{
			stack: domain.Stack{ID: domain.StackID("stack_123"), TenantID: domain.TenantID("tenant_123"), Slug: "acme-prod"},
		},
		TemplateRevisionMetadata: &recordingTemplateRepository{
			template: domain.TemplateRevision{ID: domain.TemplateRevisionID("template_123"), TenantID: domain.TenantID("tenant_123"), Status: domain.TemplateRevisionInvalid},
		},
		TemplateRevisions:      &recordingTemplateRepository{},
		StackTemplateInstaller: &recordingStackTemplateInstaller{},
		StackTemplateIDs:       fixedStackTemplateIDGenerator{id: domain.StackTemplateID("stack_template_a1b2c3d4")},
	})

	_, err := service.AddTemplateToStack(authenticatedContext(), AddTemplateToStackCommand{
		TenantID:           domain.TenantID("tenant_123"),
		StackID:            domain.StackID("stack_123"),
		TemplateRevisionID: domain.TemplateRevisionID("template_123"),
		ConfigJSON:         json.RawMessage(`{}`),
	})
	if !errors.Is(err, ErrTemplateNotInstallable) {
		t.Fatalf("error = %v, want ErrTemplateNotInstallable", err)
	}
}

func TestStartTemplateRunCreatesQueuedRunWithoutDispatchingWorkflow(t *testing.T) {
	t.Parallel()

	ctx := authenticatedContext()
	now := time.Date(2026, 7, 2, 9, 30, 0, 0, time.UTC)

	stackTemplates := &recordingStackTemplateRepository{
		stackTemplate: domain.StackTemplate{
			ID:                        domain.StackTemplateID("stack_template_123"),
			StackID:                   domain.StackID("stack_123"),
			SourceTemplateID:          domain.SourceTemplateID("source_template_vpc"),
			DesiredTemplateRevisionID: domain.TemplateRevisionID("template_rev_2"),
			DesiredConfigJSON:         json.RawMessage(`{"region":"us-east-1"}`),
			WorkspaceName:             "mtp_acme_prod_vpc_a13f9c",
			Lifecycle:                 domain.StackTemplateActive,
			// An apply needs a plan that still describes desired state.
			PendingPlanRunID:              domain.TemplateRunID("run_plan_1"),
			PendingPlanTemplateRevisionID: domain.TemplateRevisionID("template_rev_2"),
			PendingPlanConfigJSON:         json.RawMessage(`{"region":"us-east-1"}`),
		},
	}
	templates := &recordingTemplateRepository{
		template: domain.TemplateRevision{
			ID:               domain.TemplateRevisionID("template_rev_2"),
			TenantID:         domain.TenantID("tenant_123"),
			SourceTemplateID: domain.SourceTemplateID("source_template_vpc"),
			RepoOwner:        "acme",
			RepoName:         "infra-templates",
			// The run's ref comes from here now, not from the component.
			SourceRef:         "main",
			ResolvedCommitSHA: "sha-2",
			RootPath:          "modules/vpc",
			Status:            domain.TemplateRevisionActive,
		},
	}
	runs := &recordingTemplateRunRepository{run: domain.TemplateRun{ID: "run_123", TenantID: "tenant_123", StackTemplateID: "stack_template_123"}, createdRunNumber: 7}
	work := &recordingUnitOfWork{templateRuns: runs}

	service := NewService(Service{
		Users:                    &fakeUserRepository{},
		Authorization:            testPlatformAuthorizer(t),
		Work:                     work,
		StackTemplates:           stackTemplates,
		TemplateRuns:             runs,
		TemplateRevisionMetadata: templates,
		RunIDs:                   fixedTemplateRunIDGenerator{runID: domain.TemplateRunID("run_123")},
		Clock:                    fixedClock{now: now},
	})

	run, err := service.StartTemplateRun(ctx, StartTemplateRunCommand{
		TenantID:        domain.TenantID("tenant_123"),
		StackTemplateID: domain.StackTemplateID("stack_template_123"),
		Operation:       domain.OperationPlan,
	})
	if err != nil {
		t.Fatalf("StartTemplateRun returned error: %v", err)
	}

	if run.ID != domain.TemplateRunID("run_123") {
		t.Fatalf("run.ID = %q, want run_123", run.ID)
	}
	// The store assigns the number; the service hands it back unchanged.
	if run.RunNumber != 7 {
		t.Fatalf("run.RunNumber = %d, want 7", run.RunNumber)
	}

	if run.Status != domain.TemplateRunQueued {
		t.Fatalf("run.Status = %q, want %q", run.Status, domain.TemplateRunQueued)
	}
	if run.TriggerActor != domain.UserID(oidcSubject) {
		t.Fatalf("run.TriggerActor = %q, want %q", run.TriggerActor, oidcSubject)
	}

	if run.WorkspaceName != "mtp_acme_prod_vpc_a13f9c" {
		t.Fatalf("run.WorkspaceName = %q", run.WorkspaceName)
	}

	if run.SelectedRef != "main" {
		t.Fatalf("run.SelectedRef = %q, want main", run.SelectedRef)
	}

	if run.TemplateRevisionID != domain.TemplateRevisionID("template_rev_2") {
		t.Fatalf("run.TemplateRevisionID = %q, want template_rev_2", run.TemplateRevisionID)
	}

	if run.SourceTemplateID != domain.SourceTemplateID("source_template_vpc") {
		t.Fatalf("run.SourceTemplateID = %q, want source_template_vpc", run.SourceTemplateID)
	}

	if string(run.ConfigJSON) != `{"region":"us-east-1"}` {
		t.Fatalf("run.ConfigJSON = %s", run.ConfigJSON)
	}

	if run.ResolvedCommitSHA != "sha-2" {
		t.Fatalf("run.ResolvedCommitSHA = %q, want sha-2", run.ResolvedCommitSHA)
	}

	if !run.CreatedAt.Equal(now) {
		t.Fatalf("run.CreatedAt = %v, want %v", run.CreatedAt, now)
	}

	if stackTemplates.gotTenantID != domain.TenantID("tenant_123") {
		t.Fatalf("stack template lookup tenant = %q", stackTemplates.gotTenantID)
	}

	if runs.created.ID != run.ID {
		t.Fatalf("created run ID = %q, want %q", runs.created.ID, run.ID)
	}

	if len(work.requests) != 1 || work.requests[0].Kind != KindStartTemplateRun {
		t.Fatalf("queued requests = %#v, want one start_template_run request", work.requests)
	}

	if templates.gotGetTemplateTenantID != domain.TenantID("tenant_123") {
		t.Fatalf("template lookup tenant = %q, want tenant_123", templates.gotGetTemplateTenantID)
	}

	if templates.gotGetTemplateRevisionID != domain.TemplateRevisionID("template_rev_2") {
		t.Fatalf("template revision lookup ID = %q, want template_rev_2", templates.gotGetTemplateRevisionID)
	}

}

func TestUpdateStackTemplateConfigValidatesDesiredRevisionVariables(t *testing.T) {
	t.Parallel()

	stackTemplates := &recordingStackTemplateRepository{
		stackTemplate: domain.StackTemplate{
			ID:                        domain.StackTemplateID("stack_template_123"),
			TenantID:                  domain.TenantID("tenant_123"),
			StackID:                   domain.StackID("stack_123"),
			DesiredTemplateRevisionID: domain.TemplateRevisionID("template_rev_2"),
			Lifecycle:                 domain.StackTemplateActive,
		},
	}
	templates := &recordingTemplateRepository{
		template: domain.TemplateRevision{ID: "template_rev_2", RepoName: "infra", RootPath: "modules/vpc", SourceRef: "v1.2.0"},
		variables: []domain.TemplateVariable{
			{Name: "region", Required: true},
		},
	}
	service := NewService(Service{
		TemplateRuns:             &recordingTemplateRunRepository{},
		Authorization:            testPlatformAuthorizer(t),
		StackTemplates:           stackTemplates,
		TemplateRevisions:        templates,
		TemplateRevisionMetadata: templates,
	})

	updated, err := service.UpdateStackTemplateConfig(authenticatedContext(), UpdateStackTemplateConfigCommand{
		TenantID:        domain.TenantID("tenant_123"),
		StackTemplateID: domain.StackTemplateID("stack_template_123"),
		ConfigJSON:      json.RawMessage(`{"region":"us-east-1"}`),
	})
	if err != nil {
		t.Fatalf("UpdateStackTemplateConfig returned error: %v", err)
	}

	if templates.gotVariablesTemplateRevisionID != domain.TemplateRevisionID("template_rev_2") {
		t.Fatalf("variables template revision ID = %q, want template_rev_2", templates.gotVariablesTemplateRevisionID)
	}
	if string(stackTemplates.gotConfigJSON) != `{"region":"us-east-1"}` {
		t.Fatalf("updated config = %s", stackTemplates.gotConfigJSON)
	}
	if string(updated.DesiredConfigJSON) != `{"region":"us-east-1"}` {
		t.Fatalf("returned desired config = %s", updated.DesiredConfigJSON)
	}
	if updated.DisplayName != "vpc" || updated.SourceRef != "v1.2.0" {
		t.Fatalf("labels = %q, %q, want vpc and v1.2.0", updated.DisplayName, updated.SourceRef)
	}
}

func TestUpdateStackTemplateConfigRejectsMissingDesiredRevision(t *testing.T) {
	t.Parallel()

	stackTemplates := &recordingStackTemplateRepository{
		stackTemplate: domain.StackTemplate{
			ID:        domain.StackTemplateID("stack_template_123"),
			TenantID:  domain.TenantID("tenant_123"),
			Lifecycle: domain.StackTemplateActive,
		},
	}
	service := NewService(Service{
		TemplateRuns:      &recordingTemplateRunRepository{},
		Authorization:     testPlatformAuthorizer(t),
		StackTemplates:    stackTemplates,
		TemplateRevisions: &recordingTemplateRepository{},
	})

	_, err := service.UpdateStackTemplateConfig(authenticatedContext(), UpdateStackTemplateConfigCommand{
		TenantID:        domain.TenantID("tenant_123"),
		StackTemplateID: domain.StackTemplateID("stack_template_123"),
		ConfigJSON:      json.RawMessage(`{}`),
	})
	// Nothing in the request can fix a missing desired revision, so it is a
	// state conflict rather than invalid config.
	if !errors.Is(err, ErrStackTemplateNotEditable) {
		t.Fatalf("error = %v, want ErrStackTemplateNotEditable", err)
	}
	if stackTemplates.gotConfigJSON != nil {
		t.Fatalf("updated config = %s, want no update", stackTemplates.gotConfigJSON)
	}
}

// A template being torn down cannot take a config edit, and says so in its
// own terms rather than as a failed upgrade.
func TestUpdateStackTemplateConfigRejectsAnInactiveTemplate(t *testing.T) {
	t.Parallel()

	stackTemplates := &recordingStackTemplateRepository{
		stackTemplate: domain.StackTemplate{
			ID:                        domain.StackTemplateID("stack_template_123"),
			TenantID:                  domain.TenantID("tenant_123"),
			DesiredTemplateRevisionID: domain.TemplateRevisionID("template_rev_1"),
			Lifecycle:                 domain.StackTemplateDestroying,
		},
	}
	service := NewService(Service{
		TemplateRuns:      &recordingTemplateRunRepository{},
		Authorization:     testPlatformAuthorizer(t),
		StackTemplates:    stackTemplates,
		TemplateRevisions: &recordingTemplateRepository{},
	})

	_, err := service.UpdateStackTemplateConfig(authenticatedContext(), UpdateStackTemplateConfigCommand{
		TenantID:        domain.TenantID("tenant_123"),
		StackTemplateID: domain.StackTemplateID("stack_template_123"),
		ConfigJSON:      json.RawMessage(`{}`),
	})
	if !errors.Is(err, ErrStackTemplateNotEditable) {
		t.Fatalf("error = %v, want ErrStackTemplateNotEditable", err)
	}
	if errors.Is(err, ErrStackTemplateUpgradeInvalid) {
		t.Fatalf("error = %v, want it not to read as an upgrade failure", err)
	}
	if stackTemplates.gotConfigJSON != nil {
		t.Fatalf("updated config = %s, want no update", stackTemplates.gotConfigJSON)
	}
}

func TestUpgradeStackTemplateCarriesForwardCompatibleConfig(t *testing.T) {
	t.Parallel()

	stackTemplates := &recordingStackTemplateRepository{
		stackTemplate: domain.StackTemplate{
			ID:                        domain.StackTemplateID("stack_template_123"),
			TenantID:                  domain.TenantID("tenant_123"),
			SourceTemplateID:          domain.SourceTemplateID("source_template_vpc"),
			DesiredTemplateRevisionID: domain.TemplateRevisionID("template_rev_1"),
			DesiredConfigJSON:         json.RawMessage(`{"region":"us-east-1","removed":"old"}`),
			Lifecycle:                 domain.StackTemplateActive,
		},
	}
	templates := &recordingTemplateRepository{
		template: domain.TemplateRevision{
			ID:               domain.TemplateRevisionID("template_rev_2"),
			TenantID:         domain.TenantID("tenant_123"),
			SourceTemplateID: domain.SourceTemplateID("source_template_vpc"),
			Status:           domain.TemplateRevisionActive,
		},
		variables: []domain.TemplateVariable{
			{Name: "region", Required: true},
			{Name: "size", HasDefault: true},
		},
	}
	service := NewService(Service{
		TemplateRuns:             &recordingTemplateRunRepository{},
		Authorization:            testPlatformAuthorizer(t),
		StackTemplates:           stackTemplates,
		TemplateRevisionMetadata: templates,
		TemplateRevisions:        templates,
	})

	updated, err := service.UpgradeStackTemplate(authenticatedContext(), UpgradeStackTemplateCommand{
		TenantID:                 domain.TenantID("tenant_123"),
		StackTemplateID:          domain.StackTemplateID("stack_template_123"),
		TargetTemplateRevisionID: domain.TemplateRevisionID("template_rev_2"),
	})
	if err != nil {
		t.Fatalf("UpgradeStackTemplate returned error: %v", err)
	}

	if stackTemplates.gotDesiredTemplateRevisionID != domain.TemplateRevisionID("template_rev_2") {
		t.Fatalf("updated desired template revision ID = %q, want template_rev_2", stackTemplates.gotDesiredTemplateRevisionID)
	}
	if string(stackTemplates.gotConfigJSON) != `{"region":"us-east-1"}` {
		t.Fatalf("carried config = %s, want region only", stackTemplates.gotConfigJSON)
	}
	if updated.DesiredTemplateRevisionID != domain.TemplateRevisionID("template_rev_2") {
		t.Fatalf("returned desired template revision ID = %q, want template_rev_2", updated.DesiredTemplateRevisionID)
	}
}

func TestUpgradeStackTemplateRejectsDifferentSourceTemplate(t *testing.T) {
	t.Parallel()

	stackTemplates := &recordingStackTemplateRepository{
		stackTemplate: domain.StackTemplate{
			ID:               domain.StackTemplateID("stack_template_123"),
			TenantID:         domain.TenantID("tenant_123"),
			SourceTemplateID: domain.SourceTemplateID("source_template_vpc"),
			Lifecycle:        domain.StackTemplateActive,
		},
	}
	templates := &recordingTemplateRepository{
		template: domain.TemplateRevision{
			ID:               domain.TemplateRevisionID("template_rev_2"),
			TenantID:         domain.TenantID("tenant_123"),
			SourceTemplateID: domain.SourceTemplateID("source_template_db"),
			Status:           domain.TemplateRevisionActive,
		},
	}
	service := NewService(Service{
		TemplateRuns:             &recordingTemplateRunRepository{},
		Authorization:            testPlatformAuthorizer(t),
		StackTemplates:           stackTemplates,
		TemplateRevisionMetadata: templates,
		TemplateRevisions:        templates,
	})

	_, err := service.UpgradeStackTemplate(authenticatedContext(), UpgradeStackTemplateCommand{
		TenantID:                 domain.TenantID("tenant_123"),
		StackTemplateID:          domain.StackTemplateID("stack_template_123"),
		TargetTemplateRevisionID: domain.TemplateRevisionID("template_rev_2"),
	})
	if !errors.Is(err, ErrStackTemplateUpgradeInvalid) {
		t.Fatalf("error = %v, want ErrStackTemplateUpgradeInvalid", err)
	}
	// The fix is in the request, so it lands on the field the caller picked.
	assertFieldIssues(t, err, "target_template_revision_id different_template")
}

func TestStartTemplateRunRejectsInvalidOperation(t *testing.T) {
	t.Parallel()

	service := NewService(Service{
		StackTemplates: &recordingStackTemplateRepository{},
		TemplateRuns:   &recordingTemplateRunRepository{},
		RunIDs:         fixedTemplateRunIDGenerator{runID: domain.TemplateRunID("run_123")},
		Clock:          fixedClock{now: time.Now()},
	})

	_, err := service.StartTemplateRun(authenticatedContext(), StartTemplateRunCommand{
		TenantID:        domain.TenantID("tenant_123"),
		StackTemplateID: domain.StackTemplateID("stack_template_123"),
		Operation:       domain.OperationType("refresh"),
	})
	if !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("error = %v, want ErrInvalidCommand", err)
	}
}

func TestStartTemplateRunRejectsInactiveStackTemplate(t *testing.T) {
	t.Parallel()

	stackTemplates := &recordingStackTemplateRepository{
		stackTemplate: domain.StackTemplate{
			ID:            domain.StackTemplateID("stack_template_123"),
			WorkspaceName: "mtp_acme_prod_vpc_a13f9c",
			Lifecycle:     domain.StackTemplateDestroyed,
		},
	}
	runs := &recordingTemplateRunRepository{}
	service := NewService(Service{
		Authorization:  testPlatformAuthorizer(t),
		StackTemplates: stackTemplates,
		TemplateRuns:   runs,
		RunIDs:         fixedTemplateRunIDGenerator{runID: domain.TemplateRunID("run_123")},
		Clock:          fixedClock{now: time.Now()},
	})

	_, err := service.StartTemplateRun(authenticatedContext(), StartTemplateRunCommand{
		TenantID:        domain.TenantID("tenant_123"),
		StackTemplateID: domain.StackTemplateID("stack_template_123"),
		Operation:       domain.OperationPlan,
	})
	if !errors.Is(err, ErrStackTemplateNotRunnable) {
		t.Fatalf("error = %v, want ErrStackTemplateNotRunnable", err)
	}

	if runs.created.ID != "" {
		t.Fatalf("created run ID = %q, want no persisted run", runs.created.ID)
	}

}

// Approving checks that the plan waiting for approval still describes desired
// state. Config and revision cannot change while a run is in flight, so this is
// the backstop behind that rule rather than the gate itself.
func TestApproveRunRejectsAPlanThatNoLongerMatchesDesired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		stackTemplate domain.StackTemplate
	}{
		{
			name: "config changed after the plan",
			stackTemplate: domain.StackTemplate{
				PendingPlanRunID:              "run_123",
				PendingPlanTemplateRevisionID: "template_123",
				PendingPlanConfigJSON:         json.RawMessage(`{"region":"us-east-1"}`),
			},
		},
		{
			name: "revision changed after the plan",
			stackTemplate: domain.StackTemplate{
				PendingPlanRunID:              "run_123",
				PendingPlanTemplateRevisionID: "template_122",
				PendingPlanConfigJSON:         json.RawMessage(`{"region":"eu-west-1"}`),
			},
		},
		{
			name: "another run's plan is the pending one",
			stackTemplate: domain.StackTemplate{
				PendingPlanRunID:              "run_other",
				PendingPlanTemplateRevisionID: "template_123",
				PendingPlanConfigJSON:         json.RawMessage(`{"region":"eu-west-1"}`),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			stackTemplate := test.stackTemplate
			stackTemplate.ID = "stack_template_123"
			stackTemplate.TenantID = "tenant_123"
			stackTemplate.StackID = "stack_123"
			stackTemplate.DesiredTemplateRevisionID = "template_123"
			stackTemplate.DesiredConfigJSON = json.RawMessage(`{"region":"eu-west-1"}`)

			runs := &recordingTemplateRunRepository{run: domain.TemplateRun{ID: "run_123", RunNumber: 7, TenantID: "tenant_123", StackTemplateID: "stack_template_123", Status: domain.TemplateRunWaitingApproval}}
			work := &recordingUnitOfWork{templateRuns: runs}
			service := NewService(Service{
				Authorization:            testPlatformAuthorizer(t),
				Work:                     work,
				TemplateRuns:             runs,
				StackTemplates:           &recordingStackTemplateRepository{stackTemplate: stackTemplate},
				TemplateRevisionMetadata: &recordingTemplateRepository{},
			})

			_, err := service.ApproveRun(authenticatedContext(), ApproveRunCommand{TenantID: "tenant_123", RunID: "run_123"})
			if !errors.Is(err, ErrStackTemplatePlanStale) {
				t.Fatalf("error = %v, want ErrStackTemplatePlanStale", err)
			}
			// The run is named, so the person knows which plan to redo.
			if stale, ok := errors.AsType[*PlanStaleError](err); !ok || stale.RunNumber != 7 {
				t.Fatalf("error = %v, want a PlanStaleError for run #7", err)
			}
			if len(work.requests) != 0 || runs.approval.RunID != "" {
				t.Fatalf("requests = %#v, approval = %#v; want nothing recorded", work.requests, runs.approval)
			}
		})
	}
}

// Only a plan that is waiting can be approved: one already approved, applying
// or finished is refused before anything is written.
func TestApproveRunRejectsARunThatIsNotWaiting(t *testing.T) {
	t.Parallel()

	runs := &recordingTemplateRunRepository{run: domain.TemplateRun{ID: "run_123", TenantID: "tenant_123", StackTemplateID: "stack_template_123", Status: domain.TemplateRunRunning}}
	work := &recordingUnitOfWork{templateRuns: runs}
	service := NewService(Service{
		Authorization:            testPlatformAuthorizer(t),
		Work:                     work,
		TemplateRuns:             runs,
		StackTemplates:           approvableStackTemplates(),
		TemplateRevisionMetadata: &recordingTemplateRepository{},
	})

	_, err := service.ApproveRun(authenticatedContext(), ApproveRunCommand{TenantID: "tenant_123", RunID: "run_123"})
	if !errors.Is(err, ErrRunNotApprovable) {
		t.Fatalf("error = %v, want ErrRunNotApprovable", err)
	}
	if work.inTxCalls != 0 {
		t.Fatalf("transaction calls = %d, want none", work.inTxCalls)
	}
}

// Auto-approve is an approval given before the plan exists, so starting an
// auto-approved run takes approve access as well as operate access.
func TestStartTemplateRunAutoApproveRequiresApproveAccess(t *testing.T) {
	t.Parallel()

	stackTemplate := domain.StackTemplate{
		ID:                        "stack_template_123",
		StackID:                   "stack_123",
		DesiredTemplateRevisionID: "template_123",
		WorkspaceName:             "mtp_acme_prod_vpc_a13f9c",
		Lifecycle:                 domain.StackTemplateActive,
	}
	newService := func(t *testing.T, grant authorization.Relation) (*Service, *recordingTemplateRunRepository, *recordingUnitOfWork) {
		runs := &recordingTemplateRunRepository{}
		work := &recordingUnitOfWork{templateRuns: runs}
		authorizer := seedGrants(t, newPlatformAuthorizer(t), mustGrant(t, oidcSubject, "stack_123", grant))
		return NewService(Service{
			Users:                    &fakeUserRepository{},
			Authorization:            authorizer,
			Work:                     work,
			StackTemplates:           &recordingStackTemplateRepository{stackTemplate: stackTemplate},
			TemplateRuns:             runs,
			TemplateRevisionMetadata: &recordingTemplateRepository{template: domain.TemplateRevision{ID: "template_123", Status: domain.TemplateRevisionActive}},
			RunIDs:                   fixedTemplateRunIDGenerator{runID: "run_123"},
		}), runs, work
	}
	command := StartTemplateRunCommand{TenantID: "tenant_123", StackTemplateID: "stack_template_123", Operation: domain.OperationApply, AutoApprove: true}

	operator, operatorRuns, _ := newService(t, authorization.RelationOperator)
	if _, err := operator.StartTemplateRun(authenticatedContext(), command); !errors.Is(err, ErrForbidden) {
		t.Fatalf("operator error = %v, want ErrForbidden", err)
	}
	if operatorRuns.created.ID != "" {
		t.Fatalf("operator created run %q, want none", operatorRuns.created.ID)
	}

	owner, ownerRuns, ownerWork := newService(t, authorization.RelationOwner)
	run, err := owner.StartTemplateRun(authenticatedContext(), command)
	if err != nil {
		t.Fatalf("owner error = %v", err)
	}
	if !run.AutoApprove || !ownerRuns.created.AutoApprove {
		t.Fatalf("run auto-approve = %v, persisted = %v; want both set", run.AutoApprove, ownerRuns.created.AutoApprove)
	}
	// It has no plan, so it goes straight to the apply workflow, and its
	// approval, given now, is audited now.
	if len(ownerWork.requests) != 1 || ownerWork.requests[0].Kind != KindStartTemplateApply {
		t.Fatalf("requests = %#v, want one start_template_apply", ownerWork.requests)
	}
	if len(ownerWork.audits) != 1 || ownerWork.audits[0].Action != domain.AuditActionApprovalGranted {
		t.Fatalf("audits = %#v, want one approval granted", ownerWork.audits)
	}
}

// Only an apply run can be auto-approved: a plan run applies nothing, and a
// destroy always waits for someone to approve its plan.
func TestStartTemplateRunRejectsAutoApproveOutsideApply(t *testing.T) {
	t.Parallel()

	service := NewService(Service{Authorization: testPlatformAuthorizer(t)})
	for _, operation := range []domain.OperationType{domain.OperationPlan, domain.OperationDestroy} {
		_, err := service.StartTemplateRun(authenticatedContext(), StartTemplateRunCommand{
			TenantID: "tenant_123", StackTemplateID: "stack_template_123", Operation: operation, AutoApprove: true,
		})
		if !errors.Is(err, ErrInvalidCommand) {
			t.Fatalf("%s error = %v, want ErrInvalidCommand", operation, err)
		}
	}
}

// A run snapshots desired state when it starts, so desired state cannot move
// while one is unfinished: the plan waiting for approval would stop being the
// plan of what the template says.
func TestConfigAndRevisionChangesWaitForTheRunInFlight(t *testing.T) {
	t.Parallel()

	stackTemplate := domain.StackTemplate{
		ID:                        "stack_template_123",
		StackID:                   "stack_123",
		SourceTemplateID:          "source_template_vpc",
		DesiredTemplateRevisionID: "template_123",
		Lifecycle:                 domain.StackTemplateActive,
	}
	runs := &recordingTemplateRunRepository{list: []domain.TemplateRun{{ID: "run_123", RunNumber: 4, Status: domain.TemplateRunWaitingApproval}}}
	service := NewService(Service{
		Authorization:            testPlatformAuthorizer(t),
		StackTemplates:           &recordingStackTemplateRepository{stackTemplate: stackTemplate},
		TemplateRuns:             runs,
		TemplateRevisions:        &recordingTemplateRepository{},
		TemplateRevisionMetadata: &recordingTemplateRepository{template: domain.TemplateRevision{ID: "template_124", SourceTemplateID: "source_template_vpc", Status: domain.TemplateRevisionActive}},
	})

	_, err := service.UpdateStackTemplateConfig(authenticatedContext(), UpdateStackTemplateConfigCommand{TenantID: "tenant_123", StackTemplateID: "stack_template_123", ConfigJSON: json.RawMessage(`{}`)})
	if !errors.Is(err, ErrTemplateRunInFlight) {
		t.Fatalf("config error = %v, want ErrTemplateRunInFlight", err)
	}
	if inFlight, ok := errors.AsType[*RunInFlightError](err); !ok || inFlight.RunNumber != 4 || inFlight.Status != domain.TemplateRunWaitingApproval {
		t.Fatalf("config error = %v, want run #4 waiting for approval as a RunInFlightError", err)
	}
	_, err = service.UpgradeStackTemplate(authenticatedContext(), UpgradeStackTemplateCommand{TenantID: "tenant_123", StackTemplateID: "stack_template_123", TargetTemplateRevisionID: "template_124"})
	if !errors.Is(err, ErrTemplateRunInFlight) {
		t.Fatalf("upgrade error = %v, want ErrTemplateRunInFlight", err)
	}
}

// A plan waiting for approval, or approved but not yet applying, has no
// workflow running, so discarding it is only the write.
func TestDiscardRunDiscardsAWaitingPlan(t *testing.T) {
	t.Parallel()

	runs := &recordingTemplateRunRepository{run: domain.TemplateRun{ID: "run_123", TenantID: "tenant_123", StackTemplateID: "stack_template_123", Status: domain.TemplateRunWaitingApproval}}
	work := &recordingUnitOfWork{templateRuns: runs}
	service := NewService(Service{
		Authorization:  testPlatformAuthorizer(t),
		Work:           work,
		TemplateRuns:   runs,
		StackTemplates: approvableStackTemplates(),
	})

	if err := service.DiscardRun(authenticatedContext(), DiscardRunCommand{TenantID: "tenant_123", RunID: "run_123", Reason: "discard"}); err != nil {
		t.Fatalf("DiscardRun returned error: %v", err)
	}
	if runs.discarded.RunID != "run_123" || runs.discarded.RequestedBy != domain.UserID(oidcSubject) {
		t.Fatalf("discarded = %#v, want run_123 by the caller", runs.discarded)
	}
	if len(work.requests) != 0 {
		t.Fatalf("queued = %#v, want nothing queued", work.requests)
	}
}

// The gate against concurrent runs is the template_runs_in_flight_idx unique
// The store refuses the insert without naming the winner, so the service looks
// it up: the person is then told which run to wait for or discard.
func TestStartTemplateRunNamesTheRunThatWon(t *testing.T) {
	t.Parallel()

	runs := &recordingTemplateRunRepository{
		createErr: ErrTemplateRunInFlight,
		list:      []domain.TemplateRun{{RunNumber: 4, Status: domain.TemplateRunRunning}},
	}
	service := NewService(Service{
		Users:         &fakeUserRepository{},
		Work:          &recordingUnitOfWork{templateRuns: runs},
		Authorization: testPlatformAuthorizer(t),
		TemplateRuns:  runs,
		StackTemplates: &recordingStackTemplateRepository{stackTemplate: domain.StackTemplate{
			ID:                        domain.StackTemplateID("stack_template_123"),
			DesiredTemplateRevisionID: domain.TemplateRevisionID("template_123"),
			WorkspaceName:             "mtp_acme_prod_vpc_a13f9c",
			Lifecycle:                 domain.StackTemplateActive,
		}},
		TemplateRevisionMetadata: &recordingTemplateRepository{
			template: domain.TemplateRevision{ID: domain.TemplateRevisionID("template_123"), Status: domain.TemplateRevisionActive},
		},
		RunIDs: fixedTemplateRunIDGenerator{runID: domain.TemplateRunID("run_123")},
		Clock:  fixedClock{now: time.Now()},
	})

	_, err := service.StartTemplateRun(authenticatedContext(), StartTemplateRunCommand{
		TenantID: "tenant_123", StackTemplateID: "stack_template_123", Operation: domain.OperationPlan,
	})
	inFlight, ok := errors.AsType[*RunInFlightError](err)
	if !ok || inFlight.RunNumber != 4 || inFlight.Status != domain.TemplateRunRunning {
		t.Fatalf("error = %v, want run #4 running as a RunInFlightError", err)
	}
	if !errors.Is(err, ErrTemplateRunInFlight) {
		t.Fatalf("error = %v, want it to still match ErrTemplateRunInFlight", err)
	}
}

// index, not a check in this method, so the rejection arrives from the store
// through the transaction rather than from a branch above it. What the service
// owes is passage: the sentinel has to survive the InTx closure and the wrap
// around it, or the API cannot map it to its 409 and the user gets a 500.
func TestStartTemplateRunSurfacesTheStoresInFlightRejection(t *testing.T) {
	t.Parallel()

	runs := &recordingTemplateRunRepository{createErr: ErrTemplateRunInFlight}
	work := &recordingUnitOfWork{templateRuns: runs}
	service := NewService(Service{
		Users:         &fakeUserRepository{},
		Work:          work,
		Authorization: testPlatformAuthorizer(t),
		TemplateRuns:  runs,
		StackTemplates: &recordingStackTemplateRepository{stackTemplate: domain.StackTemplate{
			ID:                        domain.StackTemplateID("stack_template_123"),
			DesiredTemplateRevisionID: domain.TemplateRevisionID("template_123"),
			WorkspaceName:             "mtp_acme_prod_vpc_a13f9c",
			Lifecycle:                 domain.StackTemplateActive,
		}},
		TemplateRevisionMetadata: &recordingTemplateRepository{
			template: domain.TemplateRevision{ID: domain.TemplateRevisionID("template_123"), Status: domain.TemplateRevisionActive},
		},
		RunIDs: fixedTemplateRunIDGenerator{runID: domain.TemplateRunID("run_123")},
		Clock:  fixedClock{now: time.Now()},
	})

	_, err := service.StartTemplateRun(authenticatedContext(), StartTemplateRunCommand{
		TenantID:        domain.TenantID("tenant_123"),
		StackTemplateID: domain.StackTemplateID("stack_template_123"),
		Operation:       domain.OperationPlan,
	})
	if !errors.Is(err, ErrTemplateRunInFlight) {
		t.Fatalf("error = %v, want ErrTemplateRunInFlight", err)
	}
	if runs.created.ID != "" {
		t.Fatalf("created run ID = %q, want no persisted run", runs.created.ID)
	}
	// The insert and the start intent share the transaction, so a refused run
	// must not leave a queue item that would dispatch a workflow for a run row
	// that does not exist.
	if len(work.requests) != 0 {
		t.Fatalf("queued requests = %#v, want none", work.requests)
	}
}

// The run a start hands back is the one the UI caches as the run's detail, so
// it is labelled like any run read back.
func TestStartTemplateRunLabelsTheTriggerActor(t *testing.T) {
	t.Parallel()

	runs := &recordingTemplateRunRepository{}
	service := NewService(Service{
		Work:          &recordingUnitOfWork{templateRuns: runs},
		Authorization: testPlatformAuthorizer(t),
		TemplateRuns:  runs,
		StackTemplates: &recordingStackTemplateRepository{stackTemplate: domain.StackTemplate{
			ID:                        domain.StackTemplateID("stack_template_123"),
			DesiredTemplateRevisionID: domain.TemplateRevisionID("template_123"),
			WorkspaceName:             "mtp_acme_prod_vpc_a13f9c",
			Lifecycle:                 domain.StackTemplateActive,
		}},
		TemplateRevisionMetadata: &recordingTemplateRepository{
			template: domain.TemplateRevision{ID: domain.TemplateRevisionID("template_123"), Status: domain.TemplateRevisionActive},
		},
		Users:  &fakeUserRepository{users: []UserProfile{{Sub: oidcSubject, DisplayName: "Ada Lovelace"}}},
		RunIDs: fixedTemplateRunIDGenerator{runID: domain.TemplateRunID("run_123")},
		Clock:  fixedClock{now: time.Now()},
	})

	run, err := service.StartTemplateRun(authenticatedContext(), StartTemplateRunCommand{
		TenantID:        domain.TenantID("tenant_123"),
		StackTemplateID: domain.StackTemplateID("stack_template_123"),
		Operation:       domain.OperationPlan,
	})
	if err != nil {
		t.Fatalf("StartTemplateRun returned error: %v", err)
	}
	if run.TriggerActorDisplayName != "Ada Lovelace" {
		t.Fatalf("display name = %q, want %q", run.TriggerActorDisplayName, "Ada Lovelace")
	}
}

// The label is looked up before the run is created. Looked up after, a failed
// lookup would report an error for a run that had in fact started, and a
// retry would then be refused as a run already in flight.
func TestStartTemplateRunStartsNothingWhenTheActorLookupFails(t *testing.T) {
	t.Parallel()

	runs := &recordingTemplateRunRepository{}
	work := &recordingUnitOfWork{templateRuns: runs}
	lookupErr := errors.New("users unavailable")
	service := NewService(Service{
		Work:          work,
		Authorization: testPlatformAuthorizer(t),
		TemplateRuns:  runs,
		StackTemplates: &recordingStackTemplateRepository{stackTemplate: domain.StackTemplate{
			ID:                        domain.StackTemplateID("stack_template_123"),
			DesiredTemplateRevisionID: domain.TemplateRevisionID("template_123"),
			WorkspaceName:             "mtp_acme_prod_vpc_a13f9c",
			Lifecycle:                 domain.StackTemplateActive,
		}},
		TemplateRevisionMetadata: &recordingTemplateRepository{
			template: domain.TemplateRevision{ID: domain.TemplateRevisionID("template_123"), Status: domain.TemplateRevisionActive},
		},
		Users:  &fakeUserRepository{lookupErr: lookupErr},
		RunIDs: fixedTemplateRunIDGenerator{runID: domain.TemplateRunID("run_123")},
		Clock:  fixedClock{now: time.Now()},
	})

	_, err := service.StartTemplateRun(authenticatedContext(), StartTemplateRunCommand{
		TenantID:        domain.TenantID("tenant_123"),
		StackTemplateID: domain.StackTemplateID("stack_template_123"),
		Operation:       domain.OperationPlan,
	})
	if !errors.Is(err, lookupErr) {
		t.Fatalf("error = %v, want %v", err, lookupErr)
	}
	if runs.created.ID != "" {
		t.Fatalf("created run ID = %q, want no persisted run", runs.created.ID)
	}
	if len(work.requests) != 0 {
		t.Fatalf("queued requests = %#v, want none", work.requests)
	}
}

// The component does not own a ref. It used to keep the one chosen at install
// and stamp it onto every run, which stayed stale after a revision change: a
// component installed from main and moved to a v2.0.0 revision kept reporting
// main. The run takes the ref from the revision it is actually running.
func TestStartTemplateRunStampsTheRefOfTheRevisionBeingRun(t *testing.T) {
	t.Parallel()

	runs := &recordingTemplateRunRepository{}
	work := &recordingUnitOfWork{templateRuns: runs}
	service := NewService(Service{
		Users:         &fakeUserRepository{},
		Work:          work,
		Authorization: testPlatformAuthorizer(t),
		TemplateRuns:  runs,
		StackTemplates: &recordingStackTemplateRepository{stackTemplate: domain.StackTemplate{
			ID:                        domain.StackTemplateID("stack_template_123"),
			DesiredTemplateRevisionID: domain.TemplateRevisionID("template_123"),
			WorkspaceName:             "mtp_acme_prod_vpc_a13f9c",
			Lifecycle:                 domain.StackTemplateActive,
		}},
		TemplateRevisionMetadata: &recordingTemplateRepository{
			template: domain.TemplateRevision{
				ID:                domain.TemplateRevisionID("template_123"),
				Status:            domain.TemplateRevisionActive,
				SourceRef:         "v2.0.0",
				ResolvedCommitSHA: "sha_v2",
			},
		},
		RunIDs: fixedTemplateRunIDGenerator{runID: domain.TemplateRunID("run_123")},
		Clock:  fixedClock{now: time.Now()},
	})

	run, err := service.StartTemplateRun(authenticatedContext(), StartTemplateRunCommand{
		TenantID:        domain.TenantID("tenant_123"),
		StackTemplateID: domain.StackTemplateID("stack_template_123"),
		Operation:       domain.OperationPlan,
	})
	if err != nil {
		t.Fatalf("StartTemplateRun returned error: %v", err)
	}
	if run.SelectedRef != "v2.0.0" {
		t.Fatalf("run.SelectedRef = %q, want v2.0.0", run.SelectedRef)
	}
	// The ref and the commit describe the same revision, which is the property
	// that was impossible to guarantee while the component carried its own ref.
	if run.ResolvedCommitSHA != "sha_v2" {
		t.Fatalf("run.ResolvedCommitSHA = %q, want sha_v2", run.ResolvedCommitSHA)
	}
}

// The gate is on apply alone — re-planning is how a stale plan gets fixed, so
// gating plan would leave the template stuck.
func TestStartTemplateRunAllowsPlanWhenThePlanIsStale(t *testing.T) {
	t.Parallel()

	runs := &recordingTemplateRunRepository{}
	work := &recordingUnitOfWork{templateRuns: runs}
	service := NewService(Service{
		Users:         &fakeUserRepository{},
		Work:          work,
		Authorization: testPlatformAuthorizer(t),
		TemplateRuns:  runs,
		StackTemplates: &recordingStackTemplateRepository{stackTemplate: domain.StackTemplate{
			ID:                            domain.StackTemplateID("stack_template_123"),
			DesiredTemplateRevisionID:     domain.TemplateRevisionID("template_123"),
			DesiredConfigJSON:             json.RawMessage(`{"region":"eu-west-1"}`),
			WorkspaceName:                 "mtp_acme_prod_vpc_a13f9c",
			Lifecycle:                     domain.StackTemplateActive,
			PendingPlanRunID:              domain.TemplateRunID("run_plan_1"),
			PendingPlanTemplateRevisionID: domain.TemplateRevisionID("template_123"),
			PendingPlanConfigJSON:         json.RawMessage(`{"region":"us-east-1"}`),
		}},
		TemplateRevisionMetadata: &recordingTemplateRepository{
			template: domain.TemplateRevision{ID: domain.TemplateRevisionID("template_123"), Status: domain.TemplateRevisionActive},
		},
		RunIDs: fixedTemplateRunIDGenerator{runID: domain.TemplateRunID("run_123")},
		Clock:  fixedClock{now: time.Now()},
	})

	run, err := service.StartTemplateRun(authenticatedContext(), StartTemplateRunCommand{
		TenantID:        domain.TenantID("tenant_123"),
		StackTemplateID: domain.StackTemplateID("stack_template_123"),
		Operation:       domain.OperationPlan,
	})
	if err != nil {
		t.Fatalf("StartTemplateRun returned error: %v", err)
	}
	if run.ID != domain.TemplateRunID("run_123") {
		t.Fatalf("run.ID = %q, want run_123", run.ID)
	}
}

func TestStartTemplateRunRejectsMissingDesiredRevision(t *testing.T) {
	t.Parallel()

	stackTemplates := &recordingStackTemplateRepository{
		stackTemplate: domain.StackTemplate{
			ID:            domain.StackTemplateID("stack_template_123"),
			WorkspaceName: "mtp_acme_prod_vpc_a13f9c",
			Lifecycle:     domain.StackTemplateActive,
		},
	}
	runs := &recordingTemplateRunRepository{}
	service := NewService(Service{
		Authorization:  testPlatformAuthorizer(t),
		StackTemplates: stackTemplates,
		TemplateRuns:   runs,
		TemplateRevisionMetadata: &recordingTemplateRepository{
			template: domain.TemplateRevision{
				ID:     domain.TemplateRevisionID("template_123"),
				Status: domain.TemplateRevisionActive,
			},
		},
		RunIDs: fixedTemplateRunIDGenerator{runID: domain.TemplateRunID("run_123")},
		Clock:  fixedClock{now: time.Now()},
	})

	_, err := service.StartTemplateRun(authenticatedContext(), StartTemplateRunCommand{
		TenantID:        domain.TenantID("tenant_123"),
		StackTemplateID: domain.StackTemplateID("stack_template_123"),
		Operation:       domain.OperationPlan,
	})
	if !errors.Is(err, ErrStackTemplateNotRunnable) {
		t.Fatalf("error = %v, want ErrStackTemplateNotRunnable", err)
	}
	if runs.created.ID != "" {
		t.Fatalf("created run ID = %q, want no persisted run", runs.created.ID)
	}
}

func TestStartTemplateRunUsesDefaultRunIDGenerator(t *testing.T) {
	t.Parallel()

	stackTemplates := &recordingStackTemplateRepository{
		stackTemplate: domain.StackTemplate{
			ID:                        domain.StackTemplateID("stack_template_123"),
			DesiredTemplateRevisionID: domain.TemplateRevisionID("template_123"),
			WorkspaceName:             "mtp_acme_prod_vpc_a13f9c",
			Lifecycle:                 domain.StackTemplateActive,
		},
	}
	runs := &recordingTemplateRunRepository{}
	service := NewService(Service{
		Users:          &fakeUserRepository{},
		Authorization:  testPlatformAuthorizer(t),
		Work:           &recordingUnitOfWork{templateRuns: runs},
		StackTemplates: stackTemplates,
		TemplateRuns:   runs,
		TemplateRevisionMetadata: &recordingTemplateRepository{
			template: domain.TemplateRevision{
				ID:        domain.TemplateRevisionID("template_123"),
				TenantID:  domain.TenantID("tenant_123"),
				RepoOwner: "acme",
				RepoName:  "infra-templates",
				RootPath:  ".",
				Status:    domain.TemplateRevisionActive,
			},
		},
		Clock: fixedClock{now: time.Now()},
	})

	run, err := service.StartTemplateRun(authenticatedContext(), StartTemplateRunCommand{
		TenantID:        domain.TenantID("tenant_123"),
		StackTemplateID: domain.StackTemplateID("stack_template_123"),
		Operation:       domain.OperationPlan,
	})
	if err != nil {
		t.Fatalf("StartTemplateRun returned error: %v", err)
	}

	if run.ID == "" {
		t.Fatal("run.ID is empty")
	}
	if runs.created.ID != run.ID {
		t.Fatalf("created run ID = %q, want %q", runs.created.ID, run.ID)
	}
}

func TestRegisterTemplateCreatesPendingRegistrationAndDispatchesWorkflow(t *testing.T) {
	t.Parallel()

	ctx := authenticatedContext()
	now := time.Date(2026, 7, 6, 10, 30, 0, 0, time.UTC)
	registrations := &recordingTemplateRegistrationRepository{}
	work := &recordingUnitOfWork{templateRegistrations: registrations}

	service := NewService(Service{
		Authorization:   testPlatformAuthorizer(t),
		Work:            work,
		RegistrationIDs: fixedTemplateRegistrationIDGenerator{id: domain.TemplateRegistrationID("template_registration_123")},
		Clock:           fixedClock{now: now},
	})

	started, err := service.RegisterTemplate(ctx, RegisterTemplateCommand{
		TenantID:  domain.TenantID("tenant_123"),
		RepoOwner: " acme ",
		RepoName:  "infra-templates",
		SourceRef: "v0.0.1",
		RootPath:  "modules/vpc/",
	})
	if err != nil {
		t.Fatalf("RegisterTemplate returned error: %v", err)
	}

	// The template is stored with the cleaned source, under the id that
	// source derives, before any sync has run.
	wantTemplate := domain.SourceTemplate{
		ID:        domain.SourceTemplateIDFor("tenant_123", "acme", "infra-templates", "modules/vpc", "v0.0.1"),
		TenantID:  "tenant_123",
		RepoOwner: "acme",
		RepoName:  "infra-templates",
		SourceRef: "v0.0.1",
		RootPath:  "modules/vpc",
		CreatedAt: now,
	}
	if len(work.createdTemplates) != 1 || !reflect.DeepEqual(work.createdTemplates[0], wantTemplate) {
		t.Fatalf("created templates = %#v, want %#v", work.createdTemplates, wantTemplate)
	}
	// The answer already reads the sync it queued as the template's latest.
	wantStarted := wantTemplate
	wantStarted.LatestSync = &domain.LatestTemplateSync{Status: domain.TemplateRegistrationPending, RequestedAt: now}
	if !reflect.DeepEqual(started.Template, wantStarted) {
		t.Fatalf("started.Template = %#v, want %#v", started.Template, wantStarted)
	}

	registration := started.Sync
	if registration.SourceTemplateID != wantTemplate.ID {
		t.Fatalf("registration.SourceTemplateID = %q, want %q", registration.SourceTemplateID, wantTemplate.ID)
	}
	if registration.ID != domain.TemplateRegistrationID("template_registration_123") {
		t.Fatalf("registration.ID = %q, want template_registration_123", registration.ID)
	}
	if registration.Status != domain.TemplateRegistrationPending {
		t.Fatalf("registration.Status = %q, want %q", registration.Status, domain.TemplateRegistrationPending)
	}
	if registration.RequestedBy != domain.UserID(oidcSubject) {
		t.Fatalf("registration.RequestedBy = %q, want %q", registration.RequestedBy, oidcSubject)
	}
	if !registration.RequestedAt.Equal(now) {
		t.Fatalf("registration.RequestedAt = %v, want %v", registration.RequestedAt, now)
	}
	if registrations.created != registration {
		t.Fatalf("created registration = %#v, want %#v", registrations.created, registration)
	}
	if len(work.requests) != 1 || work.requests[0].Kind != KindStartTemplateSync {
		t.Fatalf("queued requests = %#v, want one start_template_sync request", work.requests)
	}
}

func TestRegisterTemplateRejectsMissingSourceRef(t *testing.T) {
	t.Parallel()

	service := NewService(Service{
		Authorization: testPlatformAuthorizer(t),
	})

	_, err := service.RegisterTemplate(authenticatedContext(), RegisterTemplateCommand{
		TenantID:  domain.TenantID("tenant_123"),
		RepoOwner: "acme",
		RepoName:  "infra-templates",
		RootPath:  "modules/vpc",
	})
	if !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("error = %v, want ErrInvalidCommand", err)
	}
}

func TestRegisterTemplateDoesNotDispatchWhenPersistenceFails(t *testing.T) {
	t.Parallel()

	persistErr := errors.New("database unavailable")
	registrations := &recordingTemplateRegistrationRepository{createErr: persistErr}
	work := &recordingUnitOfWork{templateRegistrations: registrations}
	service := NewService(Service{
		Authorization:   testPlatformAuthorizer(t),
		Work:            work,
		RegistrationIDs: fixedTemplateRegistrationIDGenerator{id: domain.TemplateRegistrationID("template_registration_123")},
	})

	_, err := service.RegisterTemplate(authenticatedContext(), RegisterTemplateCommand{
		TenantID:  domain.TenantID("tenant_123"),
		RepoOwner: "acme",
		RepoName:  "infra-templates",
		SourceRef: "v0.0.1",
		RootPath:  "modules/vpc",
	})
	if !errors.Is(err, persistErr) {
		t.Fatalf("error = %v, want wrapped persistence error", err)
	}
	if len(work.requests) != 0 {
		t.Fatalf("queued requests = %#v, want none", work.requests)
	}
}

func TestApproveRunRecordsApprovalAndQueuesTheApply(t *testing.T) {
	t.Parallel()

	ctx := authenticatedContext()
	now := time.Date(2026, 7, 2, 10, 15, 0, 0, time.UTC)
	runs := &recordingTemplateRunRepository{run: domain.TemplateRun{
		ID:              "run_123",
		TenantID:        "tenant_123",
		StackTemplateID: "stack_template_123",
		Status:          domain.TemplateRunWaitingApproval,
		TriggerActor:    domain.UserID("different-user"),
	}}
	work := &recordingUnitOfWork{templateRuns: runs}

	service := NewService(Service{
		Users:                    &fakeUserRepository{},
		Authorization:            testPlatformAuthorizer(t),
		Work:                     work,
		TemplateRuns:             runs,
		StackTemplates:           approvableStackTemplates(),
		TemplateRevisionMetadata: &recordingTemplateRepository{},
		Clock:                    fixedClock{now: now},
	})

	_, err := service.ApproveRun(ctx, ApproveRunCommand{
		TenantID: domain.TenantID("tenant_123"),
		RunID:    domain.TemplateRunID("run_123"),
	})
	if err != nil {
		t.Fatalf("ApproveRun returned error: %v", err)
	}

	if runs.approval.RunID != domain.TemplateRunID("run_123") {
		t.Fatalf("approval run ID = %q, want run_123", runs.approval.RunID)
	}

	if runs.approval.TenantID != domain.TenantID("tenant_123") {
		t.Fatalf("approval tenant ID = %q, want tenant_123", runs.approval.TenantID)
	}

	if runs.approval.ApprovedBy != domain.UserID(oidcSubject) {
		t.Fatalf("approval actor = %q, want %q", runs.approval.ApprovedBy, oidcSubject)
	}

	if !runs.approval.ApprovedAt.Equal(now) {
		t.Fatalf("approval time = %v, want %v", runs.approval.ApprovedAt, now)
	}

	if len(work.requests) != 1 || work.requests[0].Kind != KindStartTemplateApply {
		t.Fatalf("queued requests = %#v, want one signal_run_approval request", work.requests)
	}
}

// The approval answers with the run as a read of it would show it, already
// approved, so the client can follow the apply without reading it again. It
// is described before the commit and never read back after: a read failing
// there would report an approval that went through as a failure.
func TestApproveRunReturnsTheRunAsApproved(t *testing.T) {
	t.Parallel()

	runs := &readTrackingTemplateRunRepository{recordingTemplateRunRepository: &recordingTemplateRunRepository{
		run: domain.TemplateRun{
			ID:              "run_123",
			TenantID:        "tenant_123",
			StackTemplateID: "stack_template_123",
			Status:          domain.TemplateRunWaitingApproval,
			TriggerActor:    "sub_ada",
		},
		executions: []domain.TemplateRunWorkflowExecution{
			{Phase: domain.RunPhasePlan, Actor: "sub_ada", Status: domain.TemplateRunExecutionSucceeded},
		},
	}}
	work := &recordingUnitOfWork{templateRuns: runs}
	service := NewService(Service{
		Users:                    &fakeUserRepository{users: []UserProfile{{Sub: "sub_ada", DisplayName: "Ada Lovelace"}}},
		Authorization:            testPlatformAuthorizer(t),
		Work:                     work,
		TemplateRuns:             runs,
		StackTemplates:           approvableStackTemplates(),
		TemplateRevisionMetadata: &recordingTemplateRepository{},
		Clock:                    fixedClock{now: time.Now()},
	})

	run, err := service.ApproveRun(authenticatedContext(), ApproveRunCommand{TenantID: "tenant_123", RunID: "run_123"})
	if err != nil {
		t.Fatalf("ApproveRun returned error: %v", err)
	}

	if run.ID != "run_123" || run.Status != domain.TemplateRunApproved {
		t.Fatalf("run = %s %q, want run_123 %q", run.ID, run.Status, domain.TemplateRunApproved)
	}
	if run.TriggerActorDisplayName != "Ada Lovelace" {
		t.Fatalf("trigger actor display name = %q, want Ada Lovelace", run.TriggerActorDisplayName)
	}
	if len(run.Executions) != 1 || run.Executions[0].ActorDisplayName != "Ada Lovelace" {
		t.Fatalf("executions = %#v, want the plan execution, labelled", run.Executions)
	}
	if runs.readsAfterApproval != 0 {
		t.Fatalf("run reads after the approval = %d, want 0", runs.readsAfterApproval)
	}
}

// A run that cannot be described is not approved: describing it comes before
// the commit, so its failure leaves no approval and no queued apply behind.
func TestApproveRunApprovesNothingWhenDescribingTheRunFails(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name          string
		users         *fakeUserRepository
		executionsErr error
	}{
		{name: "executions", users: &fakeUserRepository{}, executionsErr: errors.New("executions unavailable")},
		{name: "display names", users: &fakeUserRepository{lookupErr: errors.New("users unavailable")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			runs := &readTrackingTemplateRunRepository{
				recordingTemplateRunRepository: &recordingTemplateRunRepository{run: domain.TemplateRun{
					ID:              "run_123",
					TenantID:        "tenant_123",
					StackTemplateID: "stack_template_123",
					Status:          domain.TemplateRunWaitingApproval,
				}},
				executionsErr: test.executionsErr,
			}
			work := &recordingUnitOfWork{templateRuns: runs}
			service := NewService(Service{
				Users:                    test.users,
				Authorization:            testPlatformAuthorizer(t),
				Work:                     work,
				TemplateRuns:             runs,
				StackTemplates:           approvableStackTemplates(),
				TemplateRevisionMetadata: &recordingTemplateRepository{},
				Clock:                    fixedClock{now: time.Now()},
			})

			run, err := service.ApproveRun(authenticatedContext(), ApproveRunCommand{TenantID: "tenant_123", RunID: "run_123"})
			if err == nil {
				t.Fatal("ApproveRun returned nil error, want the lookup's")
			}
			if run.ID != "" {
				t.Fatalf("run = %#v, want none", run)
			}
			if work.inTxCalls != 0 || runs.approval.RunID != "" {
				t.Fatalf("approval = %#v after %d transactions, want none", runs.approval, work.inTxCalls)
			}
			if len(work.requests) != 0 {
				t.Fatalf("queued requests = %#v, want none", work.requests)
			}
		})
	}
}

func TestApproveRunQueuesNothingWhenRunIsNotApprovable(t *testing.T) {
	t.Parallel()

	runs := &recordingTemplateRunRepository{run: domain.TemplateRun{ID: "run_123", TenantID: "tenant_123", StackTemplateID: "stack_template_123", Status: domain.TemplateRunWaitingApproval}, approvalErr: ErrRunNotApprovable}
	work := &recordingUnitOfWork{templateRuns: runs}

	service := NewService(Service{
		Users:                    &fakeUserRepository{},
		Authorization:            testPlatformAuthorizer(t),
		Work:                     work,
		TemplateRuns:             runs,
		StackTemplates:           approvableStackTemplates(),
		TemplateRevisionMetadata: &recordingTemplateRepository{},
		Clock:                    fixedClock{now: time.Now()},
	})

	_, err := service.ApproveRun(authenticatedContext(), ApproveRunCommand{
		TenantID: domain.TenantID("tenant_123"),
		RunID:    domain.TemplateRunID("run_123"),
	})
	if !errors.Is(err, ErrRunNotApprovable) {
		t.Fatalf("error = %v, want ErrRunNotApprovable", err)
	}

	if len(work.requests) != 0 {
		t.Fatalf("queued requests = %#v, want none", work.requests)
	}
}

func TestApproveRunAllowsSelfApproval(t *testing.T) {
	t.Parallel()

	ctx := authenticatedContext()
	runs := &recordingTemplateRunRepository{run: domain.TemplateRun{
		ID:              "run_123",
		TenantID:        "tenant_123",
		StackTemplateID: "stack_template_123",
		Status:          domain.TemplateRunWaitingApproval,
		TriggerActor:    domain.UserID(oidcSubject),
	}}
	work := &recordingUnitOfWork{templateRuns: runs}
	audit := &recordingAuditRepository{}

	service := NewService(Service{
		Users:                    &fakeUserRepository{},
		Authorization:            testPlatformAuthorizer(t),
		Work:                     work,
		TemplateRuns:             runs,
		StackTemplates:           approvableStackTemplates(),
		TemplateRevisionMetadata: &recordingTemplateRepository{},
		Audit:                    audit,
	})

	_, err := service.ApproveRun(ctx, ApproveRunCommand{
		TenantID: domain.TenantID("tenant_123"),
		RunID:    domain.TemplateRunID("run_123"),
	})
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}

	if runs.approval.RunID == "" {
		t.Fatalf("approval was not recorded, want approval")
	}

	if len(work.requests) != 1 || work.requests[0].Kind != KindStartTemplateApply {
		t.Fatalf("queued requests = %#v, want one approval signal", work.requests)
	}
}

func TestApproveRunSelfApprovalWorksForPlatformAdmins(t *testing.T) {
	t.Parallel()

	ctx := authentication.ContextWithPrincipal(context.Background(), authentication.Principal{Subject: oidcSubject})
	runs := &recordingTemplateRunRepository{run: domain.TemplateRun{
		ID:              "run_123",
		TenantID:        "tenant_123",
		StackTemplateID: "stack_template_123",
		Status:          domain.TemplateRunWaitingApproval,
		TriggerActor:    domain.UserID(oidcSubject),
	}}
	work := &recordingUnitOfWork{templateRuns: runs}
	audit := &recordingAuditRepository{}

	service := NewService(Service{
		Users:                    &fakeUserRepository{},
		Authorization:            testPlatformAuthorizer(t),
		Work:                     work,
		TemplateRuns:             runs,
		StackTemplates:           approvableStackTemplates(),
		TemplateRevisionMetadata: &recordingTemplateRepository{},
		Audit:                    audit,
	})

	_, err := service.ApproveRun(ctx, ApproveRunCommand{
		TenantID: domain.TenantID("tenant_123"),
		RunID:    domain.TemplateRunID("run_123"),
	})
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}

	if len(work.requests) != 1 || work.requests[0].Kind != KindStartTemplateApply {
		t.Fatalf("queued requests = %#v, want one approval signal", work.requests)
	}
}

func TestApproveRunAuditsSuccessfulApproval(t *testing.T) {
	t.Parallel()

	ctx := authenticatedContext()
	now := time.Date(2026, 7, 2, 10, 15, 0, 0, time.UTC)
	runs := &recordingTemplateRunRepository{run: domain.TemplateRun{
		ID:              "run_123",
		TenantID:        "tenant_123",
		StackTemplateID: "stack_template_123",
		Status:          domain.TemplateRunWaitingApproval,
		TriggerActor:    domain.UserID("different-user"),
	}}
	work := &recordingUnitOfWork{templateRuns: runs}
	audit := &recordingAuditRepository{}

	service := NewService(Service{
		Users:                    &fakeUserRepository{},
		Authorization:            testPlatformAuthorizer(t),
		Work:                     work,
		TemplateRuns:             runs,
		StackTemplates:           approvableStackTemplates(),
		TemplateRevisionMetadata: &recordingTemplateRepository{},
		Clock:                    fixedClock{now: now},
		Audit:                    audit,
	})

	_, err := service.ApproveRun(ctx, ApproveRunCommand{
		TenantID: domain.TenantID("tenant_123"),
		RunID:    domain.TemplateRunID("run_123"),
	})
	if err != nil {
		t.Fatalf("ApproveRun returned error: %v", err)
	}

	if len(work.audits) != 1 {
		t.Fatalf("audit events = %d, want 1", len(work.audits))
	}
	if work.audits[0].Action != domain.AuditActionApprovalGranted {
		t.Fatalf("audit action = %q, want %q", work.audits[0].Action, domain.AuditActionApprovalGranted)
	}
	if work.audits[0].Outcome != domain.AuditOutcomeSuccess {
		t.Fatalf("audit outcome = %q, want %q", work.audits[0].Outcome, domain.AuditOutcomeSuccess)
	}
}

// A run with no plan waiting, such as one still planning or applying, has
// nothing to discard, and running runs cannot be stopped.
func TestDiscardRunRefusesARunWithNoPlanWaiting(t *testing.T) {
	t.Parallel()

	runs := &recordingTemplateRunRepository{run: domain.TemplateRun{ID: "run_123", TenantID: "tenant_123", StackTemplateID: "stack_template_123", Status: domain.TemplateRunRunning}}
	work := &recordingUnitOfWork{templateRuns: runs}

	service := NewService(Service{
		Authorization:  testPlatformAuthorizer(t),
		Work:           work,
		TemplateRuns:   runs,
		StackTemplates: &recordingStackTemplateRepository{stackTemplate: domain.StackTemplate{ID: "stack_template_123", TenantID: "tenant_123", StackID: "stack_123"}},
		Clock:          fixedClock{now: time.Now()},
	})

	err := service.DiscardRun(authenticatedContext(), DiscardRunCommand{
		TenantID: domain.TenantID("tenant_123"),
		RunID:    domain.TemplateRunID("run_123"),
	})
	if !errors.Is(err, ErrRunNotDiscardable) {
		t.Fatalf("error = %v, want ErrRunNotDiscardable", err)
	}
	if runs.discarded.RunID != "" {
		t.Fatalf("discarded = %#v, want nothing", runs.discarded)
	}

	if len(work.requests) != 0 {
		t.Fatalf("queued requests = %#v, want none", work.requests)
	}
}

func TestGetTemplateRunReturnsTenantScopedRun(t *testing.T) {
	t.Parallel()

	runs := &recordingTemplateRunRepository{
		run: domain.TemplateRun{
			ID:              domain.TemplateRunID("run_123"),
			TenantID:        domain.TenantID("tenant_123"),
			StackTemplateID: domain.StackTemplateID("stack_template_123"),
			Operation:       domain.OperationPlan,
			Status:          domain.TemplateRunCompleted,
		},
		executions: []domain.TemplateRunWorkflowExecution{
			{Phase: domain.RunPhasePlan, Actor: "user_requester", Status: domain.TemplateRunExecutionSucceeded},
			{Phase: domain.RunPhaseApply, Actor: "user_unknown", Status: domain.TemplateRunExecutionSucceeded},
		},
	}
	service := NewService(Service{
		Users:          &fakeUserRepository{users: []UserProfile{{Sub: "user_requester", DisplayName: "Priya Raman"}}},
		Authorization:  testPlatformAuthorizer(t),
		TemplateRuns:   runs,
		StackTemplates: &recordingStackTemplateRepository{stackTemplate: domain.StackTemplate{ID: "stack_template_123", TenantID: "tenant_123", StackID: "stack_123"}},
	})
	ctx := authentication.ContextWithPrincipal(context.Background(), authentication.Principal{Subject: oidcSubject})

	run, err := service.GetTemplateRun(ctx, GetTemplateRunCommand{
		TenantID: domain.TenantID("tenant_123"),
		RunID:    domain.TemplateRunID("run_123"),
	})
	if err != nil {
		t.Fatalf("GetTemplateRun returned error: %v", err)
	}

	if run.ID != domain.TemplateRunID("run_123") {
		t.Fatalf("run ID = %q, want run_123", run.ID)
	}
	if runs.gotGetTenantID != domain.TenantID("tenant_123") {
		t.Fatalf("tenant lookup = %q, want tenant_123", runs.gotGetTenantID)
	}
	if runs.gotGetRunID != domain.TemplateRunID("run_123") {
		t.Fatalf("run lookup = %q, want run_123", runs.gotGetRunID)
	}
	if len(run.Executions) != 2 || run.Executions[0].Phase != domain.RunPhasePlan {
		t.Fatalf("executions = %#v, want the run's plan and apply", run.Executions)
	}
	// Each execution's actor is labelled as the trigger actor is: by name,
	// or by subject when openplan has no name for it.
	if got := run.Executions[0].ActorDisplayName; got != "Priya Raman" {
		t.Fatalf("plan actor display name = %q, want Priya Raman", got)
	}
	if got := run.Executions[1].ActorDisplayName; got != "user_unknown" {
		t.Fatalf("apply actor display name = %q, want the subject", got)
	}
}

func TestListTemplateRunsReturnsRunsScopedToStackTemplate(t *testing.T) {
	t.Parallel()

	runs := &recordingTemplateRunRepository{
		list: []domain.TemplateRun{
			{ID: domain.TemplateRunID("run_newer"), TenantID: domain.TenantID("tenant_123"), StackTemplateID: domain.StackTemplateID("stack_template_123"), Operation: domain.OperationPlan, Status: domain.TemplateRunWaitingApproval},
			{ID: domain.TemplateRunID("run_older"), TenantID: domain.TenantID("tenant_123"), StackTemplateID: domain.StackTemplateID("stack_template_123"), Operation: domain.OperationPlan, Status: domain.TemplateRunCompleted},
		},
	}
	service := NewService(Service{
		Users:          &fakeUserRepository{},
		Authorization:  testPlatformAuthorizer(t),
		TemplateRuns:   runs,
		StackTemplates: &recordingStackTemplateRepository{stackTemplate: domain.StackTemplate{ID: "stack_template_123", TenantID: "tenant_123", StackID: "stack_123"}},
	})
	ctx := authentication.ContextWithPrincipal(context.Background(), authentication.Principal{Subject: oidcSubject})

	page, err := service.ListTemplateRuns(ctx, ListTemplateRunsCommand{
		TenantID:        domain.TenantID("tenant_123"),
		StackTemplateID: domain.StackTemplateID("stack_template_123"),
		Page:            FirstPage(),
	})
	got := page.Items
	if err != nil {
		t.Fatalf("ListTemplateRuns returned error: %v", err)
	}

	if runs.gotListTenantID != domain.TenantID("tenant_123") {
		t.Fatalf("tenant lookup = %q, want tenant_123", runs.gotListTenantID)
	}
	if runs.gotListStackTemplateID != domain.StackTemplateID("stack_template_123") {
		t.Fatalf("stack template lookup = %q, want stack_template_123", runs.gotListStackTemplateID)
	}
	if len(got) != 2 || got[0].ID != domain.TemplateRunID("run_newer") {
		t.Fatalf("runs = %#v, want run_newer first", got)
	}
}

// A subject is opaque — Dex's is base64 of a protobuf — so every run the app
// hands out names its trigger actor from the users projection, and a subject
// with no row there labels itself, as a grants list does.
func TestListTemplateRunsLabelsEachTriggerActor(t *testing.T) {
	t.Parallel()

	runs := &recordingTemplateRunRepository{
		list: []domain.TemplateRun{
			{ID: "run_known", TenantID: "tenant_123", StackTemplateID: "stack_template_123", TriggerActor: "sub_ada"},
			{ID: "run_unknown", TenantID: "tenant_123", StackTemplateID: "stack_template_123", TriggerActor: "sub_never_signed_in"},
		},
	}
	service := NewService(Service{
		Authorization:  testPlatformAuthorizer(t),
		TemplateRuns:   runs,
		StackTemplates: &recordingStackTemplateRepository{stackTemplate: domain.StackTemplate{ID: "stack_template_123", TenantID: "tenant_123", StackID: "stack_123"}},
		Users:          &fakeUserRepository{users: []UserProfile{{Sub: "sub_ada", DisplayName: "Ada Lovelace"}}},
	})

	page, err := service.ListTemplateRuns(authenticatedContext(), ListTemplateRunsCommand{
		TenantID:        "tenant_123",
		StackTemplateID: "stack_template_123",
		Page:            FirstPage(),
	})
	got := page.Items
	if err != nil {
		t.Fatalf("ListTemplateRuns returned error: %v", err)
	}

	names := map[domain.TemplateRunID]string{}
	for _, run := range got {
		names[run.ID] = run.TriggerActorDisplayName
	}
	want := map[domain.TemplateRunID]string{"run_known": "Ada Lovelace", "run_unknown": "sub_never_signed_in"}
	if !maps.Equal(names, want) {
		t.Fatalf("display names = %v, want %v", names, want)
	}
}

func TestGetTemplateRunLabelsTheTriggerActor(t *testing.T) {
	t.Parallel()

	service := NewService(Service{
		Authorization:  testPlatformAuthorizer(t),
		TemplateRuns:   &recordingTemplateRunRepository{run: domain.TemplateRun{ID: "run_123", TenantID: "tenant_123", StackTemplateID: "stack_template_123", TriggerActor: "sub_ada"}},
		StackTemplates: &recordingStackTemplateRepository{stackTemplate: domain.StackTemplate{ID: "stack_template_123", TenantID: "tenant_123", StackID: "stack_123"}},
		Users:          &fakeUserRepository{users: []UserProfile{{Sub: "sub_ada", DisplayName: "Ada Lovelace"}}},
	})

	run, err := service.GetTemplateRun(authenticatedContext(), GetTemplateRunCommand{TenantID: "tenant_123", RunID: "run_123"})
	if err != nil {
		t.Fatalf("GetTemplateRun returned error: %v", err)
	}
	if run.TriggerActorDisplayName != "Ada Lovelace" {
		t.Fatalf("display name = %q, want %q", run.TriggerActorDisplayName, "Ada Lovelace")
	}
}

func TestListTemplateRunsNormalizesNilAndRequiresStackTemplateID(t *testing.T) {
	t.Parallel()

	service := NewService(Service{
		Authorization:  testPlatformAuthorizer(t),
		TemplateRuns:   &recordingTemplateRunRepository{},
		StackTemplates: &recordingStackTemplateRepository{stackTemplate: domain.StackTemplate{ID: "stack_template_123", TenantID: "tenant_123", StackID: "stack_123"}},
	})
	ctx := authentication.ContextWithPrincipal(context.Background(), authentication.Principal{Subject: oidcSubject})

	page, err := service.ListTemplateRuns(ctx, ListTemplateRunsCommand{
		TenantID:        domain.TenantID("tenant_123"),
		StackTemplateID: domain.StackTemplateID("stack_template_123"),
		Page:            FirstPage(),
	})
	got := page.Items
	if err != nil {
		t.Fatalf("ListTemplateRuns returned error: %v", err)
	}
	if got == nil {
		t.Fatal("runs = nil, want empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("len(runs) = %d, want 0", len(got))
	}

	_, err = service.ListTemplateRuns(ctx, ListTemplateRunsCommand{TenantID: domain.TenantID("tenant_123"), Page: FirstPage()})
	if !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("error = %v, want ErrInvalidCommand", err)
	}
}

func TestGetTemplateRunRejectsMissingRunID(t *testing.T) {
	t.Parallel()

	service := NewService(Service{TemplateRuns: &recordingTemplateRunRepository{}})

	_, err := service.GetTemplateRun(context.Background(), GetTemplateRunCommand{
		TenantID: domain.TenantID("tenant_123"),
	})
	if !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("error = %v, want ErrInvalidCommand", err)
	}
}

func TestGetTemplateRevisionVariablesReturnsTenantScopedVariables(t *testing.T) {
	t.Parallel()

	templates := &recordingTemplateRepository{
		variables: []domain.TemplateVariable{
			{
				TemplateRevisionID: domain.TemplateRevisionID("template_123"),
				Name:               "region",
				TypeExpression:     "string",
				Required:           true,
			},
		},
	}
	service := NewService(Service{TemplateRevisions: templates, Authorization: testPlatformAuthorizer(t)})

	variables, err := service.GetTemplateRevisionVariables(authenticatedContext(), GetTemplateRevisionVariablesCommand{
		TenantID:           domain.TenantID("tenant_123"),
		TemplateRevisionID: domain.TemplateRevisionID("template_123"),
	})
	if err != nil {
		t.Fatalf("GetTemplateRevisionVariables returned error: %v", err)
	}

	if len(variables) != 1 {
		t.Fatalf("len(variables) = %d, want 1", len(variables))
	}
	if variables[0].Name != "region" {
		t.Fatalf("variable name = %q, want region", variables[0].Name)
	}
	if templates.gotVariablesTenantID != domain.TenantID("tenant_123") {
		t.Fatalf("tenant lookup = %q, want tenant_123", templates.gotVariablesTenantID)
	}
}

func TestGetTemplateRunLogChecksRunOwnershipBeforeReadingLog(t *testing.T) {
	t.Parallel()

	runs := &recordingTemplateRunRepository{
		run: domain.TemplateRun{
			ID:              domain.TemplateRunID("run_123"),
			TenantID:        domain.TenantID("tenant_123"),
			StackTemplateID: domain.StackTemplateID("stack_template_123"),
		},
	}
	logs := &recordingTemplateRunLogReader{content: []byte("plan output\n")}
	metadata := &recordingTemplateRunLogRepository{
		log: domain.TemplateRunLog{
			TenantID:  domain.TenantID("tenant_123"),
			RunID:     domain.TemplateRunID("run_123"),
			Phase:     "plan",
			ObjectKey: "tenants/tenant_123/runs/run_123/logs/plan.log",
		},
	}
	service := NewService(Service{
		Authorization:          testPlatformAuthorizer(t),
		TemplateRuns:           runs,
		StackTemplates:         &recordingStackTemplateRepository{stackTemplate: domain.StackTemplate{ID: "stack_template_123", StackID: "stack_123"}},
		TemplateRunLogs:        logs,
		TemplateRunLogMetadata: metadata,
	})

	content, err := service.GetTemplateRunLog(authenticatedContext(), GetTemplateRunLogCommand{
		TenantID: domain.TenantID("tenant_123"),
		RunID:    domain.TemplateRunID("run_123"),
		Name:     "plan",
	})
	if err != nil {
		t.Fatalf("GetTemplateRunLog returned error: %v", err)
	}

	if string(content) != "plan output\n" {
		t.Fatalf("content = %q, want plan output", string(content))
	}
	if runs.gotGetRunID != domain.TemplateRunID("run_123") {
		t.Fatalf("run lookup = %q, want run_123", runs.gotGetRunID)
	}
	if logs.gotTenantID != domain.TenantID("tenant_123") {
		t.Fatalf("log tenant = %q, want tenant_123", logs.gotTenantID)
	}
	if logs.gotRunID != domain.TemplateRunID("run_123") {
		t.Fatalf("log run = %q, want run_123", logs.gotRunID)
	}
	if logs.gotPhase != "plan" {
		t.Fatalf("log phase = %q, want plan", logs.gotPhase)
	}
	if logs.gotObjectKey != "tenants/tenant_123/runs/run_123/logs/plan.log" {
		t.Fatalf("log object key = %q, want metadata object key", logs.gotObjectKey)
	}
	if metadata.gotGetPhase != "plan" {
		t.Fatalf("metadata phase = %q, want plan", metadata.gotGetPhase)
	}
}

func TestGetTemplateRunLogDoesNotReadLogWhenRunIsMissing(t *testing.T) {
	t.Parallel()

	runs := &recordingTemplateRunRepository{getErr: ErrNotFound}
	logs := &recordingTemplateRunLogReader{content: []byte("plan output\n")}
	service := NewService(Service{
		Authorization:          testPlatformAuthorizer(t),
		TemplateRuns:           runs,
		TemplateRunLogs:        logs,
		TemplateRunLogMetadata: &recordingTemplateRunLogRepository{},
	})

	_, err := service.GetTemplateRunLog(authenticatedContext(), GetTemplateRunLogCommand{
		TenantID: domain.TenantID("tenant_123"),
		RunID:    domain.TemplateRunID("run_123"),
		Name:     "plan",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if logs.gotRunID != "" {
		t.Fatalf("log reader run ID = %q, want no log read", logs.gotRunID)
	}
}

func TestGetTemplateRunLogDoesNotReadObjectWhenMetadataIsMissing(t *testing.T) {
	t.Parallel()

	runs := &recordingTemplateRunRepository{
		run: domain.TemplateRun{
			ID:              domain.TemplateRunID("run_123"),
			TenantID:        domain.TenantID("tenant_123"),
			StackTemplateID: domain.StackTemplateID("stack_template_123"),
		},
	}
	logs := &recordingTemplateRunLogReader{content: []byte("plan output\n")}
	metadata := &recordingTemplateRunLogRepository{getErr: ErrNotFound}
	service := NewService(Service{
		Authorization:          testPlatformAuthorizer(t),
		TemplateRuns:           runs,
		StackTemplates:         &recordingStackTemplateRepository{stackTemplate: domain.StackTemplate{ID: "stack_template_123", TenantID: "tenant_123", StackID: "stack_123"}},
		TemplateRunLogs:        logs,
		TemplateRunLogMetadata: metadata,
	})

	ctx := authentication.ContextWithPrincipal(context.Background(), authentication.Principal{Subject: oidcSubject})
	_, err := service.GetTemplateRunLog(ctx, GetTemplateRunLogCommand{
		TenantID: domain.TenantID("tenant_123"),
		RunID:    domain.TemplateRunID("run_123"),
		Name:     "plan",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if logs.gotRunID != "" {
		t.Fatalf("log reader run ID = %q, want no object read", logs.gotRunID)
	}
}

func TestGetTemplateRunLogMapsMissingLogToNotFound(t *testing.T) {
	t.Parallel()

	runs := &recordingTemplateRunRepository{
		run: domain.TemplateRun{
			ID:              domain.TemplateRunID("run_123"),
			TenantID:        domain.TenantID("tenant_123"),
			StackTemplateID: domain.StackTemplateID("stack_template_123"),
		},
	}
	logs := &recordingTemplateRunLogReader{err: os.ErrNotExist}
	metadata := &recordingTemplateRunLogRepository{
		log: domain.TemplateRunLog{
			TenantID:  domain.TenantID("tenant_123"),
			RunID:     domain.TemplateRunID("run_123"),
			Phase:     "plan",
			ObjectKey: "tenants/tenant_123/runs/run_123/logs/plan.log",
		},
	}
	service := NewService(Service{
		Authorization:          testPlatformAuthorizer(t),
		TemplateRuns:           runs,
		StackTemplates:         &recordingStackTemplateRepository{stackTemplate: domain.StackTemplate{ID: "stack_template_123", StackID: "stack_123"}},
		TemplateRunLogs:        logs,
		TemplateRunLogMetadata: metadata,
	})

	_, err := service.GetTemplateRunLog(authenticatedContext(), GetTemplateRunLogCommand{
		TenantID: domain.TenantID("tenant_123"),
		RunID:    domain.TemplateRunID("run_123"),
		Name:     "plan",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestGetTemplateRunLogRejectsUnsafeName(t *testing.T) {
	t.Parallel()

	service := NewService(Service{
		TemplateRuns:           &recordingTemplateRunRepository{},
		TemplateRunLogs:        &recordingTemplateRunLogReader{},
		TemplateRunLogMetadata: &recordingTemplateRunLogRepository{},
	})

	_, err := service.GetTemplateRunLog(context.Background(), GetTemplateRunLogCommand{
		TenantID: domain.TenantID("tenant_123"),
		RunID:    domain.TemplateRunID("run_123"),
		Name:     "../plan",
	})
	// A name of the wrong shape names a log that can't exist.
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestListTemplateRunLogsChecksRunOwnershipBeforeListingMetadata(t *testing.T) {
	t.Parallel()

	runs := &recordingTemplateRunRepository{
		run: domain.TemplateRun{
			ID:       domain.TemplateRunID("run_123"),
			TenantID: domain.TenantID("tenant_123"),
		},
	}
	metadata := &recordingTemplateRunLogRepository{
		logs: []domain.TemplateRunLog{
			{
				TenantID:    domain.TenantID("tenant_123"),
				RunID:       domain.TemplateRunID("run_123"),
				Phase:       "plan-1",
				RunPhase:    domain.RunPhasePlan,
				Seq:         1,
				Command:     domain.TerraformCommandInit,
				ObjectKey:   "tenants/tenant_123/runs/run_123/logs/plan-1.log",
				ContentType: "text/plain; charset=utf-8",
				SizeBytes:   12,
				UploadedAt:  time.Date(2026, 7, 6, 10, 15, 0, 0, time.UTC),
			},
		},
	}
	service := NewService(Service{
		Authorization:          testPlatformAuthorizer(t),
		TemplateRuns:           runs,
		StackTemplates:         &recordingStackTemplateRepository{stackTemplate: domain.StackTemplate{ID: "stack_template_123", TenantID: "tenant_123", StackID: "stack_123"}},
		TemplateRunLogMetadata: metadata,
	})

	logs, err := service.ListTemplateRunLogs(authentication.ContextWithPrincipal(context.Background(), authentication.Principal{Subject: oidcSubject}), ListTemplateRunLogsCommand{
		TenantID: domain.TenantID("tenant_123"),
		RunID:    domain.TemplateRunID("run_123"),
	})
	if err != nil {
		t.Fatalf("ListTemplateRunLogs returned error: %v", err)
	}

	if len(logs) != 1 {
		t.Fatalf("len(logs) = %d, want 1", len(logs))
	}
	if logs[0].CommandLine != "tofu init" {
		t.Fatalf("command line = %q, want tofu init", logs[0].CommandLine)
	}
	if metadata.gotListRunID != domain.TemplateRunID("run_123") {
		t.Fatalf("metadata run lookup = %q, want run_123", metadata.gotListRunID)
	}
}

// logStateService serves one run of the given status whose log store holds
// log, so a test can read it back through the app's log paths.
func logStateService(t *testing.T, status domain.TemplateRunStatus, log domain.TemplateRunLog) (*Service, *recordingTemplateRunLogReader) {
	t.Helper()

	reader := &recordingTemplateRunLogReader{content: []byte("plan output\n")}
	service := NewService(Service{
		Authorization: testPlatformAuthorizer(t),
		TemplateRuns: &recordingTemplateRunRepository{run: domain.TemplateRun{
			ID:              domain.TemplateRunID("run_123"),
			TenantID:        domain.TenantID("tenant_123"),
			StackTemplateID: domain.StackTemplateID("stack_template_123"),
			Status:          status,
		}},
		StackTemplates:         &recordingStackTemplateRepository{stackTemplate: domain.StackTemplate{ID: "stack_template_123", TenantID: "tenant_123", StackID: "stack_123"}},
		TemplateRunLogs:        reader,
		TemplateRunLogMetadata: &recordingTemplateRunLogRepository{log: log, logs: []domain.TemplateRunLog{log}},
	})
	return service, reader
}

func liveRunLog() domain.TemplateRunLog {
	return domain.TemplateRunLog{
		TenantID: domain.TenantID("tenant_123"),
		RunID:    domain.TemplateRunID("run_123"),
		Phase:    "plan-1",
		RunPhase: domain.RunPhasePlan,
		Seq:      1,
		Command:  domain.TerraformCommandInit,
		State:    domain.TemplateRunLogLive,
	}
}

func TestListTemplateRunLogsDerivesLostForALiveRowOfAFinishedRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     domain.TemplateRunStatus
		wantState  domain.TemplateRunLogState
		wantReason domain.TemplateRunLogLostReason
	}{
		{name: "finished run", status: domain.TemplateRunCompleted, wantState: domain.TemplateRunLogLost, wantReason: domain.LogLostRecordFailed},
		{name: "canceled run", status: domain.TemplateRunCanceled, wantState: domain.TemplateRunLogLost, wantReason: domain.LogLostRunTerminated},
		{name: "running run", status: domain.TemplateRunRunning, wantState: domain.TemplateRunLogLive},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service, _ := logStateService(t, test.status, liveRunLog())
			logs, err := service.ListTemplateRunLogs(authenticatedContext(), ListTemplateRunLogsCommand{
				TenantID: domain.TenantID("tenant_123"),
				RunID:    domain.TemplateRunID("run_123"),
			})
			if err != nil {
				t.Fatalf("ListTemplateRunLogs returned error: %v", err)
			}
			if len(logs) != 1 {
				t.Fatalf("len(logs) = %d, want 1", len(logs))
			}
			if logs[0].State != test.wantState || logs[0].LostReason != test.wantReason {
				t.Fatalf("state = %q/%q, want %q/%q", logs[0].State, logs[0].LostReason, test.wantState, test.wantReason)
			}
		})
	}
}

func TestGetTemplateRunLogRefusesALiveLog(t *testing.T) {
	t.Parallel()

	service, reader := logStateService(t, domain.TemplateRunRunning, liveRunLog())
	_, err := service.GetTemplateRunLog(authenticatedContext(), GetTemplateRunLogCommand{
		TenantID: domain.TenantID("tenant_123"),
		RunID:    domain.TemplateRunID("run_123"),
		Name:     "plan-1",
	})
	if !errors.Is(err, ErrTemplateRunLogLive) {
		t.Fatalf("error = %v, want ErrTemplateRunLogLive", err)
	}
	if reader.gotRunID != "" {
		t.Fatalf("log reader run ID = %q, want no object read", reader.gotRunID)
	}
}

func TestGetTemplateRunLogRefusesALostLog(t *testing.T) {
	t.Parallel()

	lost := liveRunLog()
	lost.State = domain.TemplateRunLogLost
	lost.LostReason = domain.LogLostUploadFailed
	tests := []struct {
		name   string
		status domain.TemplateRunStatus
		log    domain.TemplateRunLog
	}{
		{name: "stored lost", status: domain.TemplateRunFailed, log: lost},
		// A live row of a finished run is lost too, though the store still
		// says live.
		{name: "derived lost", status: domain.TemplateRunCanceled, log: liveRunLog()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service, reader := logStateService(t, test.status, test.log)
			_, err := service.GetTemplateRunLog(authenticatedContext(), GetTemplateRunLogCommand{
				TenantID: domain.TenantID("tenant_123"),
				RunID:    domain.TemplateRunID("run_123"),
				Name:     "plan-1",
			})
			if !errors.Is(err, ErrTemplateRunLogLost) {
				t.Fatalf("error = %v, want ErrTemplateRunLogLost", err)
			}
			if reader.gotRunID != "" {
				t.Fatalf("log reader run ID = %q, want no object read", reader.gotRunID)
			}
		})
	}
}

// The live stream's shared re-check derives as the authorized read does,
// with no principal in the context: the viewer was authorized when its stream
// opened.
func TestTemplateRunLogStateDerivesWithoutAPrincipal(t *testing.T) {
	t.Parallel()

	service, _ := logStateService(t, domain.TemplateRunFailed, liveRunLog())
	state, err := service.TemplateRunLogState(context.Background(), "tenant_123", "run_123", "plan-1")
	if err != nil {
		t.Fatalf("TemplateRunLogState returned error: %v", err)
	}
	if state != domain.TemplateRunLogLost {
		t.Fatalf("state = %q, want lost for a live row of a finished run", state)
	}
}

func TestGetTemplateRunLogMetadataDerivesState(t *testing.T) {
	t.Parallel()

	complete := liveRunLog()
	complete.State = domain.TemplateRunLogComplete
	complete.ObjectKey = "tenants/tenant_123/runs/run_123/logs/plan-1.log"
	tests := []struct {
		name       string
		status     domain.TemplateRunStatus
		log        domain.TemplateRunLog
		wantState  domain.TemplateRunLogState
		wantReason domain.TemplateRunLogLostReason
	}{
		{name: "live row of a running run", status: domain.TemplateRunRunning, log: liveRunLog(), wantState: domain.TemplateRunLogLive},
		// A failed or completed run ran its commands to the end, so a row
		// still live is one the workflow could not record, not a run cut off.
		{name: "live row of a failed run", status: domain.TemplateRunFailed, log: liveRunLog(), wantState: domain.TemplateRunLogLost, wantReason: domain.LogLostRecordFailed},
		{name: "live row of a canceled run", status: domain.TemplateRunCanceled, log: liveRunLog(), wantState: domain.TemplateRunLogLost, wantReason: domain.LogLostRunTerminated},
		{name: "complete row of a finished run", status: domain.TemplateRunCompleted, log: complete, wantState: domain.TemplateRunLogComplete},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service, reader := logStateService(t, test.status, test.log)
			log, err := service.GetTemplateRunLogMetadata(authenticatedContext(), GetTemplateRunLogCommand{
				TenantID: domain.TenantID("tenant_123"),
				RunID:    domain.TemplateRunID("run_123"),
				Name:     "plan-1",
			})
			if err != nil {
				t.Fatalf("GetTemplateRunLogMetadata returned error: %v", err)
			}
			if log.State != test.wantState || log.LostReason != test.wantReason {
				t.Fatalf("state = %q/%q, want %q/%q", log.State, log.LostReason, test.wantState, test.wantReason)
			}
			if log.CommandLine != "tofu init" {
				t.Fatalf("command line = %q, want tofu init", log.CommandLine)
			}
			// Metadata is the row alone; the body stays unread.
			if reader.gotRunID != "" {
				t.Fatalf("log reader run ID = %q, want no object read", reader.gotRunID)
			}
		})
	}
}

type recordingAuditRepository struct {
	events []domain.SecurityAuditEvent
}

func (repository *recordingAuditRepository) AppendAuditEvent(_ context.Context, event domain.SecurityAuditEvent) error {
	repository.events = append(repository.events, event)
	return nil
}

type failingAuditRepository struct{}

func (failingAuditRepository) AppendAuditEvent(_ context.Context, _ domain.SecurityAuditEvent) error {
	return errors.New("database unavailable")
}

func TestCreateStackAuditsOwnerGrant(t *testing.T) {
	t.Parallel()

	ctx := authenticatedContext()
	now := time.Date(2026, 7, 6, 13, 30, 0, 0, time.UTC)
	work := newRecordingWork(&recordingStackRepository{})
	service := NewService(Service{
		Stacks:        &recordingStackRepository{},
		Work:          work,
		Authorization: testPlatformAuthorizer(t),
		StackIDs:      fixedStackIDGenerator{id: domain.StackID("stack_new")},
		Clock:         fixedClock{now: now},
	})

	_, err := service.CreateStack(ctx, CreateStackCommand{
		TenantID: domain.TenantID("tenant_123"),
		Name:     "Acme Prod",
		Tags:     map[string]string{"env": "prod"},
	})
	if err != nil {
		t.Fatalf("CreateStack returned error: %v", err)
	}

	if len(work.audits) != 1 {
		t.Fatalf("audit events = %d, want 1", len(work.audits))
	}
	event := work.audits[0]
	if event.ActorSubject != oidcSubject {
		t.Fatalf("actor_subject = %q, want %q", event.ActorSubject, oidcSubject)
	}
	if event.Action != domain.AuditActionGrant {
		t.Fatalf("action = %q, want %q", event.Action, domain.AuditActionGrant)
	}
	if event.TargetUser != oidcSubject {
		t.Fatalf("target_user = %q, want %q", event.TargetUser, oidcSubject)
	}
	if event.TenantID != domain.TenantID("tenant_123") {
		t.Fatalf("tenant_id = %q, want tenant_123", event.TenantID)
	}
	if event.StackID != domain.StackID("stack_new") {
		t.Fatalf("stack_id = %q, want stack_new", event.StackID)
	}
	if event.NewRole != "owner" {
		t.Fatalf("new_role = %q, want owner", event.NewRole)
	}
	if event.Outcome != domain.AuditOutcomeSuccess {
		t.Fatalf("outcome = %q, want %q", event.Outcome, domain.AuditOutcomeSuccess)
	}
}

func TestCreateStackAuditWriteFailureDoesNotBlockMutation(t *testing.T) {
	t.Parallel()

	ctx := authenticatedContext()
	service := NewService(Service{
		Stacks:        &recordingStackRepository{},
		Work:          newRecordingWork(&recordingStackRepository{}),
		Authorization: testPlatformAuthorizer(t),
		StackIDs:      fixedStackIDGenerator{id: domain.StackID("stack_new")},
		Clock:         fixedClock{now: time.Now()},
		Audit:         failingAuditRepository{},
	})

	stack, err := service.CreateStack(ctx, CreateStackCommand{
		TenantID: domain.TenantID("tenant_123"),
		Name:     "Acme Prod",
		Tags:     map[string]string{},
	})
	if err != nil {
		t.Fatalf("CreateStack returned error: %v", err)
	}
	if stack.ID != domain.StackID("stack_new") {
		t.Fatalf("stack ID = %q, want stack_new", stack.ID)
	}
}

func TestCreateStackWithNilAuditRepositoryDoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := authenticatedContext()
	service := NewService(Service{
		Stacks:        &recordingStackRepository{},
		Work:          newRecordingWork(&recordingStackRepository{}),
		Authorization: testPlatformAuthorizer(t),
		StackIDs:      fixedStackIDGenerator{id: domain.StackID("stack_new")},
		Clock:         fixedClock{now: time.Now()},
		Audit:         nil,
	})

	_, err := service.CreateStack(ctx, CreateStackCommand{
		TenantID: domain.TenantID("tenant_123"),
		Name:     "Acme Prod",
		Tags:     map[string]string{},
	})
	if err != nil {
		t.Fatalf("CreateStack returned error: %v", err)
	}
}

// Both refusals are audited: the caller is told 404 when they can't view the
// stack, but it is still a refused write.
func TestAddTemplateToStackAuditsAuthorizationDenial(t *testing.T) {
	t.Parallel()

	for _, refusal := range writeRefusals {
		t.Run(refusal.name, func(t *testing.T) {
			t.Parallel()
			testAddTemplateToStackAuditsRefusal(t, refusal)
		})
	}
}

func testAddTemplateToStackAuditsRefusal(t *testing.T, refusal writeRefusal) {
	t.Helper()

	auth := newTestAuthorization(t)
	refusal.grant(t, auth, oidcSubject, "stack_123")
	audit := &recordingAuditRepository{}
	service := NewService(Service{
		Authorization: auth,
		Stacks: &recordingStackRepository{
			stack: domain.Stack{ID: "stack_123", TenantID: "tenant_123", Slug: "acme-prod"},
		},
		TemplateRevisionMetadata: &recordingTemplateRepository{
			template: domain.TemplateRevision{ID: "template_123", TenantID: "tenant_123", Status: domain.TemplateRevisionActive},
		},
		TemplateRevisions: &recordingTemplateRepository{
			variables: []domain.TemplateVariable{{Name: "region", Required: true, HasDefault: true}},
		},
		StackTemplateInstaller: &recordingStackTemplateInstaller{},
		StackTemplateIDs:       fixedStackTemplateIDGenerator{id: domain.StackTemplateID("stack_template_a1b2c3d4")},
		Audit:                  audit,
	})

	ctx := authentication.ContextWithPrincipal(context.Background(), authentication.Principal{
		Subject: oidcSubject,
	})

	_, err := service.AddTemplateToStack(ctx, AddTemplateToStackCommand{
		TenantID:           domain.TenantID("tenant_123"),
		StackID:            domain.StackID("stack_123"),
		TemplateRevisionID: domain.TemplateRevisionID("template_123"),
		ConfigJSON:         json.RawMessage(`{"region":"us-east-1"}`),
	})
	if !errors.Is(err, refusal.want) {
		t.Fatalf("error = %v, want %v", err, refusal.want)
	}

	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(audit.events))
	}
	event := audit.events[0]
	if event.Action != domain.AuditActionFailedAccessAttempt {
		t.Fatalf("action = %q, want %q", event.Action, domain.AuditActionFailedAccessAttempt)
	}
	if event.Outcome != domain.AuditOutcomeFailure {
		t.Fatalf("outcome = %q, want %q", event.Outcome, domain.AuditOutcomeFailure)
	}
	if event.ActorSubject != oidcSubject {
		t.Fatalf("actor_subject = %q, want %q", event.ActorSubject, oidcSubject)
	}
}

type recordingStackTemplateRepository struct {
	stackTemplate                domain.StackTemplate
	getErr                       error
	gotTenantID                  domain.TenantID
	gotID                        domain.StackTemplateID
	gotConfigJSON                json.RawMessage
	gotDesiredTemplateRevisionID domain.TemplateRevisionID
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

func (repository *recordingStackTemplateRepository) UpdateStackTemplateDesiredRevision(_ context.Context, tenantID domain.TenantID, id domain.StackTemplateID, templateRevisionID domain.TemplateRevisionID, configJSON json.RawMessage) (domain.StackTemplate, error) {
	repository.gotTenantID = tenantID
	repository.gotID = id
	repository.gotDesiredTemplateRevisionID = templateRevisionID
	repository.gotConfigJSON = configJSON
	updated := repository.stackTemplate
	updated.DesiredTemplateRevisionID = templateRevisionID
	updated.DesiredConfigJSON = configJSON
	return updated, nil
}

type recordingStackRepository struct {
	created         domain.Stack
	stack           domain.Stack
	list            []domain.Stack
	view            StackView
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

func (repository *recordingStackRepository) GetStackWithTemplates(_ context.Context, tenantID domain.TenantID, stackID domain.StackID) (StackView, error) {
	repository.gotTenantID = tenantID
	repository.gotStackID = stackID
	if repository.getViewErr != nil {
		return StackView{}, repository.getViewErr
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

func (repository *recordingStackRepository) ListStacksPage(_ context.Context, tenantID domain.TenantID, after *StackPageCursor, limit int, query string) ([]domain.Stack, error) {
	repository.gotListTenantID = tenantID
	if repository.listErr != nil {
		return nil, repository.listErr
	}
	stacks := matchingStacks(repository.list, query)
	start := 0
	if after != nil {
		for i, stack := range stacks {
			if stack.ID == after.ID && stack.CreatedAt.Equal(after.CreatedAt) {
				start = i + 1
				break
			}
		}
	}
	end := min(start+limit, len(stacks))
	return append([]domain.Stack(nil), stacks[start:end]...), nil
}

type recordingTemplateRunRepository struct {
	created                domain.TemplateRun
	run                    domain.TemplateRun
	list                   []domain.TemplateRun
	executions             []domain.TemplateRunWorkflowExecution
	approval               domain.TemplateRunApproval
	gotGetTenantID         domain.TenantID
	gotGetRunID            domain.TemplateRunID
	gotListTenantID        domain.TenantID
	gotListStackTemplateID domain.StackTemplateID
	getErr                 error
	createErr              error
	approvalErr            error
	discarded              domain.TemplateRunDiscard
	// createdRunNumber is what CreateTemplateRun reports it assigned.
	createdRunNumber int
}

func (repository *recordingTemplateRunRepository) CreateTemplateRun(_ context.Context, run domain.TemplateRun) (int, error) {
	if repository.createErr != nil {
		return 0, repository.createErr
	}
	repository.created = run
	return repository.createdRunNumber, nil
}

func (repository *recordingTemplateRunRepository) GetTemplateRun(_ context.Context, tenantID domain.TenantID, runID domain.TemplateRunID) (domain.TemplateRun, error) {
	repository.gotGetTenantID = tenantID
	repository.gotGetRunID = runID
	if repository.getErr != nil {
		return domain.TemplateRun{}, repository.getErr
	}
	return repository.run, nil
}

func (repository *recordingTemplateRunRepository) ListTemplateRunExecutions(context.Context, domain.TenantID, domain.TemplateRunID) ([]domain.TemplateRunWorkflowExecution, error) {
	return repository.executions, nil
}

func (repository *recordingTemplateRunRepository) ListTemplateRuns(_ context.Context, tenantID domain.TenantID, stackTemplateID domain.StackTemplateID) ([]domain.TemplateRun, error) {
	repository.gotListTenantID = tenantID
	repository.gotListStackTemplateID = stackTemplateID
	return repository.list, nil
}

// ListTemplateRunsPage pages list, which tests give newest first, as the
// store does by run number.
func (repository *recordingTemplateRunRepository) ListTemplateRunsPage(_ context.Context, tenantID domain.TenantID, stackTemplateID domain.StackTemplateID, beforeRunNumber, limit int) ([]domain.TemplateRun, error) {
	repository.gotListTenantID = tenantID
	repository.gotListStackTemplateID = stackTemplateID
	runs := []domain.TemplateRun{}
	for _, run := range repository.list {
		if (beforeRunNumber == 0 || run.RunNumber < beforeRunNumber) && len(runs) < limit {
			runs = append(runs, run)
		}
	}
	return runs, nil
}

func (repository *recordingTemplateRunRepository) GetTemplateRunByNumber(_ context.Context, tenantID domain.TenantID, stackTemplateID domain.StackTemplateID, runNumber int) (domain.TemplateRun, error) {
	repository.gotListTenantID = tenantID
	repository.gotListStackTemplateID = stackTemplateID
	for _, run := range repository.list {
		if run.StackTemplateID == stackTemplateID && run.RunNumber == runNumber {
			return run, nil
		}
	}
	return domain.TemplateRun{}, ErrNotFound
}

func (repository *recordingTemplateRunRepository) ApproveTemplateRun(_ context.Context, approval domain.TemplateRunApproval) error {
	if repository.approvalErr != nil {
		return repository.approvalErr
	}
	repository.approval = approval
	return nil
}

// readTrackingTemplateRunRepository counts the run reads that happen after the
// approval is written, and can fail the executions lookup.
type readTrackingTemplateRunRepository struct {
	*recordingTemplateRunRepository
	executionsErr      error
	approved           bool
	readsAfterApproval int
}

func (repository *readTrackingTemplateRunRepository) GetTemplateRun(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID) (domain.TemplateRun, error) {
	if repository.approved {
		repository.readsAfterApproval++
	}
	return repository.recordingTemplateRunRepository.GetTemplateRun(ctx, tenantID, runID)
}

func (repository *readTrackingTemplateRunRepository) ListTemplateRunExecutions(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID) ([]domain.TemplateRunWorkflowExecution, error) {
	if repository.approved {
		repository.readsAfterApproval++
	}
	if repository.executionsErr != nil {
		return nil, repository.executionsErr
	}
	return repository.recordingTemplateRunRepository.ListTemplateRunExecutions(ctx, tenantID, runID)
}

func (repository *readTrackingTemplateRunRepository) ApproveTemplateRun(ctx context.Context, approval domain.TemplateRunApproval) error {
	repository.approved = true
	return repository.recordingTemplateRunRepository.ApproveTemplateRun(ctx, approval)
}

// DiscardTemplateRun discards only a run waiting for approval or approved, as
// the real store does.
func (repository *recordingTemplateRunRepository) DiscardTemplateRun(_ context.Context, discard domain.TemplateRunDiscard) (bool, error) {
	if repository.run.Status != domain.TemplateRunWaitingApproval && repository.run.Status != domain.TemplateRunApproved {
		return false, nil
	}
	repository.discarded = discard
	return true, nil
}

type recordingTemplateRunLogReader struct {
	content      []byte
	err          error
	gotTenantID  domain.TenantID
	gotRunID     domain.TemplateRunID
	gotPhase     string
	gotObjectKey string
}

func (reader *recordingTemplateRunLogReader) ReadTemplateRunLog(_ context.Context, log domain.TemplateRunLog) ([]byte, error) {
	reader.gotTenantID = log.TenantID
	reader.gotRunID = log.RunID
	reader.gotPhase = log.Phase
	reader.gotObjectKey = log.ObjectKey
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
	created   domain.TemplateRegistration
	createErr error
}

func (repository *recordingTemplateRegistrationRepository) CreateTemplateRegistration(_ context.Context, registration domain.TemplateRegistration) error {
	if repository.createErr != nil {
		return repository.createErr
	}
	repository.created = registration
	return nil
}

// recordingSourceTemplateRepository serves one template, or getErr, and
// pages revisions and templates the tests give newest first.
type recordingSourceTemplateRepository struct {
	template          domain.SourceTemplate
	templates         []domain.SourceTemplate
	revisions         []domain.TemplateRevision
	getErr            error
	deleteErr         error
	gotTenantID       domain.TenantID
	gotID             domain.SourceTemplateID
	deletedID         domain.SourceTemplateID
	revisionPageCalls int
}

func (repository *recordingSourceTemplateRepository) DeleteSourceTemplate(_ context.Context, tenantID domain.TenantID, id domain.SourceTemplateID) error {
	repository.gotTenantID = tenantID
	if repository.deleteErr != nil {
		return repository.deleteErr
	}
	repository.deletedID = id
	return nil
}

func (repository *recordingSourceTemplateRepository) GetSourceTemplate(_ context.Context, tenantID domain.TenantID, id domain.SourceTemplateID) (domain.SourceTemplate, error) {
	repository.gotTenantID = tenantID
	repository.gotID = id
	if repository.getErr != nil {
		return domain.SourceTemplate{}, repository.getErr
	}
	return repository.template, nil
}

func (repository *recordingSourceTemplateRepository) ListSourceTemplatesPage(_ context.Context, tenantID domain.TenantID, after *SourceTemplateCursor, limit int) ([]domain.SourceTemplate, error) {
	repository.gotTenantID = tenantID
	return pageAfter(repository.templates, limit, func(template domain.SourceTemplate) bool {
		return after != nil && template.ID == after.ID
	}), nil
}

func (repository *recordingSourceTemplateRepository) ListTemplateRevisionsBySourcePage(_ context.Context, tenantID domain.TenantID, sourceTemplateID domain.SourceTemplateID, after *TemplateRevisionCursor, limit int) ([]domain.TemplateRevision, error) {
	repository.gotTenantID = tenantID
	repository.gotID = sourceTemplateID
	repository.revisionPageCalls++
	return pageAfter(repository.revisions, limit, func(revision domain.TemplateRevision) bool {
		return after != nil && revision.ID == after.ID
	}), nil
}

// pageAfter returns up to limit items after the one isCursor matches, or from
// the start when none does.
func pageAfter[T any](items []T, limit int, isCursor func(T) bool) []T {
	start := 0
	for i, item := range items {
		if isCursor(item) {
			start = i + 1
			break
		}
	}
	end := min(start+limit, len(items))
	return append([]T{}, items[start:end]...)
}

// approvableStackTemplates is a stack template whose pending plan is run_123
// and still describes desired state, which is what ApproveRun requires.
func approvableStackTemplates() *recordingStackTemplateRepository {
	return &recordingStackTemplateRepository{stackTemplate: domain.StackTemplate{
		ID:                    "stack_template_123",
		TenantID:              "tenant_123",
		StackID:               "stack_123",
		DesiredConfigJSON:     json.RawMessage(`{}`),
		PendingPlanRunID:      "run_123",
		PendingPlanConfigJSON: json.RawMessage(`{}`),
	}}
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

func (repository *recordingTemplateRepository) GetTemplateRevision(_ context.Context, tenantID domain.TenantID, templateRevisionID domain.TemplateRevisionID) (domain.TemplateRevision, error) {
	repository.gotGetTemplateTenantID = tenantID
	repository.gotGetTemplateRevisionID = templateRevisionID
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

func (repository *recordingTemplateRepository) GetTemplateRevisionVariables(_ context.Context, tenantID domain.TenantID, templateRevisionID domain.TemplateRevisionID) ([]domain.TemplateVariable, error) {
	repository.gotVariablesTenantID = tenantID
	repository.gotVariablesTemplateRevisionID = templateRevisionID
	if repository.variablesErr != nil {
		return nil, repository.variablesErr
	}
	return repository.variables, nil
}

type recordingWorkflowDispatcher struct {
	input                 domain.TemplateRunWorkflowInput
	startTemplateRunCalls int
	syncInput             domain.TemplateSyncWorkflowInput
	approvalRunID         domain.TemplateRunID
}

func (dispatcher *recordingWorkflowDispatcher) StartTemplateRun(_ context.Context, input domain.TemplateRunWorkflowInput) error {
	dispatcher.startTemplateRunCalls++
	dispatcher.input = input
	return nil
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

func (dispatcher *recordingWorkflowDispatcher) StartTemplateSync(_ context.Context, input domain.TemplateSyncWorkflowInput) error {
	dispatcher.syncInput = input
	return nil
}

// StartTemplateApply records a direct start, which the service never makes:
// approval queues the start instead, in the approval's transaction.
func (dispatcher *recordingWorkflowDispatcher) StartTemplateApply(_ context.Context, input domain.TemplateRunWorkflowInput) error {
	dispatcher.approvalRunID = input.RunID
	return nil
}

type fixedTemplateRunIDGenerator struct {
	runID domain.TemplateRunID
}

type fixedStackTemplateIDGenerator struct {
	id domain.StackTemplateID
}

func (generator fixedStackTemplateIDGenerator) NewStackTemplateID() domain.StackTemplateID {
	return generator.id
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

type fixedStackIDGenerator struct {
	id domain.StackID
}

func (generator fixedStackIDGenerator) NewStackID() domain.StackID {
	return generator.id
}

type fixedClock struct {
	now time.Time
}

func (clock fixedClock) Now() time.Time {
	return clock.now
}

// recordingUnitOfWork stands in for the Postgres unit of work. It applies
// writes immediately rather than staging them: rollback semantics are proven
// against a real database in internal/postgres/unitofwork_test.go, so here it
// only needs to record what the service asked for.
type recordingUnitOfWork struct {
	stacks                StackRepository
	templateRuns          TemplateRunRepository
	templateRegistrations *recordingTemplateRegistrationRepository
	audits                []domain.SecurityAuditEvent
	requests              []queue.Request
	err                   error
	inTxCalls             int
	// createdTemplates are the templates a register asked to store.
	// storedTemplate, when set, is what the store already holds for the
	// source, as after an earlier register; createTemplateErr fails the write.
	createdTemplates  []domain.SourceTemplate
	storedTemplate    domain.SourceTemplate
	createTemplateErr error
}

func newRecordingWork(stacks StackRepository) *recordingUnitOfWork {
	return &recordingUnitOfWork{stacks: stacks}
}

func (unit *recordingUnitOfWork) InTx(ctx context.Context, fn func(context.Context, TxRepo, queue.Enqueuer) error) error {
	unit.inTxCalls++
	if unit.err != nil {
		return unit.err
	}
	// No transaction is installed on the context: these tests run against an
	// in-memory engine, where an authorization write takes the datastore's
	// non-transactional path. What is atomic here is asserted against Postgres
	// in internal/authorization's own suite.
	return fn(ctx, unit, unit)
}

func (unit *recordingUnitOfWork) CreateStack(ctx context.Context, stack domain.Stack) error {
	if unit.stacks == nil {
		return nil
	}
	return unit.stacks.CreateStack(ctx, stack)
}

func (unit *recordingUnitOfWork) CreateSourceTemplate(_ context.Context, template domain.SourceTemplate) (domain.SourceTemplate, error) {
	unit.createdTemplates = append(unit.createdTemplates, template)
	if unit.createTemplateErr != nil {
		return domain.SourceTemplate{}, unit.createTemplateErr
	}
	if unit.storedTemplate.ID != "" {
		return unit.storedTemplate, nil
	}
	return template, nil
}

func (unit *recordingUnitOfWork) AppendAuditEvent(_ context.Context, event domain.SecurityAuditEvent) error {
	unit.audits = append(unit.audits, event)
	return nil
}

func (unit *recordingUnitOfWork) CreateTemplateRun(ctx context.Context, run domain.TemplateRun) (int, error) {
	if unit.templateRuns == nil {
		return 0, nil
	}
	return unit.templateRuns.CreateTemplateRun(ctx, run)
}

func (unit *recordingUnitOfWork) CreateTemplateRegistration(ctx context.Context, registration domain.TemplateRegistration) error {
	if unit.templateRegistrations == nil {
		return nil
	}
	return unit.templateRegistrations.CreateTemplateRegistration(ctx, registration)
}

func (unit *recordingUnitOfWork) ApproveTemplateRun(ctx context.Context, approval domain.TemplateRunApproval) error {
	if unit.templateRuns == nil {
		return nil
	}
	return unit.templateRuns.ApproveTemplateRun(ctx, approval)
}

func (unit *recordingUnitOfWork) DiscardTemplateRun(ctx context.Context, discard domain.TemplateRunDiscard) (bool, error) {
	discarder, ok := unit.templateRuns.(interface {
		DiscardTemplateRun(context.Context, domain.TemplateRunDiscard) (bool, error)
	})
	if !ok {
		return false, nil
	}
	return discarder.DiscardTemplateRun(ctx, discard)
}

func (unit *recordingUnitOfWork) Enqueue(_ context.Context, requests ...queue.Request) error {
	unit.requests = append(unit.requests, requests...)
	return nil
}

func TestRegisterTemplatePairsRegistrationWithSyncIntentInTransaction(t *testing.T) {
	t.Parallel()

	registrations := &recordingTemplateRegistrationRepository{}
	work := &recordingUnitOfWork{templateRegistrations: registrations}
	service := NewService(Service{
		Authorization:   testPlatformAuthorizer(t),
		Work:            work,
		RegistrationIDs: fixedTemplateRegistrationIDGenerator{id: "registration_123"},
		Clock:           fixedClock{now: time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)},
	})

	started, err := service.RegisterTemplate(authenticatedContext(), RegisterTemplateCommand{
		TenantID: "tenant_123", RepoOwner: "acme", RepoName: "infra", SourceRef: "main", RootPath: "modules/vpc",
	})
	if err != nil {
		t.Fatalf("RegisterTemplate returned error: %v", err)
	}
	registration := started.Sync
	if work.inTxCalls != 1 || len(work.createdTemplates) != 1 || registrations.created.ID != registration.ID {
		t.Fatalf("transaction calls = %d, templates = %#v, registration = %#v", work.inTxCalls, work.createdTemplates, registrations.created)
	}
	if len(work.requests) != 1 || work.requests[0].Kind != KindStartTemplateSync || work.requests[0].ActorSubject != oidcSubject || work.requests[0].TenantID != "tenant_123" {
		t.Fatalf("requests = %#v", work.requests)
	}
	var payload StartTemplateSyncPayload
	if err := json.Unmarshal(work.requests[0].Payload, &payload); err != nil {
		t.Fatalf("decode sync intent: %v", err)
	}
	if payload.RegistrationID != registration.ID || payload.TenantID != registration.TenantID || payload.RepoOwner != "acme" || payload.RepoName != "infra" || payload.SourceRef != "main" || payload.RootPath != "modules/vpc" {
		t.Fatalf("sync payload = %#v", payload)
	}
	// The sync writes into this template and never creates one, so a payload
	// without it fails every sync.
	if payload.SourceTemplateID == "" || payload.SourceTemplateID != started.Template.ID {
		t.Fatalf("sync payload SourceTemplateID = %q, want %q", payload.SourceTemplateID, started.Template.ID)
	}
}

// A repeat register lands on the stored template, whatever id it was stored
// under, and still queues a sync of it (owner decision, 2026-10-21).
func TestRegisterTemplateAgainSyncsTheStoredTemplate(t *testing.T) {
	t.Parallel()

	stored := domain.SourceTemplate{
		ID: "source_template_stored", TenantID: "tenant_123", RepoOwner: "acme", RepoName: "infra", SourceRef: "main", RootPath: "modules/vpc",
		Name: "vpc", LatestTemplateRevisionID: "revision_1",
	}
	registrations := &recordingTemplateRegistrationRepository{}
	work := &recordingUnitOfWork{templateRegistrations: registrations, storedTemplate: stored}
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	service := NewService(Service{
		Authorization:   testPlatformAuthorizer(t),
		Work:            work,
		RegistrationIDs: fixedTemplateRegistrationIDGenerator{id: "registration_again"},
		Clock:           fixedClock{now: now},
	})

	started, err := service.RegisterTemplate(authenticatedContext(), RegisterTemplateCommand{
		TenantID: "tenant_123", RepoOwner: "acme", RepoName: "infra", SourceRef: "main", RootPath: "modules/vpc",
	})
	require.NoError(t, err)

	wantTemplate := stored
	wantTemplate.LatestSync = &domain.LatestTemplateSync{Status: domain.TemplateRegistrationPending, RequestedAt: now}
	assert.Equal(t, wantTemplate, started.Template)
	assert.Equal(t, domain.SourceTemplateID("source_template_stored"), registrations.created.SourceTemplateID)
	require.Len(t, work.requests, 1)
	var payload StartTemplateSyncPayload
	require.NoError(t, json.Unmarshal(work.requests[0].Payload, &payload))
	assert.Equal(t, domain.SourceTemplateID("source_template_stored"), payload.SourceTemplateID)
	assert.Equal(t, domain.TemplateRegistrationID("registration_again"), payload.RegistrationID)
}

func TestRegisterTemplateQueuesNoSyncWhenTheTemplateIsNotStored(t *testing.T) {
	t.Parallel()

	storeErr := errors.New("database unavailable")
	registrations := &recordingTemplateRegistrationRepository{}
	work := &recordingUnitOfWork{templateRegistrations: registrations, createTemplateErr: storeErr}
	service := NewService(Service{Authorization: testPlatformAuthorizer(t), Work: work})

	_, err := service.RegisterTemplate(authenticatedContext(), RegisterTemplateCommand{
		TenantID: "tenant_123", RepoOwner: "acme", RepoName: "infra", SourceRef: "main", RootPath: ".",
	})
	require.ErrorIs(t, err, storeErr)
	assert.Empty(t, registrations.created.ID)
	assert.Empty(t, work.requests)
}

func TestStartTemplateSyncQueuesASyncOfTheStoredTemplate(t *testing.T) {
	t.Parallel()

	template := domain.SourceTemplate{
		ID: "source_template_vpc", TenantID: "tenant_123", RepoOwner: "acme", RepoName: "infra", SourceRef: "main", RootPath: "modules/vpc",
		LatestSync: &domain.LatestTemplateSync{
			Status: domain.TemplateRegistrationFailed, ErrorSummary: "clone failed",
			RequestedAt: time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC), CompletedAt: time.Date(2026, 10, 20, 9, 1, 0, 0, time.UTC),
		},
	}
	templates := &recordingSourceTemplateRepository{template: template}
	registrations := &recordingTemplateRegistrationRepository{}
	work := &recordingUnitOfWork{templateRegistrations: registrations}
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	service := NewService(Service{
		Authorization:   testPlatformAuthorizer(t),
		Work:            work,
		SourceTemplates: templates,
		RegistrationIDs: fixedTemplateRegistrationIDGenerator{id: "registration_sync"},
		Clock:           fixedClock{now: now},
	})

	started, err := service.StartTemplateSync(authenticatedContext(), StartTemplateSyncCommand{TenantID: "tenant_123", TemplateID: "source_template_vpc"})
	require.NoError(t, err)

	// The sync just queued replaces the failed one as the template's latest.
	wantTemplate := template
	wantTemplate.LatestSync = &domain.LatestTemplateSync{Status: domain.TemplateRegistrationPending, RequestedAt: now}
	assert.Equal(t, wantTemplate, started.Template)
	assert.Equal(t, domain.TenantID("tenant_123"), templates.gotTenantID)
	assert.Equal(t, domain.SourceTemplateID("source_template_vpc"), templates.gotID)
	assert.Empty(t, work.createdTemplates, "a Sync never stores a template")
	want := domain.TemplateRegistration{
		ID: "registration_sync", TenantID: "tenant_123", SourceTemplateID: "source_template_vpc",
		RepoOwner: "acme", RepoName: "infra", SourceRef: "main", RootPath: "modules/vpc",
		Status: domain.TemplateRegistrationPending, RequestedBy: oidcSubject, RequestedAt: now,
	}
	assert.Equal(t, want, started.Sync)
	assert.Equal(t, want, registrations.created)
	require.Len(t, work.requests, 1)
	assert.Equal(t, KindStartTemplateSync, work.requests[0].Kind)
	var payload StartTemplateSyncPayload
	require.NoError(t, json.Unmarshal(work.requests[0].Payload, &payload))
	assert.Equal(t, StartTemplateSyncPayload{
		RegistrationID: "registration_sync", TenantID: "tenant_123", SourceTemplateID: "source_template_vpc",
		RepoOwner: "acme", RepoName: "infra", SourceRef: "main", RootPath: "modules/vpc",
	}, payload)
}

func TestStartTemplateSyncOfAnUnknownTemplateIsNotFound(t *testing.T) {
	t.Parallel()

	work := &recordingUnitOfWork{}
	service := NewService(Service{
		Authorization:   testPlatformAuthorizer(t),
		Work:            work,
		SourceTemplates: &recordingSourceTemplateRepository{getErr: ErrNotFound},
	})

	_, err := service.StartTemplateSync(authenticatedContext(), StartTemplateSyncCommand{TenantID: "tenant_123", TemplateID: "source_template_gone"})
	require.ErrorIs(t, err, ErrNotFound)
	assert.Zero(t, work.inTxCalls)
	assert.Empty(t, work.requests)
}

func TestGetTemplateReadsTheTenantsTemplate(t *testing.T) {
	t.Parallel()

	template := domain.SourceTemplate{ID: "source_template_vpc", TenantID: "tenant_123", Name: "vpc"}
	templates := &recordingSourceTemplateRepository{template: template}
	service := NewService(Service{Authorization: testPlatformAuthorizer(t), SourceTemplates: templates})

	got, err := service.GetTemplate(authenticatedContext(), GetTemplateCommand{TenantID: "tenant_123", TemplateID: "source_template_vpc"})
	require.NoError(t, err)
	assert.Equal(t, template, got)
	assert.Equal(t, domain.TenantID("tenant_123"), templates.gotTenantID)

	templates.getErr = ErrNotFound
	_, err = service.GetTemplate(authenticatedContext(), GetTemplateCommand{TenantID: "tenant_123", TemplateID: "source_template_gone"})
	require.ErrorIs(t, err, ErrNotFound)

	_, err = service.GetTemplate(authenticatedContext(), GetTemplateCommand{TenantID: "tenant_123"})
	require.ErrorIs(t, err, ErrInvalidCommand)
}

func TestDeleteTemplateDeletesTheTenantsTemplate(t *testing.T) {
	t.Parallel()

	templates := &recordingSourceTemplateRepository{}
	service := NewService(Service{Authorization: testPlatformAuthorizer(t), SourceTemplates: templates})

	err := service.DeleteTemplate(authenticatedContext(), DeleteTemplateCommand{TenantID: "tenant_123", TemplateID: "source_template_vpc"})
	require.NoError(t, err)
	assert.Equal(t, domain.TenantID("tenant_123"), templates.gotTenantID)
	assert.Equal(t, domain.SourceTemplateID("source_template_vpc"), templates.deletedID)
}

// The store's refusals reach the caller as the sentinels the API maps: in
// use is a 409, an unknown template a 404.
func TestDeleteTemplateRefusals(t *testing.T) {
	t.Parallel()

	for _, storeErr := range []error{ErrTemplateInUse, ErrNotFound} {
		templates := &recordingSourceTemplateRepository{deleteErr: storeErr}
		service := NewService(Service{Authorization: testPlatformAuthorizer(t), SourceTemplates: templates})

		err := service.DeleteTemplate(authenticatedContext(), DeleteTemplateCommand{TenantID: "tenant_123", TemplateID: "source_template_vpc"})
		require.ErrorIs(t, err, storeErr)
	}

	templates := &recordingSourceTemplateRepository{}
	service := NewService(Service{Authorization: testPlatformAuthorizer(t), SourceTemplates: templates})
	err := service.DeleteTemplate(authenticatedContext(), DeleteTemplateCommand{TenantID: "tenant_123"})
	require.ErrorIs(t, err, ErrInvalidCommand)
	assert.Empty(t, templates.deletedID)
}

// An unknown template is a 404, not an empty page: an empty page is what a
// template whose first sync has not finished answers.
func TestListTemplateRevisionsForAnUnknownTemplateIsNotFound(t *testing.T) {
	t.Parallel()

	templates := &recordingSourceTemplateRepository{getErr: ErrNotFound}
	service := NewService(Service{Authorization: testPlatformAuthorizer(t), SourceTemplates: templates})

	_, err := service.ListTemplateRevisionsForTemplate(authenticatedContext(), ListTemplateRevisionsForTemplateCommand{
		TenantID: "tenant_123", TemplateID: "source_template_gone", Page: FirstPage(),
	})
	require.ErrorIs(t, err, ErrNotFound)
	assert.Zero(t, templates.revisionPageCalls)
}

func TestStartTemplateRunPairsRunWithStartIntentInTransaction(t *testing.T) {
	t.Parallel()

	runs := &recordingTemplateRunRepository{}
	work := &recordingUnitOfWork{templateRuns: runs}
	service := NewService(Service{
		Users:         &fakeUserRepository{},
		Work:          work,
		Authorization: testPlatformAuthorizer(t),
		TemplateRuns:  runs,
		StackTemplates: &recordingStackTemplateRepository{stackTemplate: domain.StackTemplate{
			ID: "stack_template_123", TenantID: "tenant_123", SourceTemplateID: "source_123", DesiredTemplateRevisionID: "revision_123", DesiredConfigJSON: json.RawMessage(`{"region":"us-east-1"}`), WorkspaceName: "workspace", Lifecycle: domain.StackTemplateActive,
			PendingPlanRunID: "run_plan_1", PendingPlanTemplateRevisionID: "revision_123", PendingPlanConfigJSON: json.RawMessage(`{"region":"us-east-1"}`),
		}},
		TemplateRevisionMetadata: &recordingTemplateRepository{template: domain.TemplateRevision{ID: "revision_123", TenantID: "tenant_123", SourceTemplateID: "source_123", RepoOwner: "acme", RepoName: "infra", SourceRef: "main", ResolvedCommitSHA: "sha_123", RootPath: "modules/vpc"}},
		RunIDs:                   fixedTemplateRunIDGenerator{runID: "run_123"},
		Clock:                    fixedClock{now: time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)},
	})

	run, err := service.StartTemplateRun(authenticatedContext(), StartTemplateRunCommand{TenantID: "tenant_123", StackTemplateID: "stack_template_123", Operation: domain.OperationPlan})
	if err != nil {
		t.Fatalf("StartTemplateRun returned error: %v", err)
	}
	if work.inTxCalls != 1 || runs.created.ID != run.ID {
		t.Fatalf("transaction calls = %d, run = %#v", work.inTxCalls, runs.created)
	}
	if len(work.requests) != 1 || work.requests[0].Kind != KindStartTemplateRun || work.requests[0].ActorSubject != oidcSubject || work.requests[0].TenantID != "tenant_123" {
		t.Fatalf("requests = %#v", work.requests)
	}
	var payload StartTemplateRunPayload
	if err := json.Unmarshal(work.requests[0].Payload, &payload); err != nil {
		t.Fatalf("decode start intent: %v", err)
	}
	if payload.RunID != run.ID || payload.TenantID != run.TenantID || payload.StackTemplateID != run.StackTemplateID || payload.Operation != run.Operation || payload.SelectedRef != run.SelectedRef || payload.WorkspaceName != run.WorkspaceName || payload.RepoOwner != "acme" || payload.RepoName != "infra" || payload.RootPath != "modules/vpc" || string(payload.ConfigJSON) != `{"region":"us-east-1"}` {
		t.Fatalf("start payload = %#v", payload)
	}
	// The executor checks out this commit. Without it in the payload the run would
	// fall back to the installed ref, which is a moving target and is never
	// updated when the desired revision changes.
	if payload.ResolvedCommitSHA != "sha_123" {
		t.Fatalf("payload.ResolvedCommitSHA = %q, want sha_123", payload.ResolvedCommitSHA)
	}
}

func TestApproveRunPairsApprovalAuditAndApplyIntentInTransaction(t *testing.T) {
	t.Parallel()

	runs := &recordingTemplateRunRepository{run: domain.TemplateRun{ID: "run_123", TenantID: "tenant_123", StackTemplateID: "stack_template_123", Status: domain.TemplateRunWaitingApproval}}
	work := &recordingUnitOfWork{templateRuns: runs}
	workflows := &recordingWorkflowDispatcher{}
	service := NewService(Service{
		Users:                    &fakeUserRepository{},
		Work:                     work,
		Authorization:            testPlatformAuthorizer(t),
		TemplateRuns:             runs,
		StackTemplates:           approvableStackTemplates(),
		TemplateRevisionMetadata: &recordingTemplateRepository{},
		Workflows:                workflows,
		Clock:                    fixedClock{now: time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)},
	})

	if _, err := service.ApproveRun(authenticatedContext(), ApproveRunCommand{TenantID: "tenant_123", RunID: "run_123"}); err != nil {
		t.Fatalf("ApproveRun returned error: %v", err)
	}
	if work.inTxCalls != 1 || runs.approval.RunID != "run_123" || len(work.audits) != 1 {
		t.Fatalf("transaction calls = %d, approval = %#v, audits = %#v", work.inTxCalls, runs.approval, work.audits)
	}
	if len(work.requests) != 1 || work.requests[0].Kind != KindStartTemplateApply || work.requests[0].ActorSubject != oidcSubject || work.requests[0].TenantID != "tenant_123" || workflows.approvalRunID != "" {
		t.Fatalf("requests = %#v, direct approval = %q", work.requests, workflows.approvalRunID)
	}
	var payload StartTemplateApplyPayload
	if err := json.Unmarshal(work.requests[0].Payload, &payload); err != nil {
		t.Fatalf("decode apply intent: %v", err)
	}
	if payload.TenantID != "tenant_123" || payload.RunID != "run_123" {
		t.Fatalf("apply payload = %#v", payload)
	}
}

func adminContext() context.Context {
	return authentication.ContextWithPrincipal(context.Background(), authentication.Principal{Subject: "admin_123"})
}

// The inverse of what this asserted before: the role change was queued because
// a tuple write could not commit with the audit event. It can now, so the write
// happens in the request and the queue is not involved.
func TestAssignStackRoleWritesTheGrantInTheSameTransaction(t *testing.T) {
	t.Parallel()

	authorizer := testPlatformAuthorizer(t)
	work := newRecordingWork(nil)
	users := &fakeUserRepository{users: []UserProfile{{Sub: "user_456", DisplayName: "Casey Jones", Email: "casey@example.com"}}}
	service := NewService(Service{Work: work, Authorization: authorizer, Users: users, Clock: fixedClock{now: time.Now()}})

	result, err := service.AssignStackRole(adminContext(), AssignStackRoleCommand{
		TenantID: domain.TenantID("tenant_123"),
		StackID:  domain.StackID("stack_abc"),
		UserSub:  "user_456",
		Role:     "operator",
	})
	if err != nil {
		t.Fatalf("AssignStackRole returned error: %v", err)
	}
	if view := result.Grant; view.Role != "operator" || view.UserSub != "user_456" {
		t.Fatalf("view = %#v", view)
	}
	// The API answers 201 on this, so a first grant must say so.
	if !result.Created {
		t.Fatal("Created = false for a person who held no role on the stack")
	}
	if len(work.requests) != 0 {
		t.Fatalf("enqueued %d requests, want 0 -- the grant is part of the commit", len(work.requests))
	}

	// The role is in effect when the call returns, not eventually.
	allowed, err := authorizer.Can(context.Background(), "user_456", authorization.RelationCanOperate, mustStackObject(t, "stack_abc"))
	if err != nil {
		t.Fatalf("Can() error = %v", err)
	}
	if !allowed {
		t.Fatal("the assigned role is not in effect when AssignStackRole returns")
	}
}

// Re-assigning a role the subject already holds converges rather than failing.
// OpenFGA rejects a write of an existing tuple, so a naive implementation would
// error on a repeated request; the reconcile skips what is already held.
func TestAssignStackRoleIsIdempotentForAHeldRole(t *testing.T) {
	t.Parallel()

	authorizer := seedGrants(t, testPlatformAuthorizer(t), mustGrant(t, "user_456", "stack_abc", authorization.RelationOperator))
	work := newRecordingWork(nil)
	users := &fakeUserRepository{users: []UserProfile{{Sub: "user_456", DisplayName: "Casey Jones", Email: "casey@example.com"}}}
	service := NewService(Service{Work: work, Authorization: authorizer, Users: users, Clock: fixedClock{now: time.Now()}})

	result, err := service.AssignStackRole(adminContext(), AssignStackRoleCommand{
		TenantID: domain.TenantID("tenant_123"),
		StackID:  domain.StackID("stack_abc"),
		UserSub:  "user_456",
		Role:     "operator",
	})
	if err != nil {
		t.Fatalf("AssignStackRole returned error: %v", err)
	}
	if result.Created {
		t.Fatal("Created = true for a role the person already held")
	}

	grants, err := authorizer.ListGrants(context.Background(), mustStackObject(t, "stack_abc"))
	if err != nil {
		t.Fatalf("ListGrants() error = %v", err)
	}
	if len(grants) != 1 || grants[0].Relation() != authorization.RelationOperator {
		t.Fatalf("grants = %#v, want exactly one operator grant", grants)
	}
}

// Changing a role replaces it rather than adding to it, in one transaction, so
// there is no window in which the subject holds both or neither.
func TestAssignStackRoleReplacesAStaleRole(t *testing.T) {
	t.Parallel()

	authorizer := seedGrants(t, testPlatformAuthorizer(t), mustGrant(t, "user_456", "stack_abc", authorization.RelationOperator))
	users := &fakeUserRepository{users: []UserProfile{{Sub: "user_456", DisplayName: "Casey Jones", Email: "casey@example.com"}}}
	service := NewService(Service{Work: newRecordingWork(nil), Authorization: authorizer, Users: users, Clock: fixedClock{now: time.Now()}})

	result, err := service.AssignStackRole(adminContext(), AssignStackRoleCommand{
		TenantID: domain.TenantID("tenant_123"),
		StackID:  domain.StackID("stack_abc"),
		UserSub:  "user_456",
		Role:     "viewer",
	})
	if err != nil {
		t.Fatalf("AssignStackRole returned error: %v", err)
	}
	// A change of role is not a new grant.
	if result.Created {
		t.Fatal("Created = true for a person who already held another role")
	}

	grants, err := authorizer.ListGrants(context.Background(), mustStackObject(t, "stack_abc"))
	if err != nil {
		t.Fatalf("ListGrants() error = %v", err)
	}
	if len(grants) != 1 || grants[0].Relation() != authorization.RelationViewer {
		t.Fatalf("grants = %#v, want exactly one viewer grant", grants)
	}
}

func TestRevokeStackRoleRemovesTheHeldRole(t *testing.T) {
	t.Parallel()

	authorizer := seedGrants(t, testPlatformAuthorizer(t), mustGrant(t, "user_456", "stack_abc", authorization.RelationOperator))
	service := NewService(Service{Work: newRecordingWork(nil), Authorization: authorizer, Clock: fixedClock{now: time.Now()}})

	if err := service.RevokeStackRole(adminContext(), RevokeStackRoleCommand{
		TenantID: domain.TenantID("tenant_123"),
		StackID:  domain.StackID("stack_abc"),
		UserSub:  "user_456",
	}); err != nil {
		t.Fatalf("RevokeStackRole returned error: %v", err)
	}

	grants, err := authorizer.ListGrants(context.Background(), mustStackObject(t, "stack_abc"))
	if err != nil {
		t.Fatalf("ListGrants() error = %v", err)
	}
	if len(grants) != 0 {
		t.Fatalf("grants = %#v, want none after a revoke", grants)
	}
}

// Revoking access the subject does not hold is not an error: they asked for the
// subject to have none, and the subject already has none.
func TestRevokeStackRoleIsANoOpWhenNothingIsHeld(t *testing.T) {
	t.Parallel()

	authorizer := testPlatformAuthorizer(t)
	service := NewService(Service{Work: newRecordingWork(nil), Authorization: authorizer, Clock: fixedClock{now: time.Now()}})

	if err := service.RevokeStackRole(adminContext(), RevokeStackRoleCommand{
		TenantID: domain.TenantID("tenant_123"),
		StackID:  domain.StackID("stack_abc"),
		UserSub:  "user_456",
	}); err != nil {
		t.Fatalf("RevokeStackRole returned error: %v", err)
	}
}

func mustGrant(t *testing.T, sub, stackID string, relation authorization.Relation) authorization.Grant {
	t.Helper()
	subject, err := authorization.SubjectFromOIDCSub(sub)
	if err != nil {
		t.Fatalf("SubjectFromOIDCSub: %v", err)
	}
	grant, err := authorization.NewGrant(subject, mustStackObject(t, stackID), relation)
	if err != nil {
		t.Fatalf("NewGrant: %v", err)
	}
	return grant
}

func TestAssignStackRoleWithoutUnitOfWorkFails(t *testing.T) {
	t.Parallel()

	service := NewService(Service{Authorization: testPlatformAuthorizer(t), Clock: fixedClock{now: time.Now()}})

	_, err := service.AssignStackRole(adminContext(), AssignStackRoleCommand{
		TenantID: domain.TenantID("tenant_123"),
		StackID:  domain.StackID("stack_abc"),
		UserSub:  "user_456",
		Role:     "operator",
	})
	if err == nil {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

// TestDefaultIDGeneratorsMintPrefixedRandomIDs locks the identifier shape the
// five default generators share. They now differ only in the prefix handed to
// randomID, so a wrong prefix is the one mistake the collapse made possible and
// the one this test is here to catch.
func TestDefaultIDGeneratorsMintPrefixedRandomIDs(t *testing.T) {
	t.Parallel()

	generators := map[string]func() string{
		"run":                   func() string { return string(randomTemplateRunIDGenerator{}.NewTemplateRunID()) },
		"template_registration": func() string { return string(randomTemplateRegistrationIDGenerator{}.NewTemplateRegistrationID()) },
		"stack":                 func() string { return string(randomStackIDGenerator{}.NewStackID()) },
		"stack_template":        func() string { return string(randomStackTemplateIDGenerator{}.NewStackTemplateID()) },
		"credential":            func() string { return string(randomCredentialSetIDGenerator{}.NewCredentialSetID()) },
	}

	for prefix, generate := range generators {
		t.Run(prefix, func(t *testing.T) {
			t.Parallel()

			id := generate()
			suffix, found := strings.CutPrefix(id, prefix+"_")
			if !found {
				t.Fatalf("id = %q, want prefix %q", id, prefix+"_")
			}
			// 16 random bytes, hex-encoded.
			if decoded, err := hex.DecodeString(suffix); err != nil || len(decoded) != 16 {
				t.Fatalf("id = %q, want %q followed by 32 hex characters", id, prefix+"_")
			}
			if second := generate(); second == id {
				t.Fatalf("two %s ids are both %q, want distinct values", prefix, id)
			}
		})
	}
}

// Every mutating command that reaches a stack template through
// operableStackTemplate must record the refusal, not just refuse. Only
// AddTemplateToStack had such a test, so three of the four copies of the audit
// block this replaced were unguarded — dropping the audit call from any of them
// would have left the suite green.
func TestOperableStackTemplateCommandsAuditRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call func(*Service, context.Context) error
	}{
		{"StartTemplateRun", func(service *Service, ctx context.Context) error {
			_, err := service.StartTemplateRun(ctx, StartTemplateRunCommand{
				TenantID:        domain.TenantID("tenant_123"),
				StackTemplateID: domain.StackTemplateID("stack_template_123"),
				Operation:       domain.OperationPlan,
			})
			return err
		}},
		{"UpdateStackTemplateConfig", func(service *Service, ctx context.Context) error {
			_, err := service.UpdateStackTemplateConfig(ctx, UpdateStackTemplateConfigCommand{
				TenantID:        domain.TenantID("tenant_123"),
				StackTemplateID: domain.StackTemplateID("stack_template_123"),
				ConfigJSON:      json.RawMessage(`{"region":"us-east-1"}`),
			})
			return err
		}},
		{"UpgradeStackTemplate", func(service *Service, ctx context.Context) error {
			_, err := service.UpgradeStackTemplate(ctx, UpgradeStackTemplateCommand{
				TenantID:                 domain.TenantID("tenant_123"),
				StackTemplateID:          domain.StackTemplateID("stack_template_123"),
				TargetTemplateRevisionID: domain.TemplateRevisionID("template_456"),
			})
			return err
		}},
	}

	for _, test := range tests {
		for _, refusal := range writeRefusals {
			t.Run(test.name+"/"+refusal.name, func(t *testing.T) {
				t.Parallel()
				testOperableCommandAuditsRefusal(t, test.call, refusal)
			})
		}
	}
}

func testOperableCommandAuditsRefusal(t *testing.T, call func(*Service, context.Context) error, refusal writeRefusal) {
	t.Helper()

	auth := newTestAuthorization(t)
	refusal.grant(t, auth, oidcSubject, "stack_123")
	audit := &recordingAuditRepository{}
	service := NewService(Service{
		TemplateRuns:  &recordingTemplateRunRepository{},
		Authorization: auth,
		StackTemplates: &recordingStackTemplateRepository{
			stackTemplate: domain.StackTemplate{
				ID:        domain.StackTemplateID("stack_template_123"),
				Lifecycle: domain.StackTemplateActive,
			},
		},
		Audit: audit,
		Clock: fixedClock{now: time.Now()},
	})

	err := call(service, authenticatedContext())
	if !errors.Is(err, refusal.want) {
		t.Fatalf("error = %v, want %v", err, refusal.want)
	}

	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(audit.events))
	}
	event := audit.events[0]
	if event.Action != domain.AuditActionFailedAccessAttempt {
		t.Fatalf("action = %q, want %q", event.Action, domain.AuditActionFailedAccessAttempt)
	}
	if event.Outcome != domain.AuditOutcomeFailure {
		t.Fatalf("outcome = %q, want %q", event.Outcome, domain.AuditOutcomeFailure)
	}
	if event.ActorSubject != oidcSubject {
		t.Fatalf("actor_subject = %q, want %q", event.ActorSubject, oidcSubject)
	}
	if event.TenantID != domain.TenantID("tenant_123") {
		t.Fatalf("tenant_id = %q, want %q", event.TenantID, "tenant_123")
	}
}
