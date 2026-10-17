package app

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/vishu42/openplan/internal/domain"
)

// assertFieldIssues checks err's field errors, in order, each written as
// "path/to/field issue" (or "?parameter issue").
func assertFieldIssues(t *testing.T, err error, want ...string) {
	t.Helper()
	var got []string
	for _, fieldErr := range FieldErrors(err) {
		where := strings.Join(fieldErr.Path, "/")
		if fieldErr.Parameter != "" {
			where = "?" + fieldErr.Parameter
		}
		got = append(got, where+" "+string(fieldErr.Issue))
	}
	if !slices.Equal(got, want) {
		t.Fatalf("field errors = %q, want %q (error: %v)", got, want, err)
	}
}

// A form can only mark every bad variable if the response names every one.
func TestValidateTemplateConfigReportsEveryVariable(t *testing.T) {
	t.Parallel()

	variables := []domain.TemplateVariable{
		{Name: "b", Required: true},
		{Name: "a", Required: true},
		{Name: "c"},
	}
	_, err := validateTemplateConfig(json.RawMessage(`{"z": 1, "c": null, "y": 2}`), variables)

	if !errors.Is(err, ErrStackTemplateConfigInvalid) {
		t.Fatalf("error = %v, want ErrStackTemplateConfigInvalid", err)
	}
	assertFieldIssues(t, err,
		"config/c null",
		"config/y unknown",
		"config/z unknown",
		"config/b required",
		"config/a required",
	)
}

// A required variable sent as null is missing; reporting it as null too would
// put two errors on one field.
func TestValidateTemplateConfigReportsARequiredNullOnce(t *testing.T) {
	t.Parallel()

	_, err := validateTemplateConfig(json.RawMessage(`{"a": null}`), []domain.TemplateVariable{{Name: "a", Required: true}})
	assertFieldIssues(t, err, "config/a required")
}

func TestValidateTemplateConfigRejectsANonObject(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{`[1]`, `null`, `"text"`} {
		_, err := validateTemplateConfig(json.RawMessage(raw), nil)
		assertFieldIssues(t, err, "config not_object")
	}
}

func TestCreateStackReportsEveryInvalidField(t *testing.T) {
	t.Parallel()

	err := validateCreateStackCommand(CreateStackCommand{
		TenantID:             "tenant_123",
		Name:                 " ",
		Slug:                 "Bad Slug",
		Tags:                 map[string]string{"bad key": "x"},
		DefaultCredentialIDs: []domain.CredentialSetID{""},
	})
	if !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("error = %v, want ErrInvalidCommand", err)
	}
	assertFieldIssues(t, err, "name required", "slug invalid_format", "tags invalid_format", "default_credential_ids invalid")
}

// With no slug given, a blank name would also fail to derive one; that is the
// same mistake, so it is reported once, on the name.
func TestCreateStackBlankNameIsNotAlsoABadSlug(t *testing.T) {
	t.Parallel()

	err := validateCreateStackCommand(CreateStackCommand{TenantID: "tenant_123"})
	assertFieldIssues(t, err, "name required")
}

func TestCreateStackPathIDIsNotAFieldError(t *testing.T) {
	t.Parallel()

	err := validateCreateStackCommand(CreateStackCommand{Name: ""})
	if !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("error = %v, want ErrInvalidCommand", err)
	}
	assertFieldIssues(t, err)
}

func TestCreateCredentialReportsNameAndValue(t *testing.T) {
	t.Parallel()

	err := validateCreateCredentialCommand(CreateCredentialCommand{
		TenantID: "tenant_123", Scope: domain.CredentialScopeStack, StackID: "stack_123",
		Name: "1BAD", Value: "",
	})
	assertFieldIssues(t, err, "name invalid_format", "value required")
	if !errors.Is(FieldErrors(err)[0], ErrCredentialNameInvalid) {
		t.Fatalf("name error = %v, want ErrCredentialNameInvalid", FieldErrors(err)[0])
	}
}

func TestRegisterTemplateReportsEveryInvalidField(t *testing.T) {
	t.Parallel()

	err := validateRegisterTemplateCommand(RegisterTemplateCommand{
		TenantID: "tenant_123", RepoOwner: "", RepoName: "bad name", SourceRef: " ", RootPath: "../escape",
	})
	assertFieldIssues(t, err, "repo_owner required", "repo_name invalid_format", "source_ref required", "root_path invalid_format")
}

func TestStartRunFieldErrors(t *testing.T) {
	t.Parallel()

	err := validateStartTemplateRunCommand(StartTemplateRunCommand{
		TenantID: "tenant_123", StackTemplateID: "stack_template_123", Operation: domain.OperationPlan, AutoApprove: true,
	})
	assertFieldIssues(t, err, "auto_approve apply_only")

	err = validateStartTemplateRunCommand(StartTemplateRunCommand{
		TenantID: "tenant_123", StackTemplateID: "stack_template_123", Operation: "refresh", AutoApprove: true,
	})
	assertFieldIssues(t, err, "operation unsupported")
}

func TestRequiredBodyIDsAreFieldErrors(t *testing.T) {
	t.Parallel()

	assertFieldIssues(t, validateAddTemplateToStackCommand(AddTemplateToStackCommand{TenantID: "t", StackID: "s"}),
		"template_revision_id required")
	assertFieldIssues(t, validateUpgradeStackTemplateCommand(UpgradeStackTemplateCommand{TenantID: "t", StackTemplateID: "s"}),
		"target_template_revision_id required")
	assertFieldIssues(t, validateUpdateStackTemplateConfigCommand(UpdateStackTemplateConfigCommand{TenantID: "t", StackTemplateID: "s"}),
		"config required")
}

// Role and sub are both checked before the user lookup, so a request that
// gets both wrong hears about both.
func TestAssignStackRoleReportsRoleAndSubTogether(t *testing.T) {
	t.Parallel()

	service := NewService(Service{
		Work:          newRecordingWork(nil),
		Authorization: testPlatformAuthorizer(t),
		Users:         &fakeUserRepository{},
	})
	_, err := service.AssignStackRole(adminContext(), AssignStackRoleCommand{
		TenantID: "tenant_123", StackID: "stack_abc", UserSub: "", Role: "emperor",
	})
	if !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("error = %v, want ErrInvalidCommand", err)
	}
	assertFieldIssues(t, err, "role unsupported", "user_sub invalid_format")
}

// The search rules live here alone; the handler only parses integers. Every
// bad parameter is named, as a parameter rather than a body pointer.
func TestSearchUsersReportsEveryParameter(t *testing.T) {
	t.Parallel()

	err := validateSearchUsersCommand(SearchUsersCommand{TenantID: "t", Query: strings.Repeat("a", 201), Page: PageRequest{Limit: 0}})
	assertFieldIssues(t, err, "?q too_long", "?limit out_of_range")
	assertFieldIssues(t, validateSearchUsersCommand(SearchUsersCommand{TenantID: "t", Query: "  ", Page: PageRequest{Limit: 101}}), "?q required", "?limit out_of_range")
}

// A bad query is the caller's to fix whoever they are, so it is reported
// before the platform check, as the handler did when it held these rules.
func TestSearchUsersValidatesBeforeAuthorizing(t *testing.T) {
	t.Parallel()

	service := NewService(Service{Users: &fakeUserRepository{users: []UserProfile{}}, Authorization: testPlatformAuthorizer(t)})
	_, err := service.SearchUsers(contextWithOrdinaryUser(), SearchUsersCommand{TenantID: "tenant_1", Query: "", Page: FirstPage()})
	assertFieldIssues(t, err, "?q required")
}
