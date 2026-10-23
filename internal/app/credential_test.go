package app

import (
	"context"
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
	fixture.service.CredentialEncryptor = prefixingCredentialEncryptor{}

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

type recordingCredentialRepository struct {
	refusingCredentialRepository
	created []domain.CredentialSet
}

func (repository *recordingCredentialRepository) CreateCredential(_ context.Context, credential domain.CredentialSet) error {
	repository.created = append(repository.created, credential)
	return nil
}

type prefixingCredentialEncryptor struct{}

func (prefixingCredentialEncryptor) Encrypt(value string) (string, error) {
	return "sealed:" + value, nil
}
