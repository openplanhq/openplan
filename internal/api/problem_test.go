package api

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/vishu42/openplan/internal/app"
	"github.com/vishu42/openplan/internal/domain"
)

// An app sentinel with no row reaches a client as an unexplained 500, the bug
// PR #381 fixed by hand. This makes the next one fail here instead.
func TestEverySentinelHasAProblemType(t *testing.T) {
	t.Parallel()

	for _, sentinel := range app.Sentinels() {
		found := false
		for _, row := range problemTypes {
			if errors.Is(row.sentinel, sentinel) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("app sentinel %q has no row in problemTypes", sentinel)
		}
	}
}

func TestProblemTypesAreUniqueAndKebabCase(t *testing.T) {
	t.Parallel()

	kebab := regexp.MustCompile(`^[a-z]+(-[a-z]+)*$`)
	seen := map[string]error{}
	for _, row := range problemTypes {
		if row.slug == "" {
			if row.title != "" {
				t.Errorf("about:blank row for %q has a title; it must be the status text", row.sentinel)
			}
			continue
		}
		if !kebab.MatchString(row.slug) {
			t.Errorf("slug %q is not kebab-case", row.slug)
		}
		// validation-failed is the one type several sentinels share.
		if other, ok := seen[row.slug]; ok && row.slug != validationFailedSlug {
			t.Errorf("slug %q is used by %q and %q", row.slug, other, row.sentinel)
		}
		seen[row.slug] = row.sentinel
		if row.title == "" || row.detail == "" {
			t.Errorf("slug %q needs a title and a detail", row.slug)
		}
	}
}

func renderAppError(t *testing.T, err error) (*httptest.ResponseRecorder, problemDocument) {
	t.Helper()
	response := httptest.NewRecorder()
	response.Header().Set(RequestIDHeader, "request-1")
	writeAppError(response, err)
	return response, decodeProblem(t, response)
}

func TestValidationFailedCarriesEveryFieldError(t *testing.T) {
	t.Parallel()

	err := errors.Join(
		&app.FieldError{Path: []string{"config", "a"}, Issue: app.IssueRequired, Message: "a is required", Err: app.ErrStackTemplateConfigInvalid},
		&app.FieldError{Path: []string{"config", "b"}, Issue: app.IssueUnknown, Message: "b is not a variable of this template", Err: app.ErrStackTemplateConfigInvalid},
	)
	response, problem := renderAppError(t, err)

	if response.Code != http.StatusUnprocessableEntity || problem.Status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body status = %d, want 422", response.Code, problem.Status)
	}
	if problem.Type != "https://openplan.run/problems/validation-failed" || problem.Detail != "2 fields need attention." {
		t.Fatalf("problem = %+v", problem)
	}
	want := []problemFieldError{
		{Pointer: "#/config/a", Issue: app.IssueRequired, Detail: "a is required"},
		{Pointer: "#/config/b", Issue: app.IssueUnknown, Detail: "b is not a variable of this template"},
	}
	if fmt.Sprint(problem.Errors) != fmt.Sprint(want) {
		t.Fatalf("errors = %+v, want %+v", problem.Errors, want)
	}

	_, single := renderAppError(t, app.FieldErrors(err)[0])
	if single.Detail != "a is required" {
		t.Fatalf("single-error detail = %q, want the error's own message", single.Detail)
	}
}

// A variable name is the caller's, so it can hold anything; the pointer must
// still name that one variable.
func TestJSONPointerEscapes(t *testing.T) {
	t.Parallel()

	if got, want := jsonPointer([]string{"config", "a/b~c d"}), "#/config/a~1b~0c%20d"; got != want {
		t.Fatalf("jsonPointer = %q, want %q", got, want)
	}
}

func TestWrappedFieldErrorIsNotA500(t *testing.T) {
	t.Parallel()

	fieldErr := &app.FieldError{Path: []string{"name"}, Issue: app.IssueRequired, Message: "name is required", Err: app.ErrInvalidCommand}
	err := errors.Join(fmt.Errorf("create stack: %w", fieldErr), fmt.Errorf("validate: %w", app.ErrInvalidCommand))
	response, problem := renderAppError(t, err)
	if response.Code != http.StatusUnprocessableEntity || len(problem.Errors) != 1 || problem.Errors[0].Pointer != "#/name" {
		t.Fatalf("status = %d, problem = %+v, want a 422 on #/name", response.Code, problem)
	}
}

// detail is for people and stays put; the Go error chain names call sites
// and changes whenever a wrap does.
func TestDetailNeverLeaksTheErrorChain(t *testing.T) {
	t.Parallel()

	for _, row := range problemTypes {
		response, problem := renderAppError(t, fmt.Errorf("handler call site: %w", row.sentinel))
		if strings.Contains(response.Body.String(), "handler call site") {
			t.Errorf("%q: body leaks the error: %s", row.sentinel, response.Body.String())
		}
		if response.Code != row.status || problem.Status != row.status {
			t.Errorf("%q: status = %d, body %d, want %d", row.sentinel, response.Code, problem.Status, row.status)
		}
		wantType := blankType
		if row.slug != "" {
			wantType = problemTypeURI(row.slug)
		}
		if problem.Type != wantType {
			t.Errorf("%q: type = %q, want %q", row.sentinel, problem.Type, wantType)
		}
	}
}

func TestUnmappedErrorIsAGeneric500(t *testing.T) {
	t.Parallel()

	response, problem := renderAppError(t, errors.New("pq: connection refused"))
	if response.Code != http.StatusInternalServerError || problem.Type != blankType || problem.Title != "Internal Server Error" {
		t.Fatalf("status = %d, problem = %+v", response.Code, problem)
	}
	if problem.RequestID != "request-1" || strings.Contains(response.Body.String(), "connection refused") {
		t.Fatalf("problem = %+v, want the request id and no cause", problem)
	}
}

func TestDuplicateCarriesItsField(t *testing.T) {
	t.Parallel()

	response, problem := renderAppError(t, fmt.Errorf("insert stack: %w", app.ErrDuplicateStackSlug))
	if response.Code != http.StatusConflict || problem.Type != "https://openplan.run/problems/duplicate-slug" {
		t.Fatalf("status = %d, problem = %+v", response.Code, problem)
	}
	if len(problem.Errors) != 1 || problem.Errors[0].Pointer != "#/slug" || problem.Errors[0].Issue != app.IssueDuplicate {
		t.Fatalf("errors = %+v, want one duplicate on #/slug", problem.Errors)
	}
}

// A field error wins over the sentinel it wraps: the same sentinel is a 409
// for a template being torn down but a 422 for a target from another
// template, which the caller fixes in the request.
func TestFieldErrorWinsOverItsSentinelsRow(t *testing.T) {
	t.Parallel()

	_, conflict := renderAppError(t, fmt.Errorf("%w: lifecycle is %q", app.ErrStackTemplateUpgradeInvalid, "destroying"))
	if conflict.Status != http.StatusConflict || conflict.Type != problemTypeURI("template-not-upgradable") {
		t.Fatalf("lifecycle case = %+v, want 409 template-not-upgradable", conflict)
	}
	_, invalid := renderAppError(t, &app.FieldError{
		Path: []string{"target_template_revision_id"}, Issue: app.IssueDifferentTemplate,
		Message: "target_template_revision_id is a revision of a different template", Err: app.ErrStackTemplateUpgradeInvalid,
	})
	if invalid.Status != http.StatusUnprocessableEntity || invalid.Errors[0].Issue != app.IssueDifferentTemplate {
		t.Fatalf("mismatch case = %+v, want 422 different_template", invalid)
	}
}

// The detail names the run in the way, as the spec's example does; the
// error's own text never reaches the body.
func TestRunInFlightDetailNamesTheRun(t *testing.T) {
	t.Parallel()

	for status, want := range map[domain.TemplateRunStatus]string{
		domain.TemplateRunRunning:         "Run #4 is still running. Wait for it to finish or discard it.",
		domain.TemplateRunWaitingApproval: "Run #4 has a plan waiting for approval. Approve or discard it first.",
		domain.TemplateRunQueued:          "Run #4 is queued. Wait for it to finish or discard it.",
		domain.TemplateRunApproved:        "Run #4 is applying. Wait for it to finish.",
	} {
		err := fmt.Errorf("update stack template config: %w", &app.RunInFlightError{RunNumber: 4, Status: status})
		response, problem := renderAppError(t, err)
		if response.Code != http.StatusConflict || problem.Type != problemTypeURI("run-in-flight") || problem.Detail != want {
			t.Errorf("%s: status = %d, problem = %+v, want detail %q", status, response.Code, problem, want)
		}
	}

	// The store's bare refusal has no run to name; the table's detail stands.
	if _, problem := renderAppError(t, app.ErrTemplateRunInFlight); problem.Detail == "" || strings.Contains(problem.Detail, "#") {
		t.Fatalf("bare detail = %q, want the generic sentence", problem.Detail)
	}
}

func TestPlanStaleDetailNamesTheRun(t *testing.T) {
	t.Parallel()

	_, problem := renderAppError(t, fmt.Errorf("approve: %w", &app.PlanStaleError{RunNumber: 7, PlanState: domain.PlanStale}))
	want := "Run #7's plan no longer matches the template's configuration or revision. Plan again, then apply."
	if problem.Type != problemTypeURI("plan-stale") || problem.Detail != want {
		t.Fatalf("problem = %+v, want plan-stale with detail %q", problem, want)
	}
}

// A field error joined with a failure that isn't one must not answer 422:
// that would blame the request and hide the failure from the log.
func TestFieldErrorJoinedWithAFailureIsALogged500(t *testing.T) { // not parallel: it swaps the log output
	var logged bytes.Buffer
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	fieldErr := &app.FieldError{Path: []string{"name"}, Issue: app.IssueRequired, Message: "name is required", Err: app.ErrInvalidCommand}
	err := errors.Join(fieldErr, fmt.Errorf("append audit event: %w", errors.New("pq: connection reset")))
	response, problem := renderAppError(t, err)

	if response.Code != http.StatusInternalServerError || problem.Type != blankType || len(problem.Errors) != 0 {
		t.Fatalf("status = %d, problem = %+v, want a bare 500", response.Code, problem)
	}
	if !strings.Contains(logged.String(), "pq: connection reset") || !strings.Contains(logged.String(), "request-1") {
		t.Fatalf("log = %q, want the cause under the request id", logged.String())
	}
}

// Wrapped sentinels and the app's own error types are known causes, so field
// errors joined with them still answer 422.
func TestFieldErrorsWithKnownCausesStay422(t *testing.T) {
	t.Parallel()

	fieldErr := &app.FieldError{Path: []string{"name"}, Issue: app.IssueRequired, Message: "name is required", Err: app.ErrInvalidCommand}
	err := errors.Join(fmt.Errorf("create stack: %w", fieldErr), fmt.Errorf("check: %w", app.ErrInvalidCommand))
	if response, _ := renderAppError(t, err); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", response.Code)
	}
}

// A duplicate the app reports on a field stays a 409 of its type and points
// at the field the app named: the name, when the slug was made from it.
func TestDuplicateFieldErrorIsItsConflict(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("create stack: %w", &app.FieldError{
		Path: []string{"name"}, Issue: app.IssueDuplicate,
		Message: `Another stack already uses the slug "acme-prod", made from this name. Choose another name.`, Err: app.ErrDuplicateStackSlug,
	})
	response, problem := renderAppError(t, err)
	if response.Code != http.StatusConflict || problem.Type != problemTypeURI("duplicate-slug") {
		t.Fatalf("status = %d, problem = %+v, want 409 duplicate-slug", response.Code, problem)
	}
	if len(problem.Errors) != 1 || problem.Errors[0].Pointer != "#/name" || problem.Errors[0].Issue != app.IssueDuplicate {
		t.Fatalf("errors = %+v, want one duplicate on #/name", problem.Errors)
	}
	if !strings.Contains(problem.Detail, `"acme-prod"`) {
		t.Fatalf("detail = %q, want the field error's sentence", problem.Detail)
	}
}
