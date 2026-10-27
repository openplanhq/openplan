package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/vishu42/openplan/internal/app"
	"github.com/vishu42/openplan/internal/domain"
)

func createTemplateRegistration(ctx context.Context, exec pgxExecutor, registration domain.TemplateRegistration) error {
	_, err := exec.Exec(ctx, `
		insert into template_registrations (
			id,
			tenant_id,
			repo_owner,
			repo_name,
			source_ref,
			root_path,
			status,
			template_revision_id,
			resolved_commit_sha,
			requested_by,
			requested_at,
			completed_at,
			error_summary,
			source_template_id
		) values (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, $13, nullif($14, '')
		)
	`,
		registration.ID,
		registration.TenantID,
		registration.RepoOwner,
		registration.RepoName,
		registration.SourceRef,
		registration.RootPath,
		registration.Status,
		registration.TemplateRevisionID,
		registration.ResolvedCommitSHA,
		registration.RequestedBy,
		registration.RequestedAt,
		nullTime(registration.CompletedAt),
		registration.ErrorSummary,
		registration.SourceTemplateID,
	)
	// The template was deleted between the caller reading it and this
	// insert: a Sync of a template that no longer exists.
	if foreignKeyViolation(err, "template_registrations_source_template_id_fkey") {
		return app.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("create template registration: %w", err)
	}
	return nil
}

// CreateCredential stores one already-encrypted credential under exactly one scope owner.
func (store *Store) CreateCredential(ctx context.Context, credential domain.Credential) error {
	_, err := store.pool.Exec(ctx, `
		insert into scoped_credentials (id, tenant_id, stack_id, stack_template_id, name, ciphertext, sensitive, created_at)
		values ($1, $2, nullif($3, ''), nullif($4, ''), $5, $6, $7, $8)
	`, credential.ID, credential.TenantID, credential.StackID, credential.StackTemplateID, credential.Name, credential.Ciphertext, credential.Sensitive, credential.CreatedAt)
	if duplicateConstraint(err, "scoped_credentials_stack_name_idx") || duplicateConstraint(err, "scoped_credentials_template_name_idx") {
		return app.ErrDuplicateCredentialName
	}
	if err != nil {
		return fmt.Errorf("create credential: %w", err)
	}
	return nil
}

// ListCredentials returns encrypted credential records owned directly by one scope owner.
func (store *Store) ListCredentials(ctx context.Context, tenantID domain.TenantID, scope domain.CredentialScope, ownerID string) ([]domain.Credential, error) {
	column := "stack_id"
	if scope == domain.CredentialScopeStackTemplate {
		column = "stack_template_id"
	}
	if scope != domain.CredentialScopeStack && scope != domain.CredentialScopeStackTemplate {
		return nil, app.ErrInvalidCommand
	}
	rows, err := store.pool.Query(ctx, fmt.Sprintf(`
		select id, tenant_id, stack_id, stack_template_id, name, ciphertext, sensitive, created_at
		from scoped_credentials
		where tenant_id = $1 and %s = $2
		order by name
	`, column), tenantID, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list credentials: %w", err)
	}
	defer rows.Close()
	return scanCredentials(rows)
}

// DeleteCredential removes one tenant-owned credential record by ID.
func (store *Store) DeleteCredential(ctx context.Context, tenantID domain.TenantID, id domain.CredentialID) error {
	result, err := store.pool.Exec(ctx, `delete from scoped_credentials where tenant_id = $1 and id = $2`, tenantID, id)
	if err != nil {
		return fmt.Errorf("delete credential: %w", err)
	}
	if result.RowsAffected() == 0 {
		return app.ErrNotFound
	}
	return nil
}

// ListCredentialsForStackTemplate returns stack credentials plus template credentials for runtime inheritance.
func (store *Store) ListCredentialsForStackTemplate(ctx context.Context, tenantID domain.TenantID, stackTemplateID domain.StackTemplateID) ([]domain.Credential, error) {
	rows, err := store.pool.Query(ctx, `
		select c.id, c.tenant_id, c.stack_id, c.stack_template_id, c.name, c.ciphertext, c.sensitive, c.created_at
		from scoped_credentials c
		join stack_templates st on st.id = $2
		where c.tenant_id = $1 and (c.stack_id = st.stack_id or c.stack_template_id = st.id)
		order by c.stack_id is not null desc, c.name
	`, tenantID, stackTemplateID)
	if err != nil {
		return nil, fmt.Errorf("list runtime credentials: %w", err)
	}
	defer rows.Close()
	return scanCredentials(rows)
}

type credentialRows interface {
	Next() bool
	Scan(...any) error
	Err() error
}

// scanCredentials converts nullable scope-owner columns into credential domain records.
func scanCredentials(rows credentialRows) ([]domain.Credential, error) {
	credentials := make([]domain.Credential, 0)
	for rows.Next() {
		var credential domain.Credential
		var stackID sql.NullString
		var stackTemplateID sql.NullString
		if err := rows.Scan(&credential.ID, &credential.TenantID, &stackID, &stackTemplateID, &credential.Name, &credential.Ciphertext, &credential.Sensitive, &credential.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan credential: %w", err)
		}
		if stackID.Valid {
			credential.StackID = domain.StackID(stackID.String)
		}
		if stackTemplateID.Valid {
			credential.StackTemplateID = domain.StackTemplateID(stackTemplateID.String)
		}
		credentials = append(credentials, credential)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate credentials: %w", err)
	}
	return credentials, nil
}

// RecordTemplateRegistrationStep records the step a running sync has started.
// Only a running registration has a step to start, so any other registration
// is not found.
func (store *Store) RecordTemplateRegistrationStep(ctx context.Context, input domain.TemplateRegistrationStepActivityInput) error {
	if !input.Step.Valid() {
		return fmt.Errorf("record template registration step: unknown step %q", input.Step)
	}
	commandTag, err := store.pool.Exec(ctx, `
		update template_registrations
		set step = $1
		where tenant_id = $2
			and id = $3
			and status = $4
	`, input.Step, input.TenantID, input.RegistrationID, domain.TemplateRegistrationRunning)
	if err != nil {
		return fmt.Errorf("record template registration step: %w", err)
	}
	if commandTag.RowsAffected() == 1 {
		return nil
	}
	gone, err := templateRegistrationGone(ctx, store.pool, input.TenantID, input.RegistrationID)
	if err != nil {
		return fmt.Errorf("record template registration step: %w", err)
	}
	if gone {
		return nil
	}
	return ErrNotFound
}

// templateRegistrationGone reports whether the registration no longer exists.
// Deleting a template cascades to its registrations while their syncs may
// still be running; those syncs record into nothing and end, rather than
// failing and being retried for their whole retry policy.
func templateRegistrationGone(ctx context.Context, exec pgxExecutor, tenantID domain.TenantID, id domain.TemplateRegistrationID) (bool, error) {
	var exists bool
	if err := exec.QueryRow(ctx, `
		select exists (
			select 1
			from template_registrations
			where tenant_id = $1
				and id = $2
		)
	`, tenantID, id).Scan(&exists); err != nil {
		return false, err
	}
	return !exists, nil
}

// RecordTemplateRegistrationStatus records a status transition. A
// registration that is gone, deleted with its template, has nothing left to
// record and succeeds; see templateRegistrationGone.
func (store *Store) RecordTemplateRegistrationStatus(ctx context.Context, input domain.TemplateRegistrationStatusActivityInput) error {
	var err error
	if terminalTemplateRegistrationStatus(input.Status) {
		_, err = store.pool.Exec(ctx, `
			update template_registrations
			set
				status = $1,
				template_revision_id = $2,
				resolved_commit_sha = $3,
				error_summary = $4,
				completed_at = coalesce(completed_at, now())
			where tenant_id = $5
				and id = $6
		`,
			input.Status,
			input.TemplateRevisionID,
			input.ResolvedCommitSHA,
			input.ErrorSummary,
			input.TenantID,
			input.RegistrationID,
		)
	} else {
		_, err = store.pool.Exec(ctx, `
			update template_registrations
			set
				status = $1,
				template_revision_id = $2,
				resolved_commit_sha = $3,
				error_summary = $4
			where tenant_id = $5
				and id = $6
		`,
			input.Status,
			input.TemplateRevisionID,
			input.ResolvedCommitSHA,
			input.ErrorSummary,
			input.TenantID,
			input.RegistrationID,
		)
	}
	if err != nil {
		return fmt.Errorf("record template registration status: %w", err)
	}
	return nil
}

// UpsertTemplateRevisionWithVariables records a template revision into its existing source
// template, deduplicating against an existing revision with the same identity. It never
// creates the template: a sync that outlives its template's delete would bring it back.
// Variables are only written when this call actually inserts a new revision, since a reused
// revision already has its variables recorded.
func (store *Store) UpsertTemplateRevisionWithVariables(ctx context.Context, templateRevision domain.TemplateRevision, variables []domain.TemplateVariable) (domain.TemplateRevision, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return domain.TemplateRevision{}, fmt.Errorf("begin upsert template revision: %w", err)
	}
	defer tx.Rollback(ctx)

	tagsJSON, err := encodeTemplateRevisionTags(templateRevision.Tags)
	if err != nil {
		return domain.TemplateRevision{}, fmt.Errorf("marshal template revision tags: %w", err)
	}

	if templateRevision.SourceTemplateID == "" {
		return domain.TemplateRevision{}, errors.New("upsert template revision: source template ID is required")
	}

	inserted, insertedNew, err := insertTemplateRevision(ctx, tx, templateRevision, tagsJSON)
	if err != nil {
		return domain.TemplateRevision{}, err
	}
	if err := recordLatestTemplateRevision(ctx, tx, inserted); err != nil {
		return domain.TemplateRevision{}, err
	}
	if !insertedNew {
		// Revision already existed under this identity, so its variables were already
		// recorded on the original insert — skip re-inserting them.
		if err := tx.Commit(ctx); err != nil {
			return domain.TemplateRevision{}, fmt.Errorf("commit reused template revision: %w", err)
		}
		return inserted, nil
	}

	for _, variable := range variables {
		if _, err := tx.Exec(ctx, `
			insert into template_variables (
				template_revision_id,
				name,
				type_expression,
				description,
				required,
				has_default,
				sensitive,
				has_validation
			) values ($1, $2, $3, $4, $5, $6, $7, $8)
		`,
			inserted.ID,
			variable.Name,
			variable.TypeExpression,
			variable.Description,
			variable.Required,
			variable.HasDefault,
			variable.Sensitive,
			variable.HasValidation,
		); err != nil {
			return domain.TemplateRevision{}, fmt.Errorf("insert template revision variable %q: %w", variable.Name, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.TemplateRevision{}, fmt.Errorf("commit upsert template revision: %w", err)
	}
	return inserted, nil
}

func (store *Store) GetTemplateRevisionVariables(ctx context.Context, tenantID domain.TenantID, templateRevisionID domain.TemplateRevisionID) ([]domain.TemplateVariable, error) {
	var exists bool
	if err := store.pool.QueryRow(ctx, `
		select exists (
			select 1
			from template_revisions
			where tenant_id = $1
				and id = $2
		)
	`, tenantID, templateRevisionID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check template revision existence: %w", err)
	}
	if !exists {
		return nil, app.ErrNotFound
	}

	rows, err := store.pool.Query(ctx, `
		select
			template_revision_id,
			name,
			type_expression,
			description,
			required,
			has_default,
			sensitive,
			has_validation
		from template_variables
		where template_revision_id = $1
		order by name
	`, templateRevisionID)
	if err != nil {
		return nil, fmt.Errorf("get template revision variables: %w", err)
	}
	defer rows.Close()

	// Empty rather than nil, which the API would serve as null.
	variables := []domain.TemplateVariable{}
	for rows.Next() {
		var variable domain.TemplateVariable
		if err := rows.Scan(
			&variable.TemplateRevisionID,
			&variable.Name,
			&variable.TypeExpression,
			&variable.Description,
			&variable.Required,
			&variable.HasDefault,
			&variable.Sensitive,
			&variable.HasValidation,
		); err != nil {
			return nil, fmt.Errorf("scan template revision variable: %w", err)
		}
		variables = append(variables, variable)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate template revision variables: %w", err)
	}
	return variables, nil
}

// ListTemplateRevisions returns every revision of the tenant, newest first.
func (store *Store) ListTemplateRevisions(ctx context.Context, tenantID domain.TenantID) ([]domain.TemplateRevision, error) {
	rows, err := store.pool.Query(ctx, `
		select
			id,
			tenant_id,
			source_template_id,
			repo_owner,
			repo_name,
			source_ref,
			resolved_commit_sha,
			root_path,
			name,
			description,
			tags_json,
			status,
			created_at
		from template_revisions
		where tenant_id = $1
		order by created_at desc, id desc
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list template revisions: %w", err)
	}
	defer rows.Close()

	var templateRevisions []domain.TemplateRevision
	for rows.Next() {
		templateRevision, err := scanTemplateRevision(rows)
		if err != nil {
			return nil, err
		}
		templateRevisions = append(templateRevisions, templateRevision)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate template revisions: %w", err)
	}
	return templateRevisions, nil
}

// CreateSourceTemplate stores the template unless its source already has
// one, and returns the stored template.
func (store *Store) CreateSourceTemplate(ctx context.Context, template domain.SourceTemplate) (domain.SourceTemplate, error) {
	return createSourceTemplate(ctx, store.pool, template)
}

// createSourceTemplate inserts, or finds, the template by its source rather
// than its id: a row from before ids were sha256 has another id for the same
// source, and the source is what makes two registrations one template. A
// concurrent register of the same source waits on this insert's unique index
// entry, then finds the row once it commits.
func createSourceTemplate(ctx context.Context, exec pgxExecutor, template domain.SourceTemplate) (domain.SourceTemplate, error) {
	if template.ID == "" {
		return domain.SourceTemplate{}, errors.New("create source template: id is required")
	}
	if _, err := exec.Exec(ctx, `
		insert into source_templates (id, tenant_id, repo_owner, repo_name, source_ref, root_path, created_at, updated_at)
		values ($1, $2, $3, $4, $5, $6, coalesce($7::timestamptz, now()), coalesce($7::timestamptz, now()))
		on conflict do nothing
	`,
		template.ID,
		template.TenantID,
		template.RepoOwner,
		template.RepoName,
		template.SourceRef,
		template.RootPath,
		nullTime(template.CreatedAt),
	); err != nil {
		return domain.SourceTemplate{}, fmt.Errorf("create source template: %w", err)
	}

	stored, err := scanSourceTemplate(exec.QueryRow(ctx, selectSourceTemplatesSQL+`
		where st.tenant_id = $1
			and st.repo_owner = $2
			and st.repo_name = $3
			and st.root_path = $4
			and st.source_ref = $5
	`, template.TenantID, template.RepoOwner, template.RepoName, template.RootPath, template.SourceRef))
	if err != nil {
		return domain.SourceTemplate{}, fmt.Errorf("read created source template: %w", err)
	}
	return stored, nil
}

func (store *Store) GetSourceTemplate(ctx context.Context, tenantID domain.TenantID, id domain.SourceTemplateID) (domain.SourceTemplate, error) {
	template, err := scanSourceTemplate(store.pool.QueryRow(ctx, selectSourceTemplatesSQL+`
		where st.tenant_id = $1
			and st.id = $2
	`, tenantID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SourceTemplate{}, app.ErrNotFound
	}
	if err != nil {
		return domain.SourceTemplate{}, fmt.Errorf("get source template: %w", err)
	}
	return template, nil
}

// DeleteSourceTemplate deletes the tenant's template; its revisions, their
// variables and its syncs cascade with it. The stack templates' key refuses
// the delete while any stack template names the template, whatever its
// lifecycle, and because the key's check locks and sees concurrent commits,
// an install racing this delete either lands first and refuses it, or finds
// the template gone.
func (store *Store) DeleteSourceTemplate(ctx context.Context, tenantID domain.TenantID, id domain.SourceTemplateID) error {
	result, err := store.pool.Exec(ctx, `delete from source_templates where tenant_id = $1 and id = $2`, tenantID, id)
	if foreignKeyViolation(err, "stack_templates_source_template_id_fkey") {
		return app.ErrTemplateInUse
	}
	if err != nil {
		return fmt.Errorf("delete source template: %w", err)
	}
	if result.RowsAffected() == 0 {
		return app.ErrNotFound
	}
	return nil
}

// ListSourceTemplatesPage returns up to limit templates after the cursor,
// newest first.
func (store *Store) ListSourceTemplatesPage(ctx context.Context, tenantID domain.TenantID, after *app.SourceTemplateCursor, limit int) ([]domain.SourceTemplate, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("source template page limit must be positive")
	}
	var afterCreatedAt any
	var afterID domain.SourceTemplateID
	if after != nil {
		afterCreatedAt = after.CreatedAt
		afterID = after.ID
	}
	rows, err := store.pool.Query(ctx, selectSourceTemplatesSQL+`
		where st.tenant_id = $1
			and ($2::timestamptz is null or (st.created_at, st.id) < ($2, $3))
		order by st.created_at desc, st.id desc
		limit $4
	`, tenantID, afterCreatedAt, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("list source templates: %w", err)
	}
	defer rows.Close()

	var templates []domain.SourceTemplate
	for rows.Next() {
		template, err := scanSourceTemplate(rows)
		if err != nil {
			return nil, fmt.Errorf("scan source template: %w", err)
		}
		templates = append(templates, template)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate source templates: %w", err)
	}
	return templates, nil
}

// selectSourceTemplatesSQL reads templates with their latest revision's name,
// description and tags, and their newest sync. The left joins keep a template
// no sync has produced a revision for, or been requested for, yet. The
// newest sync is one probe of the registrations index per template.
const selectSourceTemplatesSQL = `
	select
		st.id,
		st.tenant_id,
		st.repo_owner,
		st.repo_name,
		st.source_ref,
		st.root_path,
		tr.name,
		coalesce(tr.description, ''),
		coalesce(tr.tags_json, '[]'::jsonb),
		st.latest_template_revision_id,
		st.created_at,
		st.updated_at,
		ls.status,
		ls.error_summary,
		ls.resolved_commit_sha,
		ls.requested_at,
		ls.completed_at,
		ls.revision_predates_request
	from source_templates st
	left join template_revisions tr
		on tr.tenant_id = st.tenant_id
		and tr.id = st.latest_template_revision_id
	left join lateral (
		select
			r.status,
			r.error_summary,
			r.resolved_commit_sha,
			r.requested_at,
			r.completed_at,
			coalesce(rr.created_at < r.requested_at, false) as revision_predates_request
		from template_registrations r
		left join template_revisions rr
			on rr.tenant_id = r.tenant_id
			and rr.id = r.template_revision_id
		where r.source_template_id = st.id
			and r.tenant_id = st.tenant_id
		order by r.requested_at desc, r.id desc
		limit 1
	) ls on true
`

func scanSourceTemplate(scanner templateScanner) (domain.SourceTemplate, error) {
	var template domain.SourceTemplate
	var name sql.NullString
	var tagsJSON []byte
	var sync latestTemplateSyncColumns
	if err := scanner.Scan(
		&template.ID,
		&template.TenantID,
		&template.RepoOwner,
		&template.RepoName,
		&template.SourceRef,
		&template.RootPath,
		&name,
		&template.Description,
		&tagsJSON,
		&template.LatestTemplateRevisionID,
		&template.CreatedAt,
		&template.UpdatedAt,
		&sync.status,
		&sync.errorSummary,
		&sync.resolvedCommitSHA,
		&sync.requestedAt,
		&sync.completedAt,
		&sync.revisionPredatesRequest,
	); err != nil {
		return domain.SourceTemplate{}, err
	}
	template.LatestSync = sync.latestSync()
	template.Name = name.String
	if !name.Valid {
		template.Name = domain.FallbackTemplateName(template.RepoName, template.RootPath)
	}
	tags, err := decodeTemplateRevisionTags(tagsJSON)
	if err != nil {
		return domain.SourceTemplate{}, fmt.Errorf("unmarshal source template tags: %w", err)
	}
	template.Tags = tags
	return template, nil
}

// latestTemplateSyncColumns holds a template's newest-sync columns, every one
// null when no sync has been requested for it.
type latestTemplateSyncColumns struct {
	status                  sql.NullString
	errorSummary            sql.NullString
	resolvedCommitSHA       sql.NullString
	requestedAt             sql.NullTime
	completedAt             sql.NullTime
	revisionPredatesRequest sql.NullBool
}

func (columns latestTemplateSyncColumns) latestSync() *domain.LatestTemplateSync {
	if !columns.status.Valid {
		return nil
	}
	return &domain.LatestTemplateSync{
		Status:                  domain.TemplateRegistrationStatus(columns.status.String),
		ErrorSummary:            columns.errorSummary.String,
		ResolvedCommitSHA:       columns.resolvedCommitSHA.String,
		RequestedAt:             columns.requestedAt.Time,
		CompletedAt:             columns.completedAt.Time,
		RevisionPredatesRequest: columns.revisionPredatesRequest.Bool,
	}
}

// ListTemplateRevisionsBySourcePage returns up to limit of one template's
// revisions after the cursor, newest first.
func (store *Store) ListTemplateRevisionsBySourcePage(ctx context.Context, tenantID domain.TenantID, sourceTemplateID domain.SourceTemplateID, after *app.TemplateRevisionCursor, limit int) ([]domain.TemplateRevision, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("template revision page limit must be positive")
	}
	var afterCreatedAt any
	var afterID domain.TemplateRevisionID
	if after != nil {
		afterCreatedAt = after.CreatedAt
		afterID = after.ID
	}
	rows, err := store.pool.Query(ctx, `
		select
			id,
			tenant_id,
			source_template_id,
			repo_owner,
			repo_name,
			source_ref,
			resolved_commit_sha,
			root_path,
			name,
			description,
			tags_json,
			status,
			created_at
		from template_revisions
		where tenant_id = $1
			and source_template_id = $2
			and ($3::timestamptz is null or (created_at, id) < ($3, $4))
		order by created_at desc, id desc
		limit $5
	`, tenantID, sourceTemplateID, afterCreatedAt, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("list template revisions by source: %w", err)
	}
	defer rows.Close()

	var templateRevisions []domain.TemplateRevision
	for rows.Next() {
		templateRevision, err := scanTemplateRevision(rows)
		if err != nil {
			return nil, err
		}
		templateRevisions = append(templateRevisions, templateRevision)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate template revisions by source: %w", err)
	}
	return templateRevisions, nil
}

func (store *Store) CreateStack(ctx context.Context, stack domain.Stack) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin create stack: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := insertStack(ctx, tx, stack); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit create stack: %w", err)
	}
	return nil
}

func insertStack(ctx context.Context, tx pgx.Tx, stack domain.Stack) error {
	tagsJSON, err := encodeStackTags(stack.Tags)
	if err != nil {
		return fmt.Errorf("marshal stack tags: %w", err)
	}

	credentialIDsJSON, err := encodeStackCredentialIDs(stack.DefaultCredentialIDs)
	if err != nil {
		return fmt.Errorf("marshal default credential IDs: %w", err)
	}

	// A caller that sets no status gets ready, the only one there is.
	status := stack.Status
	if status == "" {
		status = domain.StackStatusReady
	}

	_, err = tx.Exec(ctx, `
		insert into stacks (
			id,
			tenant_id,
			name,
			slug,
			status,
			tags_json,
			default_credential_ids_json,
			created_by,
			created_at
		) values ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9)
	`,
		stack.ID,
		stack.TenantID,
		stack.Name,
		stack.Slug,
		string(status),
		tagsJSON,
		credentialIDsJSON,
		stack.CreatedBy,
		stack.CreatedAt,
	)
	if duplicateConstraint(err, "stacks_tenant_id_slug_idx") {
		return app.ErrDuplicateStackSlug
	}
	if err != nil {
		return fmt.Errorf("create stack: %w", err)
	}
	return nil
}

func (store *Store) GetStack(ctx context.Context, tenantID domain.TenantID, stackID domain.StackID) (domain.Stack, error) {
	stack, err := store.getStack(ctx, tenantID, stackID)
	if err != nil {
		return domain.Stack{}, err
	}
	return stack, nil
}

func (store *Store) ListStacks(ctx context.Context, tenantID domain.TenantID) ([]domain.Stack, error) {
	rows, err := store.pool.Query(ctx, `
		select
			id,
			tenant_id,
			name,
			slug,
			status,
			tags_json,
			default_credential_ids_json,
			created_by,
			created_at
		from stacks
		where tenant_id = $1
		order by created_at desc, id desc
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list stacks: %w", err)
	}
	defer rows.Close()

	var stacks []domain.Stack
	for rows.Next() {
		stack, err := scanStack(rows)
		if err != nil {
			return nil, err
		}
		stacks = append(stacks, stack)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stacks: %w", err)
	}
	return stacks, nil
}

func (store *Store) ListStacksPage(ctx context.Context, tenantID domain.TenantID, after *app.StackPageCursor, limit int, query string) ([]domain.Stack, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("stack page limit must be positive")
	}
	var afterCreatedAt any
	var afterID domain.StackID
	if after != nil {
		afterCreatedAt = after.CreatedAt
		afterID = after.ID
	}
	rows, err := store.pool.Query(ctx, `
		select
			id,
			tenant_id,
			name,
			slug,
			status,
			tags_json,
			default_credential_ids_json,
			created_by,
			created_at
		from stacks
		where tenant_id = $1
			and ($2::timestamptz is null or (created_at, id) < ($2, $3))
			and ($5::text = '' or name ilike $6 escape '\' or slug ilike $6 escape '\')
		order by created_at desc, id desc
		limit $4
	`, tenantID, afterCreatedAt, afterID, limit, query, "%"+escapeLikePattern(query)+"%")
	if err != nil {
		return nil, fmt.Errorf("list stack page: %w", err)
	}
	defer rows.Close()

	stacks := make([]domain.Stack, 0, limit)
	for rows.Next() {
		stack, err := scanStack(rows)
		if err != nil {
			return nil, err
		}
		stacks = append(stacks, stack)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stack page: %w", err)
	}
	return stacks, nil
}

func (store *Store) GetStackWithTemplates(ctx context.Context, tenantID domain.TenantID, stackID domain.StackID) (app.StackView, error) {
	stack, err := store.getStack(ctx, tenantID, stackID)
	if err != nil {
		return app.StackView{}, err
	}

	rows, err := store.pool.Query(ctx, `
		select`+stackTemplateColumns+`
		from stack_templates
		where tenant_id = $1
			and stack_id = $2
			and lifecycle != $3
		order by id
	`, tenantID, stackID, domain.StackTemplateDestroyed)
	if err != nil {
		return app.StackView{}, fmt.Errorf("get stack templates: %w", err)
	}
	defer rows.Close()

	// The repository returns views with their label fields unset; resolving
	// those needs revision metadata, which is the service's job, not storage's.
	var templates []app.StackTemplateView
	for rows.Next() {
		stackTemplate, err := scanStackTemplate(rows)
		if err != nil {
			return app.StackView{}, err
		}
		templates = append(templates, app.StackTemplateView{StackTemplate: stackTemplate})
	}
	if err := rows.Err(); err != nil {
		return app.StackView{}, fmt.Errorf("iterate stack templates: %w", err)
	}

	return app.StackView{Stack: stack, Templates: templates}, nil
}

// ListTenantStackTemplates returns every installed template in the tenant that
// is not destroyed, across all of its stacks.
func (store *Store) ListTenantStackTemplates(ctx context.Context, tenantID domain.TenantID) ([]domain.StackTemplate, error) {
	rows, err := store.pool.Query(ctx, `
		select`+stackTemplateColumns+`
		from stack_templates
		where tenant_id = $1
			and lifecycle != $2
		order by stack_id, id
	`, tenantID, domain.StackTemplateDestroyed)
	if err != nil {
		return nil, fmt.Errorf("list tenant stack templates: %w", err)
	}
	defer rows.Close()

	templates := []domain.StackTemplate{}
	for rows.Next() {
		stackTemplate, err := scanStackTemplate(rows)
		if err != nil {
			return nil, err
		}
		templates = append(templates, stackTemplate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenant stack templates: %w", err)
	}
	return templates, nil
}

// ListTemplateRunsByStatus returns the tenant's runs in one status, across all
// of its stack templates, most recent first.
func (store *Store) ListTemplateRunsByStatus(ctx context.Context, tenantID domain.TenantID, status domain.TemplateRunStatus) ([]domain.TemplateRun, error) {
	rows, err := store.pool.Query(ctx, templateRunSelect+`
		where r.tenant_id = $1 and r.status = $2
		order by r.created_at desc nulls last, r.id desc
	`, tenantID, status)
	if err != nil {
		return nil, fmt.Errorf("list template runs by status: %w", err)
	}
	defer rows.Close()

	runs := []domain.TemplateRun{}
	for rows.Next() {
		run, err := scanTemplateRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan template run: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list template runs by status: %w", err)
	}
	return runs, nil
}

// ListLatestTemplateRuns returns each stack template's newest run in the
// tenant, one per template that has run at all, whatever the template's
// lifecycle. The attention list reads it to tell a failed plan or apply that
// is still the template's last word from one a later run has superseded.
func (store *Store) ListLatestTemplateRuns(ctx context.Context, tenantID domain.TenantID) ([]domain.TemplateRun, error) {
	// distinct on keeps the first row per template in the order below, which
	// puts each template's newest run first.
	query := strings.Replace(templateRunSelect, "select", "select distinct on (r.stack_template_id)", 1) + `
		where r.tenant_id = $1
		order by r.stack_template_id, r.created_at desc nulls last, r.id desc
	`
	rows, err := store.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list latest template runs: %w", err)
	}
	defer rows.Close()

	runs := []domain.TemplateRun{}
	for rows.Next() {
		run, err := scanTemplateRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan template run: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list latest template runs: %w", err)
	}
	return runs, nil
}

func (store *Store) CreateStackTemplate(ctx context.Context, stackTemplate domain.StackTemplate) error {
	configJSON := stackTemplate.InstalledConfigJSON
	if len(configJSON) == 0 {
		configJSON = json.RawMessage(`{}`)
	}
	desiredConfigJSON := stackTemplate.DesiredConfigJSON
	if len(desiredConfigJSON) == 0 {
		desiredConfigJSON = configJSON
	}
	componentKey := strings.TrimSpace(stackTemplate.ComponentKey)
	if componentKey == "" {
		componentKey = string(stackTemplate.ID)
	}
	desiredTemplateRevisionID := stackTemplate.DesiredTemplateRevisionID

	result, err := store.pool.Exec(ctx, `
		insert into stack_templates (
			id,
			tenant_id,
			stack_id,
			component_key,
			source_template_id,
			desired_template_revision_id,
			last_applied_template_revision_id,
			workspace_name,
			installed_config_json,
			desired_config_json,
			last_applied_run_id,
			last_applied_config_json,
			last_applied_at,
			pending_plan_run_id,
			pending_plan_template_revision_id,
			pending_plan_config_json,
			pending_plan_at,
			created_by,
			lifecycle
		)
		select $1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10::jsonb, $11, $12::jsonb, $13, $14, $15, $16::jsonb, $17, $18, $19
		where exists (
			select 1
			from stacks
			where tenant_id = $2
				and id = $3
		)
	`,
		stackTemplate.ID,
		stackTemplate.TenantID,
		stackTemplate.StackID,
		componentKey,
		stackTemplate.SourceTemplateID,
		desiredTemplateRevisionID,
		stackTemplate.LastAppliedTemplateRevisionID,
		stackTemplate.WorkspaceName,
		configJSON,
		desiredConfigJSON,
		stackTemplate.LastAppliedRunID,
		nullJSON(stackTemplate.LastAppliedConfigJSON),
		nullTime(stackTemplate.LastAppliedAt),
		stackTemplate.PendingPlanRunID,
		stackTemplate.PendingPlanTemplateRevisionID,
		nullJSON(stackTemplate.PendingPlanConfigJSON),
		nullTime(stackTemplate.PendingPlanAt),
		stackTemplate.CreatedBy,
		stackTemplate.Lifecycle,
	)
	if err != nil {
		if duplicateConstraint(err, "stack_templates_active_component_key_idx") {
			return app.ErrDuplicateStackTemplateComponentKey
		}
		// The template was deleted between the caller reading it and this
		// insert; the key refuses the install rather than orphan it.
		if foreignKeyViolation(err, "stack_templates_source_template_id_fkey") {
			return app.ErrNotFound
		}
		return fmt.Errorf("create stack template: %w", err)
	}
	if result.RowsAffected() == 0 {
		return app.ErrNotFound
	}
	return nil
}

func (store *Store) GetTemplateRevision(ctx context.Context, tenantID domain.TenantID, templateRevisionID domain.TemplateRevisionID) (domain.TemplateRevision, error) {
	row := store.pool.QueryRow(ctx, `
		select
			id,
			tenant_id,
			source_template_id,
			repo_owner,
			repo_name,
			source_ref,
			resolved_commit_sha,
			root_path,
			name,
			description,
			tags_json,
			status,
			created_at
		from template_revisions
		where tenant_id = $1
			and id = $2
	`, tenantID, templateRevisionID)
	templateRevision, err := scanTemplateRevision(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TemplateRevision{}, app.ErrNotFound
	}
	if err != nil {
		return domain.TemplateRevision{}, fmt.Errorf("get template revision: %w", err)
	}
	return templateRevision, nil
}

func (store *Store) GetStackTemplate(ctx context.Context, tenantID domain.TenantID, id domain.StackTemplateID) (domain.StackTemplate, error) {
	row := store.pool.QueryRow(ctx, `
		select`+stackTemplateColumns+`
		from stack_templates
		where tenant_id = $1
			and id = $2
	`, tenantID, id)
	stackTemplate, scanErr := scanStackTemplate(row)
	if errors.Is(scanErr, pgx.ErrNoRows) {
		return domain.StackTemplate{}, app.ErrNotFound
	}
	if scanErr != nil {
		return domain.StackTemplate{}, fmt.Errorf("get stack template: %w", scanErr)
	}
	return stackTemplate, nil
}

func (store *Store) UpdateStackTemplateConfig(ctx context.Context, tenantID domain.TenantID, id domain.StackTemplateID, configJSON json.RawMessage) (domain.StackTemplate, error) {
	row := store.pool.QueryRow(ctx, `
		update stack_templates
		set desired_config_json = $1::jsonb
		where tenant_id = $2
			and id = $3
		returning`+stackTemplateColumns, defaultJSON(configJSON), tenantID, id)
	stackTemplate, err := scanStackTemplate(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.StackTemplate{}, app.ErrNotFound
	}
	if err != nil {
		return domain.StackTemplate{}, fmt.Errorf("update stack template config: %w", err)
	}
	return stackTemplate, nil
}

func (store *Store) UpdateStackTemplateDesiredRevision(ctx context.Context, tenantID domain.TenantID, id domain.StackTemplateID, templateRevisionID domain.TemplateRevisionID, configJSON json.RawMessage) (domain.StackTemplate, error) {
	row := store.pool.QueryRow(ctx, `
		update stack_templates
		set
			desired_template_revision_id = $1,
			desired_config_json = $2::jsonb
		where tenant_id = $3
			and id = $4
		returning`+stackTemplateColumns, templateRevisionID, defaultJSON(configJSON), tenantID, id)
	stackTemplate, err := scanStackTemplate(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.StackTemplate{}, app.ErrNotFound
	}
	if err != nil {
		return domain.StackTemplate{}, fmt.Errorf("update stack template desired revision: %w", err)
	}
	return stackTemplate, nil
}

func (store *Store) CreateTemplateRun(ctx context.Context, run domain.TemplateRun) (int, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin create template run: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	runNumber, err := createTemplateRun(ctx, tx, run)
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit template run: %w", err)
	}

	return runNumber, nil
}

// createTemplateRun inserts run and returns the run number it was assigned. The
// number is max + 1 within the stack template, computed by the insert itself;
// see migration 0022 for why that needs no counter.
func createTemplateRun(ctx context.Context, exec pgxExecutor, run domain.TemplateRun) (int, error) {
	var runNumber int
	err := exec.QueryRow(ctx, `
		insert into template_runs (
			id,
			tenant_id,
			stack_template_id,
			template_revision_id,
			source_template_id,
			operation,
			selected_ref,
			resolved_commit_sha,
			workspace_name,
			config_json,
			backend_type,
			backend_config_hash,
			status,
			trigger_actor,
			created_at,
			completed_at,
			auto_approve,
			run_number
		) values (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10::jsonb, $11, $12, $13, $14, $15, $16, $17,
			(
				select coalesce(max(run_number), 0) + 1
				from template_runs
				where tenant_id = $2
					and stack_template_id = $3
			)
		)
		returning run_number
	`,
		run.ID,
		run.TenantID,
		run.StackTemplateID,
		run.TemplateRevisionID,
		run.SourceTemplateID,
		run.Operation,
		run.SelectedRef,
		run.ResolvedCommitSHA,
		run.WorkspaceName,
		defaultJSON(run.ConfigJSON),
		run.BackendType,
		run.BackendConfigHash,
		run.Status,
		run.TriggerActor,
		nullTime(run.CreatedAt),
		nullTime(run.CompletedAt),
		run.AutoApprove,
	).Scan(&runNumber)
	// The gate against concurrent runs on one stack template: the insert is the
	// check, so there is no window between deciding and writing. See migration
	// 0021 for why it lives here rather than in StartTemplateRun.
	//
	// A losing concurrent insert computed the same run number as the winner, so
	// it can trip the run number index before the in-flight one; which index
	// Postgres checks first is not something to rely on. Either means another
	// run got there first.
	if duplicateConstraint(err, "template_runs_in_flight_idx") || duplicateConstraint(err, "template_runs_run_number_idx") {
		return 0, app.ErrTemplateRunInFlight
	}
	if err != nil {
		return 0, fmt.Errorf("create template run: %w", err)
	}

	return runNumber, nil
}

// templateRunSelect reads a run as domain.TemplateRun has it. A run's step,
// error and counts are its workflow executions', so they are derived here:
//
//   - step is the latest step of the execution that started last, or none if
//     that execution has not started one: an apply between its claim and its
//     first step reads no step, not the plan's last;
//   - the error is the latest failed execution's;
//   - the counts are the plan execution's if the run has one, and otherwise
//     the apply's, which an auto-approved run has instead.
//
// Each is a lateral lookup on an indexed key, so a list of runs is still one
// query. Callers append the where clause, and the order for a list.
const templateRunSelect = `
	select
		r.id,
		r.tenant_id,
		r.stack_template_id,
		r.template_revision_id,
		r.source_template_id,
		r.operation,
		r.selected_ref,
		r.resolved_commit_sha,
		r.workspace_name,
		r.config_json,
		r.backend_type,
		r.backend_config_hash,
		r.status,
		coalesce(latest_step.step, ''),
		r.trigger_actor,
		r.created_at,
		r.completed_at,
		coalesce(failed.error_summary, ''),
		coalesce(failed.command_error, ''),
		r.run_number,
		r.auto_approve,
		counts.resource_add,
		counts.resource_change,
		counts.resource_destroy,
		r.plan_changes_state,
		r.plan_changes_total
	from template_runs r
	left join lateral (
		select s.step
		from template_run_workflow_executions e
		left join template_run_step_executions s on s.workflow_execution_id = e.id
		where e.tenant_id = r.tenant_id and e.run_id = r.id
		order by e.id desc, s.id desc nulls last
		limit 1
	) latest_step on true
	left join lateral (
		select e.error_summary, e.command_error
		from template_run_workflow_executions e
		where e.tenant_id = r.tenant_id and e.run_id = r.id and e.status = 'failed'
		order by e.id desc
		limit 1
	) failed on true
	left join lateral (
		select e.resource_add, e.resource_change, e.resource_destroy
		from template_run_workflow_executions e
		where e.tenant_id = r.tenant_id and e.run_id = r.id
		order by e.phase = 'plan' desc, e.id
		limit 1
	) counts on true
`

// scanTemplateRun scans one row of templateRunSelect.
func scanTemplateRun(row pgx.Row) (domain.TemplateRun, error) {
	var run domain.TemplateRun
	var createdAt sql.NullTime
	var completedAt sql.NullTime
	var add, change, destroy *int
	var planChangesState *string
	if err := row.Scan(
		&run.ID,
		&run.TenantID,
		&run.StackTemplateID,
		&run.TemplateRevisionID,
		&run.SourceTemplateID,
		&run.Operation,
		&run.SelectedRef,
		&run.ResolvedCommitSHA,
		&run.WorkspaceName,
		&run.ConfigJSON,
		&run.BackendType,
		&run.BackendConfigHash,
		&run.Status,
		&run.Step,
		&run.TriggerActor,
		&createdAt,
		&completedAt,
		&run.ErrorSummary,
		&run.CommandError,
		&run.RunNumber,
		&run.AutoApprove,
		&add,
		&change,
		&destroy,
		&planChangesState,
		&run.PlanChangesTotal,
	); err != nil {
		return domain.TemplateRun{}, err
	}
	if planChangesState != nil {
		run.PlanChangesState = domain.PlanChangesState(*planChangesState)
	}
	if createdAt.Valid {
		run.CreatedAt = createdAt.Time
	}
	if completedAt.Valid {
		run.CompletedAt = completedAt.Time
	}
	run.PlanSummary = planSummary(add, change, destroy)
	return run, nil
}

func (store *Store) GetTemplateRun(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID) (domain.TemplateRun, error) {
	run, err := scanTemplateRun(store.pool.QueryRow(ctx, templateRunSelect+`
		where r.tenant_id = $1 and r.id = $2
	`, tenantID, runID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TemplateRun{}, app.ErrNotFound
	}
	if err != nil {
		return domain.TemplateRun{}, fmt.Errorf("get template run: %w", err)
	}
	return run, nil
}

// planSummary reassembles an execution's counts, which are written together:
// either all three are set or none is.
func planSummary(add, change, destroy *int) *domain.PlanSummary {
	if add == nil || change == nil || destroy == nil {
		return nil
	}
	return &domain.PlanSummary{Add: *add, Change: *change, Destroy: *destroy}
}

// GetTemplateRunByNumber reads a run by the number people see, which is
// unique within its stack template.
func (store *Store) GetTemplateRunByNumber(ctx context.Context, tenantID domain.TenantID, stackTemplateID domain.StackTemplateID, runNumber int) (domain.TemplateRun, error) {
	run, err := scanTemplateRun(store.pool.QueryRow(ctx, templateRunSelect+`
		where r.tenant_id = $1 and r.stack_template_id = $2 and r.run_number = $3
	`, tenantID, stackTemplateID, runNumber))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TemplateRun{}, app.ErrNotFound
	}
	if err != nil {
		return domain.TemplateRun{}, fmt.Errorf("get template run by number: %w", err)
	}
	return run, nil
}

// ListTemplateRunsPage returns up to limit runs numbered below
// beforeRunNumber, newest first; 0 starts from the newest. run_number is the
// key because it is unique per stack template (template_runs_run_number_idx)
// and assigned in creation order, so it is that order with an index behind it.
func (store *Store) ListTemplateRunsPage(ctx context.Context, tenantID domain.TenantID, stackTemplateID domain.StackTemplateID, beforeRunNumber, limit int) ([]domain.TemplateRun, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("template run page limit must be positive")
	}
	rows, err := store.pool.Query(ctx, templateRunSelect+`
		where r.tenant_id = $1 and r.stack_template_id = $2
			and ($3::integer = 0 or r.run_number < $3)
		order by r.run_number desc
		limit $4
	`, tenantID, stackTemplateID, beforeRunNumber, limit)
	if err != nil {
		return nil, fmt.Errorf("list template run page: %w", err)
	}
	defer rows.Close()

	runs := []domain.TemplateRun{}
	for rows.Next() {
		run, err := scanTemplateRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan template run: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list template run page: %w", err)
	}
	return runs, nil
}

func (store *Store) ListTemplateRuns(ctx context.Context, tenantID domain.TenantID, stackTemplateID domain.StackTemplateID) ([]domain.TemplateRun, error) {
	rows, err := store.pool.Query(ctx, templateRunSelect+`
		where r.tenant_id = $1 and r.stack_template_id = $2
		order by r.created_at desc nulls last, r.id desc
	`, tenantID, stackTemplateID)
	if err != nil {
		return nil, fmt.Errorf("list template runs: %w", err)
	}
	defer rows.Close()

	runs := []domain.TemplateRun{}
	for rows.Next() {
		run, err := scanTemplateRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan template run: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list template runs: %w", err)
	}
	return runs, nil
}

// RecordTemplateRunLog upserts one log's metadata, keyed by its run phase and
// number. The database derives its name, phase, from those two. The same row
// moves from live to complete or lost as the command's output is uploaded or
// given up on.
func (store *Store) RecordTemplateRunLog(ctx context.Context, log domain.TemplateRunLog) error {
	// The object columns are null until the output is in the object store,
	// and lost_reason is null unless the log is lost: the table's checks
	// refuse a row whose state and columns disagree, so a zero value must
	// not be written as though it were real.
	var objectKey, contentType, lostReason *string
	var sizeBytes *int64
	var uploadedAt *time.Time
	if log.State == domain.TemplateRunLogComplete {
		objectKey, contentType = &log.ObjectKey, &log.ContentType
		sizeBytes, uploadedAt = &log.SizeBytes, &log.UploadedAt
	}
	if log.LostReason != "" {
		reason := string(log.LostReason)
		lostReason = &reason
	}
	result, err := store.pool.Exec(ctx, `
		insert into template_run_logs (
			tenant_id,
			run_id,
			run_phase,
			seq,
			command,
			state,
			lost_reason,
			object_key,
			content_type,
			size_bytes,
			uploaded_at
		)
		select $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
		where exists (
			select 1
			from template_runs
			where tenant_id = $1
				and id = $2
		)
		on conflict (tenant_id, run_id, run_phase, seq) do update set
			command = excluded.command,
			state = excluded.state,
			lost_reason = excluded.lost_reason,
			object_key = excluded.object_key,
			content_type = excluded.content_type,
			size_bytes = excluded.size_bytes,
			uploaded_at = excluded.uploaded_at
	`,
		log.TenantID,
		log.RunID,
		log.RunPhase,
		log.Seq,
		log.Command,
		log.State,
		lostReason,
		objectKey,
		contentType,
		sizeBytes,
		uploadedAt,
	)
	if err != nil {
		return fmt.Errorf("record template run log: %w", err)
	}
	if result.RowsAffected() == 0 {
		return app.ErrNotFound
	}
	return nil
}

func (store *Store) GetTemplateRunLog(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID, name string) (domain.TemplateRunLog, error) {
	log, err := scanTemplateRunLog(store.pool.QueryRow(ctx, `
		select
			tenant_id,
			run_id,
			name,
			run_phase,
			seq,
			command,
			state,
			lost_reason,
			object_key,
			content_type,
			size_bytes,
			uploaded_at
		from template_run_logs
		where tenant_id = $1
			and run_id = $2
			and name = $3
	`, tenantID, runID, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TemplateRunLog{}, app.ErrNotFound
	}
	if err != nil {
		return domain.TemplateRunLog{}, fmt.Errorf("get template run log: %w", err)
	}
	return log, nil
}

func (store *Store) ListTemplateRunLogs(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID) ([]domain.TemplateRunLog, error) {
	rows, err := store.pool.Query(ctx, `
		select
			tenant_id,
			run_id,
			name,
			run_phase,
			seq,
			command,
			state,
			lost_reason,
			object_key,
			content_type,
			size_bytes,
			uploaded_at
		from template_run_logs
		where tenant_id = $1
			and run_id = $2
		order by case run_phase when 'plan' then 0 else 1 end, seq
	`, tenantID, runID)
	if err != nil {
		return nil, fmt.Errorf("list template run logs: %w", err)
	}
	defer rows.Close()

	var logs []domain.TemplateRunLog
	for rows.Next() {
		log, err := scanTemplateRunLog(rows)
		if err != nil {
			return nil, fmt.Errorf("scan template run log: %w", err)
		}
		logs = append(logs, log)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate template run logs: %w", err)
	}

	return logs, nil
}

// scanTemplateRunLog reads one row of the columns GetTemplateRunLog and
// ListTemplateRunLogs select, in their order. The nullable columns come back
// as the domain's zero values, so a log that is not complete carries no
// object fields and one that is not lost no reason.
func scanTemplateRunLog(row pgx.Row) (domain.TemplateRunLog, error) {
	var log domain.TemplateRunLog
	var objectKey, contentType, lostReason *string
	var sizeBytes *int64
	var uploadedAt *time.Time
	if err := row.Scan(
		&log.TenantID,
		&log.RunID,
		&log.Name,
		&log.RunPhase,
		&log.Seq,
		&log.Command,
		&log.State,
		&lostReason,
		&objectKey,
		&contentType,
		&sizeBytes,
		&uploadedAt,
	); err != nil {
		return domain.TemplateRunLog{}, err
	}
	if lostReason != nil {
		log.LostReason = domain.TemplateRunLogLostReason(*lostReason)
	}
	if objectKey != nil {
		log.ObjectKey = *objectKey
	}
	if contentType != nil {
		log.ContentType = *contentType
	}
	if sizeBytes != nil {
		log.SizeBytes = *sizeBytes
	}
	if uploadedAt != nil {
		log.UploadedAt = *uploadedAt
	}
	return log, nil
}

func (store *Store) ApproveTemplateRun(ctx context.Context, approval domain.TemplateRunApproval) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin approve template run: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := approveTemplateRun(ctx, tx, approval); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit approve template run: %w", err)
	}

	return nil
}

func approveTemplateRun(ctx context.Context, exec pgxExecutor, approval domain.TemplateRunApproval) error {
	commandTag, err := exec.Exec(ctx, `
		update template_runs
		set status = $1
		where tenant_id = $2
			and id = $3
			and status = $4
	`,
		domain.TemplateRunApproved,
		approval.TenantID,
		approval.RunID,
		domain.TemplateRunWaitingApproval,
	)
	if err != nil {
		return fmt.Errorf("approve template run status: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return app.ErrRunNotApprovable
	}

	if _, err := exec.Exec(ctx, `
		insert into template_run_approvals (
			run_id,
			tenant_id,
			approved_by,
			approved_at
		) values ($1, $2, $3, $4)
	`,
		approval.RunID,
		approval.TenantID,
		approval.ApprovedBy,
		approval.ApprovedAt,
	); err != nil {
		return fmt.Errorf("insert template run approval: %w", err)
	}

	return nil
}

func (store *Store) AppendAuditEvent(ctx context.Context, event domain.SecurityAuditEvent) error {
	return appendAuditEvent(ctx, store.pool, event)
}

// appendAuditEvent holds the insert so both the standalone method and the
// transaction-scoped repository share one copy of the SQL.
func appendAuditEvent(ctx context.Context, exec pgxExecutor, event domain.SecurityAuditEvent) error {
	_, err := exec.Exec(ctx,
		`INSERT INTO security_audit_log
			(actor_subject, action, target_user, tenant_id, stack_id, old_role, new_role, outcome, correlation_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		event.ActorSubject, event.Action, event.TargetUser,
		event.TenantID, event.StackID, event.OldRole, event.NewRole,
		event.Outcome, event.CorrelationID,
	)
	return err
}

// ErrTemplateRunTransition is a status write the run's current state does not
// allow.
var ErrTemplateRunTransition = errors.New("postgres: template run cannot make that transition")

// workflowStatusSources is every status the workflow records, with the
// statuses a run may be in when it does. Waiting, approved and canceled are
// written by the approval flow, never here.
var workflowStatusSources = map[domain.TemplateRunStatus][]domain.TemplateRunStatus{
	domain.TemplateRunRunning:   {domain.TemplateRunQueued},
	domain.TemplateRunCompleted: {domain.TemplateRunRunning},
	domain.TemplateRunFailed: {
		domain.TemplateRunQueued,
		domain.TemplateRunRunning,
		domain.TemplateRunWaitingApproval,
		domain.TemplateRunApproved,
	},
}

// RecordTemplateRunStatus moves a run along its lifecycle, with the run row
// locked. A run already in the status is this write retried after its
// acknowledgement was lost, so it succeeds and changes nothing.
//
// Becoming terminal sets completed_at, drops the run's saved plan, and, for a
// destroy that failed after it began destroying, leaves the stack template
// failed.
//
// The run's workflow execution moves with it: running starts the plan's,
// and becoming terminal ends the input phase's and the step it was on (see
// endWorkflowExecution). A failed run still reads the step it failed on. A
// terminal write from a workflow that no longer carries the run, such as a
// plan failing after its lost FinishPlan acknowledgement, changes nothing.
func (store *Store) RecordTemplateRunStatus(ctx context.Context, input domain.TemplateRunStatusActivityInput) error {
	sources, ok := workflowStatusSources[input.Status]
	if !ok {
		return fmt.Errorf("record template run status: the workflow does not record %q", input.Status)
	}

	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin record template run status: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var current domain.TemplateRunStatus
	err = tx.QueryRow(ctx, `
		select status
		from template_runs
		where tenant_id = $1
			and id = $2
			and stack_template_id = $3
			and operation = $4
		for update
	`, input.TenantID, input.RunID, input.StackTemplateID, input.Operation).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read template run status: %w", err)
	}
	if current == input.Status {
		return nil
	}
	if !slices.Contains(sources, current) {
		return fmt.Errorf("%w: %q cannot become %q", ErrTemplateRunTransition, current, input.Status)
	}
	if !input.Phase.Valid() {
		return fmt.Errorf("record template run status: unknown phase %q", input.Phase)
	}

	if !input.Status.Terminal() {
		if _, err := tx.Exec(ctx, `
			update template_runs set status = $1 where tenant_id = $2 and id = $3
		`, input.Status, input.TenantID, input.RunID); err != nil {
			return fmt.Errorf("record template run status: %w", err)
		}
		// Running is the only status recorded here, and only the plan
		// workflow records it: an apply claims its run with
		// BeginTemplateApply. The plan's execution starts with it.
		if _, err := insertWorkflowExecution(ctx, tx, input.TenantID, input.RunID, input.Phase, input.WorkflowID, domain.TemplateRunExecutionRunning, "", ""); err != nil {
			return err
		}
		return commitTemplateRunStatus(ctx, tx)
	}

	carried, err := endWorkflowExecution(ctx, tx, input)
	if err != nil {
		return err
	}
	if !carried {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		update template_runs
		set status = $1, completed_at = coalesce(completed_at, now())
		where tenant_id = $2 and id = $3
	`, input.Status, input.TenantID, input.RunID); err != nil {
		return fmt.Errorf("record template run status: %w", err)
	}
	if input.Status == domain.TemplateRunFailed && input.Operation == domain.OperationDestroy {
		if err := recordInterruptedDestroyLifecycle(ctx, tx, input.TenantID, input.StackTemplateID); err != nil {
			return err
		}
	}
	if err := releaseRunPlan(ctx, tx, input.TenantID, input.RunID); err != nil {
		return err
	}
	return commitTemplateRunStatus(ctx, tx)
}

func commitTemplateRunStatus(ctx context.Context, tx pgx.Tx) error {
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit record template run status: %w", err)
	}
	return nil
}

// releaseRunPlan is what a run becoming terminal does to its saved plan. Its
// key is dropped, so the plan file left in the artifact store can never be
// opened again, whoever holds the file; and the plan stops being the stack
// template's pending plan, so a discarded or failed plan no longer counts as
// reviewed. Every path that makes a run terminal calls it in the same
// transaction.
func releaseRunPlan(ctx context.Context, exec pgxExecutor, tenantID domain.TenantID, runID domain.TemplateRunID) error {
	if _, err := exec.Exec(ctx, `
		update template_runs
		set plan_artifact_dek = null
		where tenant_id = $1 and id = $2
	`, tenantID, runID); err != nil {
		return fmt.Errorf("drop plan key: %w", err)
	}
	if _, err := exec.Exec(ctx, `
		update stack_templates
		set
			pending_plan_run_id = '',
			pending_plan_template_revision_id = '',
			pending_plan_config_json = null,
			pending_plan_at = null
		where tenant_id = $1 and pending_plan_run_id = $2
	`, tenantID, runID); err != nil {
		return fmt.Errorf("clear pending plan: %w", err)
	}
	return nil
}

type stackTemplateLastAppliedWriter interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

type stackTemplateLifecycleWriter interface {
	stackTemplateLastAppliedWriter
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// recordStackTemplateLastApplied makes the run the stack template's live
// state. runStatus is the status the run must be in, so a run that has
// already moved on cannot become live.
func recordStackTemplateLastApplied(ctx context.Context, writer stackTemplateLastAppliedWriter, tenantID domain.TenantID, stackTemplateID domain.StackTemplateID, runID domain.TemplateRunID, runStatus domain.TemplateRunStatus) error {
	commandTag, err := writer.Exec(ctx, `
		update stack_templates
		set
			last_applied_run_id = template_runs.id,
			last_applied_template_revision_id = template_runs.template_revision_id,
			last_applied_config_json = template_runs.config_json,
			last_applied_at = now()
		from template_runs
		where stack_templates.tenant_id = $1
			and stack_templates.id = $2
			and template_runs.tenant_id = stack_templates.tenant_id
			and template_runs.id = $3
			and template_runs.stack_template_id = stack_templates.id
			and template_runs.operation = $4
			and template_runs.status = $5
	`, tenantID, stackTemplateID, runID, domain.OperationApply, runStatus)
	if err != nil {
		return fmt.Errorf("record stack template last applied: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func recordStackTemplateLifecycle(ctx context.Context, writer stackTemplateLastAppliedWriter, tenantID domain.TenantID, stackTemplateID domain.StackTemplateID, lifecycle domain.StackTemplateLifecycle) error {
	commandTag, err := writer.Exec(ctx, `
		update stack_templates
		set lifecycle = $1
		where tenant_id = $2
			and id = $3
	`, lifecycle, tenantID, stackTemplateID)
	if err != nil {
		return fmt.Errorf("record stack template lifecycle: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func recordInterruptedDestroyLifecycle(ctx context.Context, writer stackTemplateLifecycleWriter, tenantID domain.TenantID, stackTemplateID domain.StackTemplateID) error {
	var lifecycle domain.StackTemplateLifecycle
	err := writer.QueryRow(ctx, `
		select lifecycle
		from stack_templates
		where tenant_id = $1 and id = $2
		for update
	`, tenantID, stackTemplateID).Scan(&lifecycle)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read interrupted destroy stack template lifecycle: %w", err)
	}
	if lifecycle != domain.StackTemplateDestroying {
		return nil
	}
	return recordStackTemplateLifecycle(ctx, writer, tenantID, stackTemplateID, domain.StackTemplateFailed)
}

// stackTemplateColumns are a stack_templates row's columns in the order
// scanStackTemplate scans them, for a select list or a returning clause.
const stackTemplateColumns = `
	id,
	tenant_id,
	stack_id,
	component_key,
	source_template_id,
	desired_template_revision_id,
	last_applied_template_revision_id,
	workspace_name,
	installed_config_json,
	desired_config_json,
	last_applied_run_id,
	last_applied_config_json,
	last_applied_at,
	pending_plan_run_id,
	pending_plan_template_revision_id,
	pending_plan_config_json,
	pending_plan_at,
	created_by,
	lifecycle
`

type stackTemplateScanner interface {
	Scan(dest ...any) error
}

type stackScanner interface {
	Scan(dest ...any) error
}

type templateScanner interface {
	Scan(dest ...any) error
}

func scanStackTemplate(scanner stackTemplateScanner) (domain.StackTemplate, error) {
	var stackTemplate domain.StackTemplate
	var configJSON []byte
	var desiredConfigJSON []byte
	var lastAppliedConfigJSON []byte
	var lastPlannedConfigJSON []byte
	var lastAppliedAt sql.NullTime
	var lastPlannedAt sql.NullTime

	if err := scanner.Scan(
		&stackTemplate.ID,
		&stackTemplate.TenantID,
		&stackTemplate.StackID,
		&stackTemplate.ComponentKey,
		&stackTemplate.SourceTemplateID,
		&stackTemplate.DesiredTemplateRevisionID,
		&stackTemplate.LastAppliedTemplateRevisionID,
		&stackTemplate.WorkspaceName,
		&configJSON,
		&desiredConfigJSON,
		&stackTemplate.LastAppliedRunID,
		&lastAppliedConfigJSON,
		&lastAppliedAt,
		&stackTemplate.PendingPlanRunID,
		&stackTemplate.PendingPlanTemplateRevisionID,
		&lastPlannedConfigJSON,
		&lastPlannedAt,
		&stackTemplate.CreatedBy,
		&stackTemplate.Lifecycle,
	); err != nil {
		return domain.StackTemplate{}, fmt.Errorf("scan stack template: %w", err)
	}
	stackTemplate.InstalledConfigJSON = configJSON
	stackTemplate.DesiredConfigJSON = desiredConfigJSON
	// A nil scan target stays nil rather than becoming an empty RawMessage, so
	// the state comparisons can tell "never recorded" from "recorded as '{}'".
	if lastAppliedConfigJSON != nil {
		stackTemplate.LastAppliedConfigJSON = lastAppliedConfigJSON
	}
	if lastPlannedConfigJSON != nil {
		stackTemplate.PendingPlanConfigJSON = lastPlannedConfigJSON
	}
	if lastAppliedAt.Valid {
		stackTemplate.LastAppliedAt = lastAppliedAt.Time
	}
	if lastPlannedAt.Valid {
		stackTemplate.PendingPlanAt = lastPlannedAt.Time
	}
	return stackTemplate, nil
}

func scanTemplateRevision(scanner templateScanner) (domain.TemplateRevision, error) {
	var templateRevision domain.TemplateRevision
	var tagsJSON []byte

	if err := scanner.Scan(
		&templateRevision.ID,
		&templateRevision.TenantID,
		&templateRevision.SourceTemplateID,
		&templateRevision.RepoOwner,
		&templateRevision.RepoName,
		&templateRevision.SourceRef,
		&templateRevision.ResolvedCommitSHA,
		&templateRevision.RootPath,
		&templateRevision.Name,
		&templateRevision.Description,
		&tagsJSON,
		&templateRevision.Status,
		&templateRevision.CreatedAt,
	); err != nil {
		return domain.TemplateRevision{}, err
	}
	tags, err := decodeTemplateRevisionTags(tagsJSON)
	if err != nil {
		return domain.TemplateRevision{}, fmt.Errorf("unmarshal template revision tags: %w", err)
	}
	templateRevision.Tags = tags
	return templateRevision, nil
}

// json.Marshal writes a nil slice or map as null, and the API serves these
// columns as it reads them, so no tags are stored as an empty array or
// object. Rows stored as null before that are read as empty.

// encodeTemplateRevisionTags is a revision's tags as stored: [] for none.
func encodeTemplateRevisionTags(tags []string) ([]byte, error) {
	if tags == nil {
		tags = []string{}
	}
	return json.Marshal(tags)
}

// decodeTemplateRevisionTags reads stored tags, an empty slice for none.
func decodeTemplateRevisionTags(tagsJSON []byte) ([]string, error) {
	var tags []string
	if err := json.Unmarshal(tagsJSON, &tags); err != nil {
		return nil, err
	}
	if tags == nil {
		tags = []string{}
	}
	return tags, nil
}

// encodeStackTags is a stack's tags as stored: {} for none.
func encodeStackTags(tags map[string]string) ([]byte, error) {
	if tags == nil {
		tags = map[string]string{}
	}
	return json.Marshal(tags)
}

// decodeStackTags reads stored stack tags, an empty map for none.
func decodeStackTags(tagsJSON []byte) (map[string]string, error) {
	var tags map[string]string
	if err := json.Unmarshal(tagsJSON, &tags); err != nil {
		return nil, err
	}
	if tags == nil {
		tags = map[string]string{}
	}
	return tags, nil
}

// encodeStackCredentialIDs is a stack's default credentials as stored: [] for
// none.
func encodeStackCredentialIDs(ids []domain.CredentialID) ([]byte, error) {
	if ids == nil {
		ids = []domain.CredentialID{}
	}
	return json.Marshal(ids)
}

// decodeStackCredentialIDs reads stored default credentials, an empty slice
// for none.
func decodeStackCredentialIDs(idsJSON []byte) ([]domain.CredentialID, error) {
	var ids []domain.CredentialID
	if err := json.Unmarshal(idsJSON, &ids); err != nil {
		return nil, err
	}
	if ids == nil {
		ids = []domain.CredentialID{}
	}
	return ids, nil
}

func scanStack(scanner stackScanner) (domain.Stack, error) {
	var stack domain.Stack
	var tagsJSON []byte
	var credentialIDsJSON []byte

	var status string

	if err := scanner.Scan(
		&stack.ID,
		&stack.TenantID,
		&stack.Name,
		&stack.Slug,
		&status,
		&tagsJSON,
		&credentialIDsJSON,
		&stack.CreatedBy,
		&stack.CreatedAt,
	); err != nil {
		return domain.Stack{}, err
	}
	stack.Status = domain.StackStatus(status)
	tags, err := decodeStackTags(tagsJSON)
	if err != nil {
		return domain.Stack{}, fmt.Errorf("unmarshal stack tags: %w", err)
	}
	stack.Tags = tags
	credentialIDs, err := decodeStackCredentialIDs(credentialIDsJSON)
	if err != nil {
		return domain.Stack{}, fmt.Errorf("unmarshal stack credential IDs: %w", err)
	}
	stack.DefaultCredentialIDs = credentialIDs
	return stack, nil
}

func (store *Store) getStack(ctx context.Context, tenantID domain.TenantID, stackID domain.StackID) (domain.Stack, error) {
	row := store.pool.QueryRow(ctx, `
		select
			id,
			tenant_id,
			name,
			slug,
			status,
			tags_json,
			default_credential_ids_json,
			created_by,
			created_at
		from stacks
		where tenant_id = $1
			and id = $2
	`, tenantID, stackID)
	stack, err := scanStack(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Stack{}, app.ErrNotFound
	}
	if err != nil {
		return domain.Stack{}, fmt.Errorf("get stack: %w", err)
	}
	return stack, nil
}

func duplicateConstraint(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}

func foreignKeyViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503" && pgErr.ConstraintName == constraint
}

func recordLatestTemplateRevision(ctx context.Context, tx pgx.Tx, templateRevision domain.TemplateRevision) error {
	commandTag, err := tx.Exec(ctx, `
		update source_templates
		set
			latest_template_revision_id = $1,
			updated_at = now()
		where tenant_id = $2
			and id = $3
	`,
		templateRevision.ID,
		templateRevision.TenantID,
		templateRevision.SourceTemplateID,
	)
	if err != nil {
		return fmt.Errorf("record latest template revision: %w", err)
	}
	// A reused revision takes no lock on its template, so a delete can commit
	// between the insert and this update.
	if commandTag.RowsAffected() == 0 {
		return sourceTemplateGone(templateRevision.SourceTemplateID)
	}

	return nil
}

// sourceTemplateGone is the error for a revision whose template no longer
// exists. It wraps app.ErrNotFound, the store's not found, and os.ErrNotExist,
// which the sync activity checks for: the activities package can't import app,
// because the executor shares it and must not carry app's dependencies.
func sourceTemplateGone(id domain.SourceTemplateID) error {
	return fmt.Errorf("source template %s: %w (%w)", id, app.ErrNotFound, os.ErrNotExist)
}

func defaultJSON(input json.RawMessage) json.RawMessage {
	if len(input) == 0 {
		return json.RawMessage(`{}`)
	}
	return input
}

// nullJSON keeps an absent config absent. The snapshot config columns are
// nullable precisely so "never recorded" stays distinguishable from "recorded
// as the empty object", which is what an all-optional template's config is, so
// empty must not be coerced to '{}' the way defaultJSON does.
func nullJSON(input json.RawMessage) any {
	if len(input) == 0 {
		return nil
	}
	return input
}

func insertTemplateRevision(ctx context.Context, tx pgx.Tx, templateRevision domain.TemplateRevision, tagsJSON []byte) (domain.TemplateRevision, bool, error) {
	var inserted domain.TemplateRevision
	var insertedTagsJSON []byte
	err := tx.QueryRow(ctx, `
		insert into template_revisions (
			id,
			tenant_id,
			source_template_id,
			repo_owner,
			repo_name,
			source_ref,
			resolved_commit_sha,
			root_path,
			name,
			description,
			tags_json,
			status,
			created_at
		) values (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10, $11::jsonb, $12, coalesce($13::timestamptz, now())
		)
		on conflict (tenant_id, source_template_id, resolved_commit_sha) do nothing
		returning
			id,
			tenant_id,
			source_template_id,
			repo_owner,
			repo_name,
			source_ref,
			resolved_commit_sha,
			root_path,
			name,
			description,
			tags_json,
			status,
			created_at
	`,
		templateRevision.ID,
		templateRevision.TenantID,
		templateRevision.SourceTemplateID,
		templateRevision.RepoOwner,
		templateRevision.RepoName,
		templateRevision.SourceRef,
		templateRevision.ResolvedCommitSHA,
		templateRevision.RootPath,
		templateRevision.Name,
		templateRevision.Description,
		tagsJSON,
		templateRevision.Status,
		nullTime(templateRevision.CreatedAt),
	).Scan(
		&inserted.ID,
		&inserted.TenantID,
		&inserted.SourceTemplateID,
		&inserted.RepoOwner,
		&inserted.RepoName,
		&inserted.SourceRef,
		&inserted.ResolvedCommitSHA,
		&inserted.RootPath,
		&inserted.Name,
		&inserted.Description,
		&insertedTagsJSON,
		&inserted.Status,
		&inserted.CreatedAt,
	)
	if err == nil {
		tags, err := decodeTemplateRevisionTags(insertedTagsJSON)
		if err != nil {
			return domain.TemplateRevision{}, false, fmt.Errorf("unmarshal inserted template revision tags: %w", err)
		}
		inserted.Tags = tags
		return inserted, true, nil
	}
	if foreignKeyViolation(err, "template_revisions_source_template_id_fkey") {
		return domain.TemplateRevision{}, false, sourceTemplateGone(templateRevision.SourceTemplateID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.TemplateRevision{}, false, fmt.Errorf("insert template revision: %w", err)
	}

	selected, err := selectTemplateRevisionByIdentity(ctx, tx, templateRevision)
	if err != nil {
		return domain.TemplateRevision{}, false, err
	}
	return selected, false, nil
}

func selectTemplateRevisionByIdentity(ctx context.Context, tx pgx.Tx, templateRevision domain.TemplateRevision) (domain.TemplateRevision, error) {
	var selected domain.TemplateRevision
	var tagsJSON []byte
	err := tx.QueryRow(ctx, `
		select
			id,
			tenant_id,
			source_template_id,
			repo_owner,
			repo_name,
			source_ref,
			resolved_commit_sha,
			root_path,
			name,
			description,
			tags_json,
			status,
			created_at
		from template_revisions
		where tenant_id = $1
			and source_template_id = $2
			and resolved_commit_sha = $3
	`,
		templateRevision.TenantID,
		templateRevision.SourceTemplateID,
		templateRevision.ResolvedCommitSHA,
	).Scan(
		&selected.ID,
		&selected.TenantID,
		&selected.SourceTemplateID,
		&selected.RepoOwner,
		&selected.RepoName,
		&selected.SourceRef,
		&selected.ResolvedCommitSHA,
		&selected.RootPath,
		&selected.Name,
		&selected.Description,
		&tagsJSON,
		&selected.Status,
		&selected.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TemplateRevision{}, app.ErrNotFound
	}
	if err != nil {
		return domain.TemplateRevision{}, fmt.Errorf("select template revision by identity: %w", err)
	}
	tags, err := decodeTemplateRevisionTags(tagsJSON)
	if err != nil {
		return domain.TemplateRevision{}, fmt.Errorf("unmarshal selected template revision tags: %w", err)
	}
	selected.Tags = tags
	return selected, nil
}

func terminalTemplateRegistrationStatus(status domain.TemplateRegistrationStatus) bool {
	switch status {
	case domain.TemplateRegistrationCompleted, domain.TemplateRegistrationInvalid, domain.TemplateRegistrationFailed:
		return true
	case domain.TemplateRegistrationPending, domain.TemplateRegistrationRunning:
		return false
	default:
		return false
	}
}

func nullTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}

	return value
}
