package domain

// The template catalogue: sources, revisions, syncs, variables.

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// TemplateRevisionStatus identifies template revision validation state.
type TemplateRevisionStatus string

const (
	TemplateRevisionPendingValidation TemplateRevisionStatus = "pending_validation"
	TemplateRevisionValidating        TemplateRevisionStatus = "validating"
	TemplateRevisionActive            TemplateRevisionStatus = "active"
	TemplateRevisionInvalid           TemplateRevisionStatus = "invalid"
)

// Valid reports whether the status is one of the supported template revision states.
func (status TemplateRevisionStatus) Valid() bool {
	switch status {
	case TemplateRevisionPendingValidation, TemplateRevisionValidating, TemplateRevisionActive, TemplateRevisionInvalid:
		return true
	default:
		return false
	}
}

// TemplateSyncState is a sync's stored lifecycle. It is not what the sync
// shows: TemplateSyncStatus is derived from it (see LatestTemplateSync.StatusAt).
type TemplateSyncState string

const (
	TemplateSyncStatePending   TemplateSyncState = "pending"
	TemplateSyncStateRunning   TemplateSyncState = "running"
	TemplateSyncStateCompleted TemplateSyncState = "completed"
	TemplateSyncStateInvalid   TemplateSyncState = "invalid"
	TemplateSyncStateFailed    TemplateSyncState = "failed"
)

// Valid reports whether the status is one of the supported sync states.
func (status TemplateSyncState) Valid() bool {
	switch status {
	case TemplateSyncStatePending,
		TemplateSyncStateRunning,
		TemplateSyncStateCompleted,
		TemplateSyncStateInvalid,
		TemplateSyncStateFailed:
		return true
	default:
		return false
	}
}

// TemplateSyncStep is what a running sync is doing right now, for the
// people watching it. It is not a status: nothing branches on it. It is
// recorded when a step starts and kept when the sync ends, so a failed sync
// still says where it failed. The zero value is a sync that has not started a
// step.
type TemplateSyncStep string

const (
	// TemplateSyncStepSyncing is cloning the repository, parsing the
	// template it holds and saving the revision it describes.
	TemplateSyncStepSyncing TemplateSyncStep = "syncing"
)

// AllTemplateSyncSteps is every step a sync may record, in the order a
// sync takes them. The step check constraint in the migrations lists the same
// values.
var AllTemplateSyncSteps = []TemplateSyncStep{
	TemplateSyncStepSyncing,
}

// Valid reports whether the step is one a sync may record. The zero value is
// not: it is what a sync has before it records one.
func (step TemplateSyncStep) Valid() bool {
	return slices.Contains(AllTemplateSyncSteps, step)
}

// SourceTemplate is the stable logical identity tracked by template revisions.
// It exists from registration on, before any sync has produced a revision.
type SourceTemplate struct {
	ID        SourceTemplateID `json:"id"`
	TenantID  TenantID         `json:"tenant_id"`
	RepoOwner string           `json:"repo_owner"`
	RepoName  string           `json:"repo_name"`
	SourceRef string           `json:"source_ref"`
	RootPath  string           `json:"root_path"`
	// Name, Description and Tags are the latest revision's. Before the first
	// revision, Name is FallbackTemplateName, Description is empty and Tags
	// is empty.
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	// LatestTemplateRevisionID is empty until a sync records a revision.
	LatestTemplateRevisionID TemplateRevisionID `json:"latest_template_revision_id"`
	CreatedAt                time.Time          `json:"created_at"`
	UpdatedAt                time.Time          `json:"updated_at"`
	// LatestSync is the newest sync requested for the template, nil until one
	// is. Not serialised as is: its stored status is not what a reader is
	// shown (see LatestTemplateSync.StatusAt).
	LatestSync *LatestTemplateSync `json:"-"`
}

// TemplateSyncStatus is how a template's latest sync reads to the people
// looking at the template, derived from the stored TemplateSyncState.
type TemplateSyncStatus string

const (
	TemplateSyncSyncing  TemplateSyncStatus = "syncing"
	TemplateSyncStalled  TemplateSyncStatus = "stalled"
	TemplateSyncUpToDate TemplateSyncStatus = "up_to_date"
	TemplateSyncSynced   TemplateSyncStatus = "synced"
	TemplateSyncInvalid  TemplateSyncStatus = "invalid"
	TemplateSyncFailed   TemplateSyncStatus = "failed"
)

// TemplateSyncStallsAfter is how long a sync may stay pending or running
// before it reads as stalled. Its worst-case retry chain is about 24 minutes
// (4 attempts of up to 5 minutes, with backoff doubling from 30 seconds to 5
// minutes) plus queue delay, so a sync still unfinished after 30 minutes is
// not coming back on its own.
const TemplateSyncStallsAfter = 30 * time.Minute

// LatestTemplateSync is the newest sync of a template, as stored.
type LatestTemplateSync struct {
	Status            TemplateSyncState
	ErrorSummary      string
	ResolvedCommitSHA string
	RequestedAt       time.Time
	// CompletedAt is zero until the sync ends.
	CompletedAt time.Time
	// RevisionPredatesRequest is true when the revision the sync resolved to
	// was created before the sync was requested: the commit was already
	// registered, so the sync found nothing new. The two times come from the
	// api's and the executor's clocks, so skew between them larger than a
	// sync's duration would misread it.
	RevisionPredatesRequest bool
}

// StatusAt is the sync's status as shown at now. Stalled is derived here,
// never stored: the row stays pending or running.
func (sync LatestTemplateSync) StatusAt(now time.Time) TemplateSyncStatus {
	switch sync.Status {
	case TemplateSyncStatePending, TemplateSyncStateRunning:
		if now.Sub(sync.RequestedAt) >= TemplateSyncStallsAfter {
			return TemplateSyncStalled
		}
		return TemplateSyncSyncing
	case TemplateSyncStateCompleted:
		if sync.RevisionPredatesRequest {
			return TemplateSyncUpToDate
		}
		return TemplateSyncSynced
	case TemplateSyncStateInvalid:
		return TemplateSyncInvalid
	case TemplateSyncStateFailed:
		return TemplateSyncFailed
	}
	// The status check constraint admits no other status; were one stored,
	// failed is the reading that asks for another sync.
	return TemplateSyncFailed
}

// SourceTemplateIDFor derives the id of the template a source names, so
// registering the same source twice lands on the same template. rootPath is
// the cleaned root path. The hash is over the values joined by NUL, which no
// value may contain, so two different sources never join to the same input.
func SourceTemplateIDFor(tenantID TenantID, repoOwner, repoName, rootPath, sourceRef string) SourceTemplateID {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		string(tenantID),
		repoOwner,
		repoName,
		rootPath,
		sourceRef,
	}, "\x00")))
	return SourceTemplateID("source_template_" + hex.EncodeToString(sum[:16]))
}

// FallbackTemplateName names a template whose source does not name itself:
// the last element of its root path, or the repository name for a template
// at the repository root.
func FallbackTemplateName(repoName string, rootPath string) string {
	base := filepath.Base(filepath.Clean(rootPath))
	if base == "." || base == string(filepath.Separator) || base == "" {
		return repoName
	}
	return base
}

// TemplateRevision is one resolved GitHub-sourced Terraform template revision.
type TemplateRevision struct {
	ID                TemplateRevisionID     `json:"id"`
	TenantID          TenantID               `json:"tenant_id"`
	SourceTemplateID  SourceTemplateID       `json:"source_template_id"`
	RepoOwner         string                 `json:"repo_owner"`
	RepoName          string                 `json:"repo_name"`
	SourceRef         string                 `json:"source_ref"`
	ResolvedCommitSHA string                 `json:"resolved_commit_sha"`
	RootPath          string                 `json:"root_path"`
	Name              string                 `json:"name"`
	Description       string                 `json:"description"`
	Tags              []string               `json:"tags"`
	Status            TemplateRevisionStatus `json:"status"`
	CreatedAt         time.Time              `json:"created_at"`
}

// TemplateSync is one sync of a template: cloning its repository at its ref
// and recording the revision it describes.
type TemplateSync struct {
	ID       TemplateSyncID `json:"id"`
	TenantID TenantID       `json:"tenant_id"`
	// SourceTemplateID is the template synced. It is empty on
	// syncs made before templates were created at registration.
	SourceTemplateID   SourceTemplateID   `json:"source_template_id"`
	RepoOwner          string             `json:"repo_owner"`
	RepoName           string             `json:"repo_name"`
	SourceRef          string             `json:"source_ref"`
	RootPath           string             `json:"root_path"`
	Status             TemplateSyncState  `json:"status"`
	Step               TemplateSyncStep   `json:"step"`
	TemplateRevisionID TemplateRevisionID `json:"template_revision_id"`
	ResolvedCommitSHA  string             `json:"resolved_commit_sha"`
	RequestedBy        UserID             `json:"requested_by"`
	RequestedAt        time.Time          `json:"requested_at"`
	// CompletedAt is omitted from JSON until the sync ends.
	CompletedAt  time.Time `json:"completed_at,omitzero"`
	ErrorSummary string    `json:"error_summary"`
}

// TemplateVariable is inferred from Terraform root module variables.
type TemplateVariable struct {
	TemplateRevisionID TemplateRevisionID `json:"template_revision_id"`
	Name               string             `json:"name"`
	TypeExpression     string             `json:"type_expression"`
	Description        string             `json:"description"`
	Required           bool               `json:"required"`
	HasDefault         bool               `json:"has_default"`
	Sensitive          bool               `json:"sensitive"`
	HasValidation      bool               `json:"has_validation"`
}
