package app

import (
	"fmt"

	"github.com/vishu42/openplan/internal/domain"
)

// Issue says what is wrong with one field. The API sends it to clients as a
// stable code, so a value is never renamed or reused.
type Issue string

const (
	IssueRequired          Issue = "required"
	IssueUnknown           Issue = "unknown"
	IssueNull              Issue = "null"
	IssueNotObject         Issue = "not_object"
	IssueInvalidFormat     Issue = "invalid_format"
	IssueInvalid           Issue = "invalid"
	IssueUnsupported       Issue = "unsupported"
	IssueApplyOnly         Issue = "apply_only"
	IssueDifferentTemplate Issue = "different_template"
	IssueNotSignedIn       Issue = "not_signed_in"
	IssueTooLong           Issue = "too_long"
	IssueOutOfRange        Issue = "out_of_range"
	IssueDuplicate         Issue = "duplicate"
)

// FieldError is one problem with one field of a request. It wraps the
// sentinel that names the cause, so errors.Is keeps working, and carries where
// the problem is so a client can put it on the field that caused it.
//
// The app layer says only which field; the API turns that into a JSON Pointer
// or a parameter name, so the wire format stays out of this package.
type FieldError struct {
	// Path locates a body field, one element per level: {"config", "region"}.
	Path []string
	// Parameter names a query parameter instead, when Path is empty.
	Parameter string
	Issue     Issue
	// Message is one sentence for a person, naming the field.
	Message string
	Err     error
}

// Error keeps the sentinel's text in front, as fmt.Errorf("%w: ...") did, so
// log lines read the same as before field errors existed.
func (e *FieldError) Error() string { return e.Err.Error() + ": " + e.Message }

func (e *FieldError) Unwrap() error { return e.Err }

// fieldError is a body-field problem. Validators join several with
// errors.Join, so one response can report everything wrong with a request.
func fieldError(sentinel error, issue Issue, message string, path ...string) error {
	return &FieldError{Path: path, Issue: issue, Message: message, Err: sentinel}
}

// parameterError is a query-parameter problem, the counterpart of fieldError.
func parameterError(sentinel error, issue Issue, message, parameter string) error {
	return &FieldError{Parameter: parameter, Issue: issue, Message: message, Err: sentinel}
}

// FieldErrors returns every field error in err's tree, in the order they were
// joined. errors.As would stop at the first.
func FieldErrors(err error) []*FieldError {
	var found []*FieldError
	var walk func(error)
	walk = func(err error) {
		if fieldErr, ok := err.(*FieldError); ok { //nolint:errorlint // walking the tree by hand is the point
			found = append(found, fieldErr)
			return
		}
		switch wrapped := err.(type) { //nolint:errorlint // as above
		case interface{ Unwrap() []error }:
			for _, inner := range wrapped.Unwrap() {
				walk(inner)
			}
		case interface{ Unwrap() error }:
			walk(wrapped.Unwrap())
		}
	}
	if err != nil {
		walk(err)
	}
	return found
}

// RunInFlightError names the unfinished run that blocks a change, so a
// person can be told which run to wait for or discard. It wraps
// ErrTemplateRunInFlight, which stays the cause to match on.
type RunInFlightError struct {
	RunNumber int
	Status    domain.TemplateRunStatus
}

func (e *RunInFlightError) Error() string {
	return fmt.Sprintf("%v: run #%d is %s", ErrTemplateRunInFlight, e.RunNumber, e.Status)
}

func (e *RunInFlightError) Unwrap() error { return ErrTemplateRunInFlight }

// PlanStaleError names the run whose plan no longer describes the template's
// desired state. It wraps ErrStackTemplatePlanStale.
type PlanStaleError struct {
	RunNumber int
	PlanState domain.PlanState
}

func (e *PlanStaleError) Error() string {
	return fmt.Sprintf("%v: run #%d, plan state is %q", ErrStackTemplatePlanStale, e.RunNumber, e.PlanState)
}

func (e *PlanStaleError) Unwrap() error { return ErrStackTemplatePlanStale }

// Sentinels lists every exported error of this package. The API's test
// checks that each has a problem type, so a new sentinel can't reach a client
// as an unexplained 500; a test here checks the list is complete.
func Sentinels() []error {
	return []error{
		ErrUnauthenticated,
		ErrInvalidCommand,
		ErrInvalidQuery,
		ErrInvalidQuery,
		ErrDuplicateStackSlug,
		ErrDuplicateStackTemplateComponentKey,
		ErrNotFound,
		ErrRevisionNotInstallable,
		ErrStackTemplateConfigInvalid,
		ErrStackTemplateRevisionNotChangeable,
		ErrStackTemplateNotEditable,
		ErrStackTemplateNotRunnable,
		ErrStackTemplatePlanStale,
		ErrTemplateRunInFlight,
		ErrRunNotApprovable,
		ErrRunNotDiscardable,
		ErrLastOwner,
		ErrCredentialNameInvalid,
		ErrDuplicateCredentialName,
		ErrCredentialEncryptionUnavailable,
		ErrTemplateInUse,
		ErrTemplateRunLogLive,
		ErrTemplateRunLogLost,
		ErrForbidden,
		ErrUserNotProvisioned,
	}
}
