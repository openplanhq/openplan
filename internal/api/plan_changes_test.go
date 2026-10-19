package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vishu42/openplan/internal/domain"
)

// The changes list pages in the order the plan was sorted in, and every item
// carries every field, null where a value is missing, so a client reads one
// shape.
func TestListTemplateRunResourceChangesPagesInOrder(t *testing.T) {
	t.Parallel()

	deps := newAPITestDependencies(t)
	deps.templateRuns.run = domain.TemplateRun{ID: "run_123", TenantID: "tenant_123"}
	deps.planChanges.changes = map[domain.TemplateRunID][]domain.PlanResourceChange{
		"run_123": {
			{Address: "aws_db_instance.main", Mode: "managed", Type: "aws_db_instance", Name: "main", Action: domain.PlanChangeReplace,
				ActionReason: "replace_because_cannot_update", ReplacePaths: json.RawMessage(`[["storage_encrypted"]]`),
				Before: json.RawMessage(`{"password":"(sensitive)"}`), After: json.RawMessage(`{"password":"(sensitive)"}`)},
			{Address: "aws_s3_bucket.logs", Mode: "managed", Type: "aws_s3_bucket", Name: "logs", Action: domain.PlanChangeDestroy,
				Before: json.RawMessage(`{"bucket":"logs"}`)},
			{Address: "aws_iam_role.monitoring", Mode: "managed", Type: "aws_iam_role", Name: "monitoring", Action: domain.PlanChangeCreate,
				After: json.RawMessage(`{"arn":"(known after apply)"}`)},
		},
	}
	server := NewServer(deps.service(), configuredTenantID)

	first := httptest.NewRecorder()
	server.ServeHTTP(first, authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/template-runs/run_123/changes?limit=2", nil))
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	var page struct {
		Items      []map[string]json.RawMessage `json:"items"`
		NextCursor *string                      `json:"next_cursor"`
	}
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &page))
	require.Len(t, page.Items, 2)
	require.JSONEq(t, `"aws_db_instance.main"`, string(page.Items[0]["address"]))
	require.JSONEq(t, `"replace"`, string(page.Items[0]["action"]))
	require.JSONEq(t, `[["storage_encrypted"]]`, string(page.Items[0]["replace_paths"]))
	require.JSONEq(t, `null`, string(page.Items[1]["after"]), "a destroy has no after")
	require.JSONEq(t, `null`, string(page.Items[1]["replace_paths"]))
	require.NotNil(t, page.NextCursor)

	second := httptest.NewRecorder()
	server.ServeHTTP(second, authenticatedRequest(http.MethodGet, "/v1/tenants/tenant_123/template-runs/run_123/changes?limit=2&cursor="+*page.NextCursor, nil))
	require.Equal(t, http.StatusOK, second.Code, second.Body.String())
	require.NoError(t, json.Unmarshal(second.Body.Bytes(), &page))
	require.Len(t, page.Items, 1)
	require.JSONEq(t, `"aws_iam_role.monitoring"`, string(page.Items[0]["address"]))
	require.Nil(t, page.NextCursor)
}

// The run says what its list is, so the UI can tell an empty list from one
// it never got.
func TestTemplateRunCarriesPlanChangesState(t *testing.T) {
	t.Parallel()

	total := 6
	require.Nil(t, newTemplateRunResponse(domain.TemplateRun{}).PlanChanges)
	require.Equal(t, &planChangesResponse{State: "truncated", Total: &total},
		newTemplateRunResponse(domain.TemplateRun{PlanChangesState: domain.PlanChangesTruncated, PlanChangesTotal: &total}).PlanChanges)
	require.Equal(t, &planChangesResponse{State: "unavailable"},
		newTemplateRunResponse(domain.TemplateRun{PlanChangesState: domain.PlanChangesUnavailable}).PlanChanges)
}
