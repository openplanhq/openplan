package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openfga/openfga/pkg/storage/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vishu42/openplan/internal/app"
	"github.com/vishu42/openplan/internal/authentication"
	"github.com/vishu42/openplan/internal/authorization"
	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/queue"
)

// The sync never creates a template: it writes its revision into the one its
// input names. This drives the app's register and Sync over the real schema
// and proves each queued sync names a stored template its revision can land
// in, which is what the templates routes and the web's Sync button depend
// on.
func TestRegisteredTemplateTakesItsSyncsRevision(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	specs, err := queue.NewSpecRegistry(app.QueueSpecs()...)
	require.NoError(t, err)
	store := NewStore(pool, WithQueueSpecs(specs))
	service := app.NewService(app.Service{
		Authorization:   platformAdminAuthorization(t, "user-1"),
		Work:            store,
		SourceTemplates: store,
	})
	actx := authentication.ContextWithPrincipal(ctx, authentication.Principal{Subject: "user-1"})
	register := app.RegisterTemplateCommand{TenantID: "tenant_123", RepoOwner: "acme", RepoName: "infra", SourceRef: "main", RootPath: "modules/vpc"}

	registered, err := service.RegisterTemplate(actx, register)
	require.NoError(t, err)
	template, err := store.GetSourceTemplate(ctx, "tenant_123", registered.Template.ID)
	require.NoError(t, err, "the template is stored before any sync runs")
	assert.Equal(t, "vpc", template.Name)
	assert.Empty(t, template.LatestTemplateRevisionID)

	input := queuedSync(t, ctx, pool, registered.Sync.ID)
	assert.Equal(t, registered.Template.ID, input.SourceTemplateID)
	stored, err := store.GetTemplateRegistration(ctx, "tenant_123", registered.Sync.ID)
	require.NoError(t, err)
	assert.Equal(t, registered.Template.ID, stored.SourceTemplateID)

	// What the sync activity stores on success, into the template it was handed.
	_, err = store.UpsertTemplateRevisionWithVariables(ctx, domain.TemplateRevision{
		ID: "revision_1", TenantID: input.TenantID, SourceTemplateID: input.SourceTemplateID,
		RepoOwner: input.RepoOwner, RepoName: input.RepoName, SourceRef: input.SourceRef, RootPath: input.RootPath,
		ResolvedCommitSHA: "abc123", Name: "vpc", Status: domain.TemplateRevisionActive, CreatedAt: time.Now().UTC(),
	}, nil)
	require.NoError(t, err)
	template, err = store.GetSourceTemplate(ctx, "tenant_123", registered.Template.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TemplateRevisionID("revision_1"), template.LatestTemplateRevisionID)

	again, err := service.RegisterTemplate(actx, register)
	require.NoError(t, err)
	assert.Equal(t, registered.Template.ID, again.Template.ID, "a repeat register lands on the same template")
	assert.Equal(t, registered.Template.ID, queuedSync(t, ctx, pool, again.Sync.ID).SourceTemplateID)

	synced, err := service.StartTemplateSync(actx, app.StartTemplateSyncCommand{TenantID: "tenant_123", TemplateID: registered.Template.ID})
	require.NoError(t, err)
	assert.Equal(t, registered.Template.ID, queuedSync(t, ctx, pool, synced.Sync.ID).SourceTemplateID)

	var templates int
	require.NoError(t, pool.QueryRow(ctx, `select count(*) from source_templates`).Scan(&templates))
	assert.Equal(t, 1, templates)
}

// queuedSync reads the sync input queued for one registration.
func queuedSync(t *testing.T, ctx context.Context, pool *pgxpool.Pool, registrationID domain.TemplateRegistrationID) app.StartTemplateSyncPayload {
	t.Helper()
	var payload []byte
	require.NoError(t, pool.QueryRow(ctx, `
		select payload from work_queue
		where kind = 'start_template_sync' and payload->>'RegistrationID' = $1
	`, string(registrationID)).Scan(&payload))
	var input app.StartTemplateSyncPayload
	require.NoError(t, json.Unmarshal(payload, &input))
	return input
}

// platformAdminAuthorization is an in-memory authorizer where subject is a
// platform administrator, so it may publish and read templates.
func platformAdminAuthorization(t *testing.T, subject string) *authorization.Authorization {
	t.Helper()
	auth, err := authorization.NewWithDatastore(context.Background(), memory.New(), "openplan-test")
	require.NoError(t, err)
	t.Cleanup(auth.Close)
	principal, err := authorization.SubjectFromOIDCSub(subject)
	require.NoError(t, err)
	grant, err := authorization.NewGrant(principal, authorization.Platform, authorization.RelationAdmin)
	require.NoError(t, err)
	require.NoError(t, auth.Grant(context.Background(), grant))
	return auth
}
