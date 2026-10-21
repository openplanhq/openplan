package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/vishu42/openplan/internal/app"
	"github.com/vishu42/openplan/internal/domain"
)

// Every error the API writes is an RFC 9457 problem document. Clients branch
// on type, which is why types are never renamed or reused; title is fixed per
// type, and detail is one sentence about this occurrence that is never parsed.
const problemContentType = "application/problem+json"

// problemTypeBase prefixes every type. The URIs needn't resolve: openplan
// runs in egress-restricted networks, and nothing fetches them.
const problemTypeBase = "https://openplan.run/problems/"

// blankType is RFC 9457's type for a status that means all there is to say.
const blankType = "about:blank"

type problemDocument struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
	// RequestID matches the response's X-Request-Id and the API's log lines.
	RequestID string              `json:"request_id,omitempty"`
	Errors    []problemFieldError `json:"errors,omitempty"`
}

// problemFieldError is one entry of the errors extension, shaped like RFC
// 9457's own example: where the problem is, a stable issue code, and a
// sentence for a person.
type problemFieldError struct {
	Pointer   string    `json:"pointer,omitempty"`
	Parameter string    `json:"parameter,omitempty"`
	Issue     app.Issue `json:"issue"`
	Detail    string    `json:"detail"`
}

func problemTypeURI(slug string) string { return problemTypeBase + slug }

// blankProblem is a problem whose status says it all; RFC 9457 gives it the
// status's reason phrase as title.
func blankProblem(status int, detail string) problemDocument {
	return problemDocument{Type: blankType, Title: http.StatusText(status), Status: status, Detail: detail}
}

// writeProblem is the one writer every API error goes through.
// internal/authentication spells its 401 out separately; a test keeps the two
// identical.
func writeProblem(response http.ResponseWriter, problem problemDocument) {
	problem.RequestID = requestIDOf(response)
	response.Header().Set("Content-Type", problemContentType)
	response.WriteHeader(problem.Status)
	// The status is already sent, so a failed encode has no one left to tell.
	_ = json.NewEncoder(response).Encode(problem) //nolint:errchkjson // see above
}

// problemType is one row of the table writeAppError walks: what a sentinel
// becomes on the wire. An empty slug means about:blank.
type problemType struct {
	sentinel error
	status   int
	slug     string
	title    string
	detail   string
	// field is set on the duplicate types: the one body field the clash is
	// about, reported as an errors entry so a form marks it like a 422.
	field string
	// occurrence, when set, words detail from data the error carries about
	// this occurrence, such as which run is in the way. It reports false when
	// the error carries none, and detail stands.
	occurrence func(err error) (string, bool)
}

// problemTypes maps every app sentinel to its problem. Rows are tried in
// order, so a specific sentinel must come before a broad one it might be
// joined with. A test fails when an app sentinel has no row.
var problemTypes = []problemType{
	{sentinel: app.ErrUnauthenticated, status: http.StatusUnauthorized, detail: "Sign in to continue."},
	{sentinel: app.ErrForbidden, status: http.StatusForbidden, detail: "You don't have permission to do this."},
	{sentinel: app.ErrNotFound, status: http.StatusNotFound, detail: "The resource doesn't exist, or you can't see it."},

	{
		sentinel: app.ErrTemplateRunInFlight, status: http.StatusConflict, slug: "run-in-flight",
		title:      "Another run is in progress",
		detail:     "Another run on this template is still in progress. Wait for it to finish or discard it.",
		occurrence: runInFlightDetail,
	},
	{
		sentinel: app.ErrStackTemplatePlanStale, status: http.StatusConflict, slug: "plan-stale",
		title:      "The plan is out of date",
		detail:     "The template changed after this plan was made. Plan again, then apply.",
		occurrence: planStaleDetail,
	},
	{
		sentinel: app.ErrRunNotApprovable, status: http.StatusConflict, slug: "run-not-approvable",
		title:  "The run can't be approved",
		detail: "The run isn't waiting for approval any more. Someone may have acted on it first.",
	},
	{
		sentinel: app.ErrRunNotDiscardable, status: http.StatusConflict, slug: "run-not-discardable",
		title:  "The run can't be discarded",
		detail: "The run has no plan waiting to discard.",
	},
	{
		sentinel: app.ErrStackTemplateNotRunnable, status: http.StatusConflict, slug: "template-not-runnable",
		title:  "The template can't run",
		detail: "The template is being torn down, or has no revision to run.",
	},
	{
		sentinel: app.ErrStackTemplateNotEditable, status: http.StatusConflict, slug: "template-not-editable",
		title:  "The template can't be edited",
		detail: "The template is being torn down, so its configuration can't change.",
	},
	{
		sentinel: app.ErrStackTemplateUpgradeInvalid, status: http.StatusConflict, slug: "template-not-upgradable",
		title:  "The template can't change revision",
		detail: "The template is being torn down, so it can't move to another revision.",
	},
	{
		sentinel: app.ErrTemplateNotInstallable, status: http.StatusConflict, slug: "revision-not-installable",
		title:  "The revision can't be installed",
		detail: "The revision is still syncing, or its sync failed. Wait for it, or pick another revision.",
	},
	{
		sentinel: app.ErrTemplateInUse, status: http.StatusConflict, slug: "template-in-use",
		title:  "The template is in use",
		detail: "A stack has installed this template, so it can't be deleted.",
	},
	{
		sentinel: app.ErrDuplicateStackSlug, status: http.StatusConflict, slug: "duplicate-slug",
		title:  "The slug is taken",
		detail: "Another stack already uses this slug.", field: "slug",
	},
	{
		sentinel: app.ErrDuplicateStackTemplateComponentKey, status: http.StatusConflict, slug: "duplicate-component-key",
		title:  "The component key is taken",
		detail: "This stack already has a template with this component key.", field: "component_key",
	},
	{
		sentinel: app.ErrDuplicateCredentialName, status: http.StatusConflict, slug: "duplicate-credential-name",
		title:  "The credential name is taken",
		detail: "A credential with this name already exists here.", field: "name",
	},
	{
		sentinel: app.ErrLastOwner, status: http.StatusConflict, slug: "last-owner",
		title:  "The stack needs an owner",
		detail: "This is the stack's last owner. Make someone else owner first.",
	},
	{
		sentinel: app.ErrTemplateRunLogLive, status: http.StatusConflict, slug: "log-live",
		title:  "The log is still being written",
		detail: "This command is still running. Follow it live, or try again when it finishes.",
	},
	// 404, not 409: no wait brings the body back, but its own type lets the
	// client say the output was lost rather than that the log doesn't exist.
	{
		sentinel: app.ErrTemplateRunLogLost, status: http.StatusNotFound, slug: "log-lost",
		title:  "The log was lost",
		detail: "This command's output was not stored.",
	},

	// These three always arrive as field errors, which writeAppError renders
	// before reaching the table; the rows cover one returned bare.
	{sentinel: app.ErrStackTemplateConfigInvalid, status: http.StatusUnprocessableEntity, slug: validationFailedSlug, title: validationFailedTitle, detail: "The template's configuration is invalid."},
	{sentinel: app.ErrCredentialNameInvalid, status: http.StatusUnprocessableEntity, slug: validationFailedSlug, title: validationFailedTitle, detail: "The credential name is invalid."},
	{sentinel: app.ErrUserNotProvisioned, status: http.StatusUnprocessableEntity, slug: validationFailedSlug, title: validationFailedTitle, detail: "That person hasn't signed in to openplan yet."},

	{
		sentinel: app.ErrCredentialEncryptionUnavailable, status: http.StatusServiceUnavailable, slug: "credential-encryption-unavailable",
		title:  "Credential encryption isn't configured",
		detail: "No credential encryption key is configured. An operator has to set one.",
	},

	// Always arrives as a parameter error (a cursor this API never gave out);
	// the row covers one returned bare.
	{
		sentinel: app.ErrInvalidQuery, status: http.StatusBadRequest, slug: invalidQuerySlug,
		title: invalidQueryTitle, detail: "A query parameter can't be read.",
	},

	// Last: the specific sentinels above may be joined with it. Left here, it
	// is a check the router or handler makes unreachable over HTTP, such as a
	// missing path id, so there is no field to point at.
	{sentinel: app.ErrInvalidCommand, status: http.StatusBadRequest, detail: "The request is invalid."},
}

const (
	validationFailedSlug  = "validation-failed"
	validationFailedTitle = "The request has invalid fields"
	invalidQuerySlug      = "invalid-query"
	invalidQueryTitle     = "The query string can't be read"
)

// writeAppError renders an error from the app layer. Field errors come first,
// whatever sentinel each wraps: one response then carries every problem with
// the request, and a sentinel such as ErrStackTemplateUpgradeInvalid can be
// a field problem in one case and a state conflict in another. detail never
// comes from err.Error(), which is a Go error chain naming call sites.
func writeAppError(response http.ResponseWriter, err error) {
	if fieldErrs := app.FieldErrors(err); len(fieldErrs) > 0 {
		// A field error joined with a failure that is neither a field error
		// nor a known cause, such as a database error, is the server's
		// failure: answering 422 would blame the request and drop the cause
		// from the log. Matching the table would do the same through the
		// field errors' own sentinels, so it is the logged 500.
		if !onlyKnownCauses(err) {
			writeInternalError(response, err)
			return
		}
		writeProblem(response, fieldProblem(err, fieldErrs))
		return
	}
	for _, row := range problemTypes {
		if errors.Is(err, row.sentinel) {
			writeProblem(response, row.problem(err))
			return
		}
	}
	writeInternalError(response, err)
}

// writeInternalError is the 500 for anything unmapped. Authorization failures
// land here: a denial never does, since it is (false, nil) at the boundary and
// becomes ErrForbidden, so a failure can never render as 403.
//
// The body says nothing about the cause, so the log is the only place it
// survives; the request id is how an operator finds this line.
func writeInternalError(response http.ResponseWriter, err error) {
	log.Printf("api: internal error request_id=%s: %v", requestIDOf(response), err)
	writeProblem(response, blankProblem(http.StatusInternalServerError,
		"Something went wrong on the server. Quote the request ID when you report it."))
}

// fieldProblem renders field errors. A clash with existing data (the
// duplicate types) stays its 409, carrying the field the app says caused it,
// so a slug derived from the name points at the name. A query parameter that
// can't be read at all is the 400, even beside one that breaks a rule: the
// request has to be rewritten before its rules mean anything. Anything else is
// the 422 for content that breaks a rule.
func fieldProblem(err error, fieldErrs []*app.FieldError) problemDocument {
	if errors.Is(err, app.ErrInvalidQuery) {
		return invalidQueryProblem(problemFieldErrors(fieldErrs))
	}
	for _, row := range problemTypes {
		if row.field != "" && errors.Is(err, row.sentinel) {
			problem := row.problem(err)
			problem.Errors = problemFieldErrors(fieldErrs)
			if len(fieldErrs) == 1 {
				problem.Detail = fieldErrs[0].Message
			}
			return problem
		}
	}
	return validationProblem(fieldErrs)
}

// onlyKnownCauses reports whether every leaf of err's tree is a field error
// or an app sentinel, so nothing in it is a failure the response would hide.
// Wrapping (fmt.Errorf with %w, the app's own error types) is looked through.
func onlyKnownCauses(err error) bool {
	if _, ok := err.(*app.FieldError); ok { //nolint:errorlint // a field error is a leaf here; its sentinel is known
		return true
	}
	switch wrapped := err.(type) { //nolint:errorlint // walking the tree by hand is the point
	case interface{ Unwrap() []error }:
		for _, inner := range wrapped.Unwrap() {
			if !onlyKnownCauses(inner) {
				return false
			}
		}
		return true
	case interface{ Unwrap() error }:
		if inner := wrapped.Unwrap(); inner != nil {
			return onlyKnownCauses(inner)
		}
	}
	return slices.Contains(app.Sentinels(), err)
}

func (row problemType) problem(err error) problemDocument {
	detail := row.detail
	if row.occurrence != nil {
		if said, ok := row.occurrence(err); ok {
			detail = said
		}
	}
	if row.slug == "" {
		return blankProblem(row.status, detail)
	}
	problem := problemDocument{Type: problemTypeURI(row.slug), Title: row.title, Status: row.status, Detail: detail}
	if row.field != "" {
		problem.Errors = []problemFieldError{{
			Pointer: jsonPointer([]string{row.field}), Issue: app.IssueDuplicate, Detail: row.detail,
		}}
	}
	return problem
}

// runInFlightDetail names the run in the way and what to do about it, from
// what the app knows about it; the spec's own example is "Run #4 is still
// planning. Wait for it to finish or discard it."
func runInFlightDetail(err error) (string, bool) {
	inFlight, ok := errors.AsType[*app.RunInFlightError](err)
	if !ok {
		return "", false
	}
	run := fmt.Sprintf("Run #%d", inFlight.RunNumber)
	switch inFlight.Status {
	case domain.TemplateRunQueued:
		return run + " is queued. Wait for it to finish or discard it.", true
	case domain.TemplateRunRunning:
		return run + " is still running. Wait for it to finish or discard it.", true
	case domain.TemplateRunWaitingApproval:
		return run + " has a plan waiting for approval. Approve or discard it first.", true
	case domain.TemplateRunApproved:
		return run + " is applying. Wait for it to finish.", true
	// A finished run is in nobody's way, so there is nothing true to say
	// about it; the table's sentence stands.
	case domain.TemplateRunCompleted, domain.TemplateRunFailed, domain.TemplateRunCanceled:
		return "", false
	default:
		return run + " hasn't finished. Wait for it to finish or discard it.", true
	}
}

func planStaleDetail(err error) (string, bool) {
	stale, ok := errors.AsType[*app.PlanStaleError](err)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("Run #%d's plan no longer matches the template's configuration or revision. Plan again, then apply.", stale.RunNumber), true
}

// invalidQueryProblem is the 400 for query parameters that can't be read.
func invalidQueryProblem(entries []problemFieldError) problemDocument {
	problem := problemDocument{
		Type: problemTypeURI(invalidQuerySlug), Title: invalidQueryTitle,
		Status: http.StatusBadRequest, Detail: "A query parameter can't be read.", Errors: entries,
	}
	if len(entries) == 1 {
		problem.Detail = entries[0].Detail
	}
	return problem
}

// validationProblem is the 422 for content that parses but breaks a rule.
func validationProblem(fieldErrs []*app.FieldError) problemDocument {
	problem := problemDocument{
		Type:   problemTypeURI(validationFailedSlug),
		Title:  validationFailedTitle,
		Status: http.StatusUnprocessableEntity,
		Errors: problemFieldErrors(fieldErrs),
	}
	// One problem is said as itself. Its message starts with the field's
	// name, which is case-sensitive, so it isn't capitalized.
	if len(fieldErrs) == 1 {
		problem.Detail = fieldErrs[0].Message
	} else {
		problem.Detail = fmt.Sprintf("%d fields need attention.", len(fieldErrs))
	}
	return problem
}

func problemFieldErrors(fieldErrs []*app.FieldError) []problemFieldError {
	entries := make([]problemFieldError, 0, len(fieldErrs))
	for _, fieldErr := range fieldErrs {
		entry := problemFieldError{Parameter: fieldErr.Parameter, Issue: fieldErr.Issue, Detail: fieldErr.Message}
		if fieldErr.Parameter == "" {
			entry.Pointer = jsonPointer(fieldErr.Path)
		}
		entries = append(entries, entry)
	}
	return entries
}

// jsonPointer renders a body path as a JSON Pointer (RFC 6901) in URI
// fragment form, as RFC 9457's example does. Variable names come from the
// caller, so a "/" or "~" in one must be escaped or the pointer would name a
// different field, and anything a fragment can't hold is percent-encoded.
func jsonPointer(path []string) string {
	var pointer strings.Builder
	for _, segment := range path {
		pointer.WriteString("/")
		pointer.WriteString(strings.ReplaceAll(strings.ReplaceAll(segment, "~", "~0"), "/", "~1"))
	}
	return "#" + (&url.URL{Fragment: pointer.String()}).EscapedFragment()
}
