package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vishu42/openplan/internal/domain"
)

const recordTenant = domain.TenantID("tenant_123")

// seedSyncableTemplate stores a template tracking ref, and a running sync of
// it, which is the state RecordSyncedTemplateRevision is called in.
func seedSyncableTemplate(t *testing.T, ctx context.Context, store *Store, templateID domain.TemplateID, ref string, syncID domain.TemplateSyncID) {
	t.Helper()

	if _, err := store.CreateTemplate(ctx, domain.Template{
		ID:        templateID,
		TenantID:  recordTenant,
		RepoOwner: "acme",
		RepoName:  "infra",
		SourceRef: ref,
		RootPath:  "modules/vpc",
	}); err != nil {
		t.Fatalf("seed template %s: %v", templateID, err)
	}
	seedRunningSync(t, ctx, store, templateID, ref, syncID)
}

func seedRunningSync(t *testing.T, ctx context.Context, store *Store, templateID domain.TemplateID, ref string, syncID domain.TemplateSyncID) {
	t.Helper()

	if err := store.CreateTemplateSync(ctx, domain.TemplateSync{
		ID:          syncID,
		TenantID:    recordTenant,
		TemplateID:  templateID,
		RepoOwner:   "acme",
		RepoName:    "infra",
		SourceRef:   ref,
		RootPath:    "modules/vpc",
		Status:      domain.TemplateSyncStateRunning,
		RequestedBy: "user_123",
		RequestedAt: time.Date(2026, 11, 10, 11, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("seed sync %s: %v", syncID, err)
	}
}

// syncedRevision is what the sync activity read at sha: a revision with no ID.
func syncedRevision(templateID domain.TemplateID, ref, sha string) domain.TemplateRevision {
	return domain.TemplateRevision{
		TenantID:          recordTenant,
		TemplateID:        templateID,
		RepoOwner:         "acme",
		RepoName:          "infra",
		SourceRef:         ref,
		ResolvedCommitSHA: sha,
		RootPath:          "modules/vpc",
		Name:              "vpc",
		Status:            domain.TemplateRevisionActive,
	}
}

type recordedSync struct {
	status     domain.TemplateSyncState
	revisionID domain.TemplateRevisionID
	commitSHA  string
	upToDate   bool
}

func readRecordedSync(t *testing.T, ctx context.Context, pool *pgxpool.Pool, syncID domain.TemplateSyncID) recordedSync {
	t.Helper()

	var got recordedSync
	if err := pool.QueryRow(ctx, `
		select status, template_revision_id, resolved_commit_sha, up_to_date
		from template_syncs
		where tenant_id = $1 and id = $2
	`, recordTenant, syncID).Scan(&got.status, &got.revisionID, &got.commitSHA, &got.upToDate); err != nil {
		t.Fatalf("read sync %s: %v", syncID, err)
	}
	return got
}

func countRevisions(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()

	var count int
	if err := pool.QueryRow(ctx, `select count(*) from template_revisions`).Scan(&count); err != nil {
		t.Fatalf("count revisions: %v", err)
	}
	return count
}

func TestRecordSyncedTemplateRevisionStoresANewRevision(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	seedSyncableTemplate(t, ctx, store, "template_vpc", "main", "template_sync_1")

	variables := []domain.TemplateVariable{
		{Name: "region", TypeExpression: "string", Required: true},
		{Name: "cidr", TypeExpression: "string", HasDefault: true},
	}
	stored, err := store.RecordSyncedTemplateRevision(ctx, "template_sync_1", syncedRevision("template_vpc", "main", "abc123"), variables)
	if err != nil {
		t.Fatalf("RecordSyncedTemplateRevision: %v", err)
	}

	if !strings.HasPrefix(string(stored.ID), "template_revision_") {
		t.Fatalf("revision ID = %q, want the template_revision_ prefix", stored.ID)
	}
	want := recordedSync{status: domain.TemplateSyncStateCompleted, revisionID: stored.ID, commitSHA: "abc123", upToDate: false}
	if got := readRecordedSync(t, ctx, pool, "template_sync_1"); got != want {
		t.Fatalf("sync = %+v, want %+v", got, want)
	}
	if got := latestRevisionOf(t, ctx, store, "template_vpc"); got != stored.ID {
		t.Fatalf("latest revision = %q, want %q", got, stored.ID)
	}
	gotVariables, err := store.GetTemplateRevisionVariables(ctx, recordTenant, stored.ID)
	if err != nil {
		t.Fatalf("GetTemplateRevisionVariables: %v", err)
	}
	if len(gotVariables) != 2 || gotVariables[0].Name != "cidr" || gotVariables[1].Name != "region" {
		t.Fatalf("variables = %+v, want cidr and region", gotVariables)
	}
}

// A second sync that finds a commit already recorded reuses its revision,
// and that is what "up to date" means.
func TestRecordSyncedTemplateRevisionReusesARecordedCommit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	seedSyncableTemplate(t, ctx, store, "template_vpc", "main", "template_sync_1")
	seedRunningSync(t, ctx, store, "template_vpc", "main", "template_sync_2")
	variables := []domain.TemplateVariable{{Name: "region", TypeExpression: "string"}}

	first, err := store.RecordSyncedTemplateRevision(ctx, "template_sync_1", syncedRevision("template_vpc", "main", "abc123"), variables)
	if err != nil {
		t.Fatalf("first RecordSyncedTemplateRevision: %v", err)
	}
	second, err := store.RecordSyncedTemplateRevision(ctx, "template_sync_2", syncedRevision("template_vpc", "main", "abc123"), variables)
	if err != nil {
		t.Fatalf("second RecordSyncedTemplateRevision: %v", err)
	}

	if second.ID != first.ID {
		t.Fatalf("reused revision ID = %q, want %q", second.ID, first.ID)
	}
	want := recordedSync{status: domain.TemplateSyncStateCompleted, revisionID: first.ID, commitSHA: "abc123", upToDate: true}
	if got := readRecordedSync(t, ctx, pool, "template_sync_2"); got != want {
		t.Fatalf("second sync = %+v, want %+v", got, want)
	}
	gotVariables, err := store.GetTemplateRevisionVariables(ctx, recordTenant, first.ID)
	if err != nil {
		t.Fatalf("GetTemplateRevisionVariables: %v", err)
	}
	if len(gotVariables) != 1 {
		t.Fatalf("variables = %+v, want region once", gotVariables)
	}
}

// Issue #598: two templates on one repo and path, on refs that point at the
// same commit, each get a revision of it.
func TestRecordSyncedTemplateRevisionGivesEachTemplateItsOwnRevisionOfASharedCommit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	seedSyncableTemplate(t, ctx, store, "template_main", "main", "template_sync_main")
	seedSyncableTemplate(t, ctx, store, "template_release", "release", "template_sync_release")

	onMain, err := store.RecordSyncedTemplateRevision(ctx, "template_sync_main", syncedRevision("template_main", "main", "abc123"), nil)
	if err != nil {
		t.Fatalf("sync of main: %v", err)
	}
	onRelease, err := store.RecordSyncedTemplateRevision(ctx, "template_sync_release", syncedRevision("template_release", "release", "abc123"), nil)
	if err != nil {
		t.Fatalf("sync of release: %v", err)
	}

	if onMain.ID == onRelease.ID {
		t.Fatalf("both templates got revision %q, want one each", onMain.ID)
	}
	for syncID, revisionID := range map[domain.TemplateSyncID]domain.TemplateRevisionID{
		"template_sync_main":    onMain.ID,
		"template_sync_release": onRelease.ID,
	} {
		want := recordedSync{status: domain.TemplateSyncStateCompleted, revisionID: revisionID, commitSHA: "abc123", upToDate: false}
		if got := readRecordedSync(t, ctx, pool, syncID); got != want {
			t.Fatalf("sync %s = %+v, want %+v", syncID, got, want)
		}
	}
	if got := latestRevisionOf(t, ctx, store, "template_main"); got != onMain.ID {
		t.Fatalf("main's latest = %q, want %q", got, onMain.ID)
	}
	if got := latestRevisionOf(t, ctx, store, "template_release"); got != onRelease.ID {
		t.Fatalf("release's latest = %q, want %q", got, onRelease.ID)
	}
}

// A retried sync activity, whose first attempt committed, gets back what the
// first attempt recorded, even if the ref moved in between, and writes
// nothing: a new revision must not read as up to date on the retry.
func TestRecordSyncedTemplateRevisionRecordsASyncOnce(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	seedSyncableTemplate(t, ctx, store, "template_vpc", "main", "template_sync_1")

	first, err := store.RecordSyncedTemplateRevision(ctx, "template_sync_1", syncedRevision("template_vpc", "main", "aaa111"), nil)
	if err != nil {
		t.Fatalf("first attempt: %v", err)
	}
	retried, err := store.RecordSyncedTemplateRevision(ctx, "template_sync_1", syncedRevision("template_vpc", "main", "bbb222"), nil)
	if err != nil {
		t.Fatalf("retried attempt: %v", err)
	}

	if retried.ID != first.ID {
		t.Fatalf("retried attempt returned %q, want the first attempt's %q", retried.ID, first.ID)
	}
	want := recordedSync{status: domain.TemplateSyncStateCompleted, revisionID: first.ID, commitSHA: "aaa111", upToDate: false}
	if got := readRecordedSync(t, ctx, pool, "template_sync_1"); got != want {
		t.Fatalf("sync = %+v, want %+v", got, want)
	}
	if got := countRevisions(t, ctx, pool); got != 1 {
		t.Fatalf("revisions = %d, want only the first attempt's", got)
	}
	if got := latestRevisionOf(t, ctx, store, "template_vpc"); got != first.ID {
		t.Fatalf("latest revision = %q, want %q", got, first.ID)
	}
}

// A sync of a ref the template no longer tracks still records its revision
// and completes, but the template's latest stays on its current ref.
func TestRecordSyncedTemplateRevisionKeepsTheLatestForTheCurrentRef(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	seedSyncableTemplate(t, ctx, store, "template_vpc", "main", "template_sync_current")
	seedRunningSync(t, ctx, store, "template_vpc", "old-branch", "template_sync_old")

	current, err := store.RecordSyncedTemplateRevision(ctx, "template_sync_current", syncedRevision("template_vpc", "main", "abc123"), nil)
	if err != nil {
		t.Fatalf("sync of main: %v", err)
	}
	old, err := store.RecordSyncedTemplateRevision(ctx, "template_sync_old", syncedRevision("template_vpc", "old-branch", "def456"), nil)
	if err != nil {
		t.Fatalf("sync of old-branch: %v", err)
	}

	want := recordedSync{status: domain.TemplateSyncStateCompleted, revisionID: old.ID, commitSHA: "def456", upToDate: false}
	if got := readRecordedSync(t, ctx, pool, "template_sync_old"); got != want {
		t.Fatalf("old-ref sync = %+v, want %+v", got, want)
	}
	if got := latestRevisionOf(t, ctx, store, "template_vpc"); got != current.ID {
		t.Fatalf("latest revision = %q, want main's %q", got, current.ID)
	}
}

func TestRecordSyncedTemplateRevisionIntoADeletedTemplate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	seedSyncableTemplate(t, ctx, store, "template_vpc", "main", "template_sync_1")
	if err := store.DeleteTemplate(ctx, recordTenant, "template_vpc"); err != nil {
		t.Fatalf("DeleteTemplate: %v", err)
	}

	_, err := store.RecordSyncedTemplateRevision(ctx, "template_sync_1", syncedRevision("template_vpc", "main", "abc123"), nil)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error = %v, want os.ErrNotExist", err)
	}
	if got := countRevisions(t, ctx, pool); got != 0 {
		t.Fatalf("revisions = %d, want none", got)
	}
}

func TestRecordSyncedTemplateRevisionRejectsAnAssignedID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	seedSyncableTemplate(t, ctx, store, "template_vpc", "main", "template_sync_1")

	revision := syncedRevision("template_vpc", "main", "abc123")
	revision.ID = "template_x"
	if _, err := store.RecordSyncedTemplateRevision(ctx, "template_sync_1", revision, nil); err == nil {
		t.Fatal("RecordSyncedTemplateRevision accepted a caller's revision ID")
	}
	if got := countRevisions(t, ctx, pool); got != 0 {
		t.Fatalf("revisions = %d, want none", got)
	}
	if got := readRecordedSync(t, ctx, pool, "template_sync_1"); got.status != domain.TemplateSyncStateRunning {
		t.Fatalf("sync status = %q, want it still running", got.status)
	}
}

// Migration 0050 records "up to date" for syncs that finished before it by
// the rule that used to be worked out on read: the revision predates the
// request. A failed sync is never up to date.
func TestTemplateSyncsUpToDateMigrationBackfillsFromTimestamps(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openTestPool(t, ctx)
	migrateThrough(t, ctx, pool, "0049_security_audit_operation")
	store := NewStore(pool)
	if _, err := store.CreateTemplate(ctx, domain.Template{
		ID: "template_vpc", TenantID: recordTenant, RepoOwner: "acme", RepoName: "infra", SourceRef: "main", RootPath: "modules/vpc",
	}); err != nil {
		t.Fatalf("seed template: %v", err)
	}
	for id, createdAt := range map[domain.TemplateRevisionID]time.Time{
		"template_old": time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC),
		"template_new": time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC),
	} {
		revision := syncedRevision("template_vpc", "main", string(id))
		revision.ID = id
		revision.CreatedAt = createdAt
		tx := mustBegin(t, ctx, pool)
		if _, _, err := insertTemplateRevision(ctx, tx, revision, []byte("[]")); err != nil {
			t.Fatalf("seed revision %s: %v", id, err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit revision %s: %v", id, err)
		}
	}
	for _, sync := range []struct {
		id         domain.TemplateSyncID
		status     domain.TemplateSyncState
		revisionID domain.TemplateRevisionID
	}{
		{"sync_found_nothing_new", domain.TemplateSyncStateCompleted, "template_old"},
		{"sync_found_a_new_commit", domain.TemplateSyncStateCompleted, "template_new"},
		{"sync_failed", domain.TemplateSyncStateFailed, "template_old"},
	} {
		if err := store.CreateTemplateSync(ctx, domain.TemplateSync{
			ID: sync.id, TenantID: recordTenant, TemplateID: "template_vpc", RepoOwner: "acme", RepoName: "infra",
			SourceRef: "main", RootPath: "modules/vpc", Status: sync.status, TemplateRevisionID: sync.revisionID,
			RequestedBy: "user_123", RequestedAt: time.Date(2026, 11, 10, 11, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("seed sync %s: %v", sync.id, err)
		}
	}

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate the rest: %v", err)
	}

	for syncID, want := range map[domain.TemplateSyncID]bool{
		"sync_found_nothing_new":  true,
		"sync_found_a_new_commit": false,
		"sync_failed":             false,
	} {
		if got := readRecordedSync(t, ctx, pool, syncID).upToDate; got != want {
			t.Fatalf("sync %s up_to_date = %v, want %v", syncID, got, want)
		}
	}
}
