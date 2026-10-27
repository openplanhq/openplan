package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vishu42/openplan/internal/authorization"
	"github.com/vishu42/openplan/internal/domain"
)

// Go's zero bool is false, the unsafe side, so a create that says nothing
// about sensitivity must store the value as sensitive (#497).
func TestCreateCredentialStoresSensitiveByDefault(t *testing.T) {
	t.Parallel()

	auth := newTestAuthorization(t)
	grantStack(t, auth, oidcSubject, "stack_123", authorization.RelationOwner)
	fixture := newWriteFixture(auth)
	credentials := &recordingCredentialRepository{}
	fixture.service.Credentials = credentials
	fixture.service.CredentialCipher = prefixingCredentialCipher{}

	_, err := fixture.service.CreateCredential(authenticatedContext(), CreateCredentialCommand{
		TenantID: "tenant_123", Scope: domain.CredentialScopeStack, StackID: "stack_123",
		Name: "CLOUD_TOKEN", Value: "secret",
	})
	if err != nil {
		t.Fatalf("CreateCredential returned error: %v", err)
	}
	if len(credentials.created) != 1 || !credentials.created[0].Sensitive {
		t.Fatalf("stored credentials = %+v, want one sensitive credential", credentials.created)
	}
}

func TestCreateCredentialReturnsValueOnlyWhenNotSensitive(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		sensitive *bool
		wantValue string
	}{
		{name: "left out", sensitive: nil, wantValue: ""},
		{name: "sensitive", sensitive: new(true), wantValue: ""},
		{name: "not sensitive", sensitive: new(false), wantValue: "us-east-1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fixture := newCredentialFixture(t)
			created, err := fixture.service.CreateCredential(authenticatedContext(), CreateCredentialCommand{
				TenantID: "tenant_123", Scope: domain.CredentialScopeStack, StackID: "stack_123",
				Name: "AWS_REGION", Value: "us-east-1", Sensitive: test.sensitive,
			})
			if err != nil {
				t.Fatalf("CreateCredential returned error: %v", err)
			}
			wantSensitive := test.wantValue == ""
			if created.Sensitive != wantSensitive || created.Value != test.wantValue {
				t.Fatalf("created = %+v, want sensitive %t and value %q", created, wantSensitive, test.wantValue)
			}
			if stored := fixture.credentials.created[0]; stored.Sensitive != wantSensitive || stored.Ciphertext != "sealed:us-east-1" {
				t.Fatalf("stored = %+v, want sensitive %t and the value encrypted", stored, wantSensitive)
			}
		})
	}
}

// Migration 0010 lets a credential have exactly one owner, and the fakes here
// don't enforce it, so the row the service builds for a component is pinned:
// the template id alone, never the stack id it authorized with (#480).
func TestCreateCredentialForATemplateStoresOnlyTheTemplateOwner(t *testing.T) {
	t.Parallel()

	fixture := newCredentialFixture(t)
	_, err := fixture.service.CreateCredential(authenticatedContext(), CreateCredentialCommand{
		TenantID: "tenant_123", Scope: domain.CredentialScopeStackTemplate, StackTemplateID: "stack_template_123",
		Name: "AWS_REGION", Value: "eu-west-1",
	})
	if err != nil {
		t.Fatalf("CreateCredential returned error: %v", err)
	}
	if len(fixture.credentials.created) != 1 {
		t.Fatalf("stored credentials = %+v, want one", fixture.credentials.created)
	}
	if stored := fixture.credentials.created[0]; stored.StackTemplateID != "stack_template_123" || stored.StackID != "" {
		t.Fatalf("stored owners = stack %q, template %q; want the template alone", stored.StackID, stored.StackTemplateID)
	}
}

func TestListCredentialsOpensOnlyValuesThatAreNotSensitive(t *testing.T) {
	t.Parallel()

	fixture := newCredentialFixture(t)
	fixture.credentials.listed = []domain.CredentialSet{
		{ID: "cred_secret", StackID: "stack_123", Name: "AWS_SECRET_ACCESS_KEY", Ciphertext: "sealed:hunter2", Sensitive: true},
		{ID: "cred_region", StackID: "stack_123", Name: "AWS_REGION", Ciphertext: "sealed:us-east-1", Sensitive: false},
	}

	listed, err := fixture.service.ListCredentials(authenticatedContext(), ListCredentialsCommand{
		TenantID: "tenant_123", Scope: domain.CredentialScopeStack, StackID: "stack_123",
	})
	if err != nil {
		t.Fatalf("ListCredentials returned error: %v", err)
	}
	if len(listed) != 2 || listed[0].Value != "" || !listed[0].Sensitive || listed[1].Value != "us-east-1" || listed[1].Sensitive {
		t.Fatalf("listed = %+v, want the secret without a value and the region with one", listed)
	}
}

func TestListCredentialsFailsWhenAValueWontOpen(t *testing.T) {
	t.Parallel()

	fixture := newCredentialFixture(t)
	fixture.credentials.listed = []domain.CredentialSet{
		{ID: "cred_region", StackID: "stack_123", Name: "AWS_REGION", Ciphertext: "under another key", Sensitive: false},
	}

	listed, err := fixture.service.ListCredentials(authenticatedContext(), ListCredentialsCommand{
		TenantID: "tenant_123", Scope: domain.CredentialScopeStack, StackID: "stack_123",
	})
	if !errors.Is(err, errNotSealed) || listed != nil {
		t.Fatalf("ListCredentials = %+v, %v; want no list and %v", listed, err, errNotSealed)
	}
}

type credentialFixture struct {
	service     *Service
	credentials *recordingCredentialRepository
}

func newCredentialFixture(t *testing.T) credentialFixture {
	t.Helper()
	auth := newTestAuthorization(t)
	grantStack(t, auth, oidcSubject, "stack_123", authorization.RelationOwner)
	fixture := newWriteFixture(auth)
	credentials := &recordingCredentialRepository{}
	fixture.service.Credentials = credentials
	fixture.service.CredentialCipher = prefixingCredentialCipher{}
	return credentialFixture{service: fixture.service, credentials: credentials}
}

type recordingCredentialRepository struct {
	refusingCredentialRepository
	created []domain.CredentialSet
	listed  []domain.CredentialSet
}

func (repository *recordingCredentialRepository) ListCredentials(context.Context, domain.TenantID, domain.CredentialScope, string) ([]domain.CredentialSet, error) {
	return repository.listed, nil
}

func (repository *recordingCredentialRepository) CreateCredential(_ context.Context, credential domain.CredentialSet) error {
	repository.created = append(repository.created, credential)
	return nil
}

type prefixingCredentialCipher struct{}

var errNotSealed = errors.New("not sealed by this cipher")

func (prefixingCredentialCipher) Encrypt(value string) (string, error) {
	return "sealed:" + value, nil
}

func (prefixingCredentialCipher) Decrypt(value string) (string, error) {
	plaintext, ok := strings.CutPrefix(value, "sealed:")
	if !ok {
		return "", errNotSealed
	}
	return plaintext, nil
}
