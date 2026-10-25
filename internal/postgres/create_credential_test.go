package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vishu42/openplan/internal/app"
	"github.com/vishu42/openplan/internal/authentication"
	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/encryption"
	"github.com/vishu42/openplan/internal/queue"
)

// A scoped credential has exactly one owner, and migration 0010 refuses a row
// that names both. The app consults the owning stack to authorize a component
// credential, and once stored that stack id with it, so every component create
// answered 500 (#480). This drives CreateCredential through the app over the
// real schema for both scopes and reads the owner columns back.
func TestCreateCredentialStoresOnlyItsScopeOwner(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	specs, err := queue.NewSpecRegistry(app.QueueSpecs()...)
	require.NoError(t, err)
	cipher, err := encryption.NewCipher("01234567890123456789012345678901")
	require.NoError(t, err)
	store := NewStore(pool, WithQueueSpecs(specs), WithCredentialCipher(cipher))
	service := app.NewService(app.Service{
		Authorization:    platformAdminAuthorization(t, "user-1"),
		Work:             store,
		Stacks:           store,
		StackTemplates:   store,
		Credentials:      store,
		CredentialCipher: store,
	})
	actx := authentication.ContextWithPrincipal(ctx, authentication.Principal{Subject: "user-1"})

	stack, err := service.CreateStack(actx, app.CreateStackCommand{TenantID: "tenant_123", Name: "Credentials"})
	require.NoError(t, err)
	stackTemplate := domain.StackTemplate{
		ID: "stack_template_vpc", TenantID: "tenant_123", StackID: stack.ID,
		SourceTemplateID: "source_template_vpc", DesiredTemplateRevisionID: "template_123",
		WorkspaceName: "credentials_vpc", InstalledConfigJSON: json.RawMessage(`{}`), CreatedBy: "user-1",
		Lifecycle: domain.StackTemplateActive,
	}
	seedSourceTemplate(t, ctx, store, "tenant_123", stackTemplate.SourceTemplateID)
	require.NoError(t, store.CreateStackTemplate(ctx, stackTemplate))

	for _, test := range []struct {
		name          string
		command       app.CreateCredentialCommand
		wantSensitive bool
	}{
		{
			name:          "stack",
			command:       app.CreateCredentialCommand{Scope: domain.CredentialScopeStack, StackID: stack.ID, Name: "CLOUD_TOKEN"},
			wantSensitive: true,
		},
		{
			name:          "stack template",
			command:       app.CreateCredentialCommand{Scope: domain.CredentialScopeStackTemplate, StackTemplateID: stackTemplate.ID, Name: "CLOUD_PROJECT"},
			wantSensitive: true,
		},
		{
			name:          "stack template sensitive",
			command:       app.CreateCredentialCommand{Scope: domain.CredentialScopeStackTemplate, StackTemplateID: stackTemplate.ID, Name: "CLOUD_SECRET", Sensitive: new(true)},
			wantSensitive: true,
		},
		{
			name:          "stack template not sensitive",
			command:       app.CreateCredentialCommand{Scope: domain.CredentialScopeStackTemplate, StackTemplateID: stackTemplate.ID, Name: "CLOUD_REGION", Sensitive: new(false)},
			wantSensitive: false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := test.command
			command.TenantID = "tenant_123"
			command.Value = "eu-west-1"

			created, err := service.CreateCredential(actx, command)
			require.NoError(t, err)

			var stackID, templateID sql.NullString
			var sensitive bool
			require.NoError(t, pool.QueryRow(ctx, `
				select stack_id, stack_template_id, sensitive from scoped_credentials where id = $1
			`, created.ID).Scan(&stackID, &templateID, &sensitive))
			if command.Scope == domain.CredentialScopeStack {
				assert.Equal(t, sql.NullString{String: string(stack.ID), Valid: true}, stackID)
				assert.False(t, templateID.Valid, "a stack credential names no template")
			} else {
				assert.False(t, stackID.Valid, "a component credential names no stack; the stack is consulted for authorization only")
				assert.Equal(t, sql.NullString{String: string(stackTemplate.ID), Valid: true}, templateID)
			}
			assert.Equal(t, test.wantSensitive, sensitive)
		})
	}
}
