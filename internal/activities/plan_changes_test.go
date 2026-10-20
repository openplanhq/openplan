package activities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vishu42/openplan/internal/config"
	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/runner"
	"github.com/vishu42/openplan/internal/runseal"
	"github.com/vishu42/openplan/internal/sandbox"
)

// exitCodeError is a command that exited with code, as tofu plan does with
// -detailed-exitcode: 2 means a plan with changes.
type exitCodeError struct{ code int }

func (err exitCodeError) Error() string { return fmt.Sprintf("exit status %d", err.code) }
func (err exitCodeError) ExitCode() int { return err.code }

// planningExecutor is tofu planning a change: plan exits 2, and show prints
// the plan's JSON, unless failPlan makes the plan itself fail.
type planningExecutor struct {
	planJSON []byte
	failPlan bool
}

func (e *planningExecutor) Run(_ context.Context, _ string, _ []string, stdout io.Writer, _ io.Writer, _ string, args ...string) error {
	switch args[0] {
	case "plan":
		if e.failPlan {
			return exitCodeError{code: 1}
		}
		_, _ = io.WriteString(stdout, "Plan: 4 to add, 2 to change, 3 to destroy.\n")
		return exitCodeError{code: 2}
	case "show":
		_, _ = stdout.Write(e.planJSON)
	}
	return nil
}

// memoryPlanChangesStore is the artifact store's plan changes, in memory.
type memoryPlanChangesStore struct {
	mu   sync.Mutex
	body map[domain.TemplateRunID][]byte
	puts int
}

func (store *memoryPlanChangesStore) PutPlanChanges(_ context.Context, _ domain.TenantID, runID domain.TemplateRunID, body []byte) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.body == nil {
		store.body = map[domain.TemplateRunID][]byte{}
	}
	store.body[runID] = append([]byte(nil), body...)
	store.puts++
	return nil
}

func (store *memoryPlanChangesStore) GetPlanChanges(_ context.Context, _ domain.TenantID, runID domain.TemplateRunID) ([]byte, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	body, ok := store.body[runID]
	if !ok {
		return nil, fmt.Errorf("get plan changes: %w", os.ErrNotExist)
	}
	return body, nil
}

// The whole executor path: the plan's sandbox parses `tofu show -json`, the
// worker leaves the masked changes beside the log, and UploadLog's storage
// sandbox puts them in the artifact store. The plan JSON, and its secret,
// stop at the sandbox; the activity's output, which Temporal keeps, carries
// only the counts.
func TestPlanChangesTravelFromThePlanToTheArtifactStore(t *testing.T) {
	t.Parallel()

	planJSON, err := os.ReadFile("../planchanges/testdata/tofu.plan.json")
	require.NoError(t, err)
	activities, changes, workspace := newPlanChangesActivities(t, &planningExecutor{planJSON: planJSON})

	output, err := activities.RunTerraform(context.Background(), planInput(workspace))
	require.NoError(t, err)
	require.True(t, output.HasChanges)
	encodedOutput, err := json.Marshal(output)
	require.NoError(t, err)
	require.NotContains(t, string(encodedOutput), "terraform_data", "the activity's output must not carry the changes")

	_, err = activities.UploadLog(context.Background(), domain.UploadLogActivityInput{TenantID: "tenant_123", RunID: "run_123", LogName: "plan"})
	require.NoError(t, err)

	uploaded, err := changes.GetPlanChanges(context.Background(), "tenant_123", "run_123")
	require.NoError(t, err)
	var read domain.PlanChanges
	require.NoError(t, json.Unmarshal(uploaded, &read))
	require.Equal(t, 8, read.Total)
	require.Equal(t, "terraform_data.replaced", read.Changes[0].Address)
	require.NoError(t, read.Validate())
}

// A retried plan that fails must not upload the changes an earlier attempt
// left behind as if they were its own.
func TestRunTerraformDropsAnEarlierAttemptsChanges(t *testing.T) {
	t.Parallel()

	planJSON, err := os.ReadFile("../planchanges/testdata/tofu.plan.json")
	require.NoError(t, err)
	tofu := &planningExecutor{planJSON: planJSON}
	activities, changes, workspace := newPlanChangesActivities(t, tofu)

	_, err = activities.RunTerraform(context.Background(), planInput(workspace))
	require.NoError(t, err)
	tofu.failPlan = true
	_, err = activities.RunTerraform(context.Background(), planInput(workspace))
	require.Error(t, err)

	_, err = activities.UploadLog(context.Background(), domain.UploadLogActivityInput{TenantID: "tenant_123", RunID: "run_123", LogName: "plan"})
	require.NoError(t, err)
	require.Zero(t, changes.puts)
}

// A plan whose JSON this openplan can't read still plans: its counts stand
// and no changes are uploaded, which FinishPlan records as unavailable.
func TestRunTerraformPlansWhenTheChangesCannotBeRead(t *testing.T) {
	t.Parallel()

	activities, changes, workspace := newPlanChangesActivities(t, &planningExecutor{
		planJSON: []byte(`{"format_version":"2.0","resource_changes":[{"change":{"actions":["create"]}}]}`),
	})
	output, err := activities.RunTerraform(context.Background(), planInput(workspace))
	require.NoError(t, err)
	require.Equal(t, domain.PlanSummary{Add: 1}, output.Summary)

	_, err = activities.UploadLog(context.Background(), domain.UploadLogActivityInput{TenantID: "tenant_123", RunID: "run_123", LogName: "plan"})
	require.NoError(t, err)
	require.Zero(t, changes.puts)
}

func newPlanChangesActivities(t *testing.T, tofu runner.CommandExecutor) (*TemplateRunActivities, *memoryPlanChangesStore, string) {
	t.Helper()
	changes := &memoryPlanChangesStore{}
	bodies := &Bodies{
		terraform: runner.NewLocalTerraformRunnerWithExecutor(tofu),
		logs:      &recordingTemplateRunLogStore{},
		changes:   changes,
	}
	activities := NewTemplateRunActivities(sandbox.InProcess{Handlers: bodies.Handlers()}, sandbox.Workspaces{Root: t.TempDir()},
		config.ArtifactStoreConfig{Kind: config.ArtifactStoreS3}, nil, runseal.NewKeyRing())
	return activities, changes, prepareTestWorkspace(t, activities)
}

// A list that can't be stored must not cost the plan its log, the record of
// every change.
func TestUploadLogKeepsTheLogWhenTheChangesCannotBeStored(t *testing.T) {
	t.Parallel()

	planJSON, err := os.ReadFile("../planchanges/testdata/tofu.plan.json")
	require.NoError(t, err)
	logs := &recordingTemplateRunLogStore{}
	bodies := &Bodies{
		terraform: runner.NewLocalTerraformRunnerWithExecutor(&planningExecutor{planJSON: planJSON}),
		logs:      logs,
		changes:   refusingChangesStore{},
	}
	activities := NewTemplateRunActivities(sandbox.InProcess{Handlers: bodies.Handlers()}, sandbox.Workspaces{Root: t.TempDir()},
		config.ArtifactStoreConfig{Kind: config.ArtifactStoreS3}, nil, runseal.NewKeyRing())
	workspace := prepareTestWorkspace(t, activities)

	_, err = activities.RunTerraform(context.Background(), planInput(workspace))
	require.NoError(t, err)
	log, err := activities.UploadLog(context.Background(), domain.UploadLogActivityInput{TenantID: "tenant_123", RunID: "run_123", LogName: "plan"})
	require.NoError(t, err)
	require.Equal(t, "plan", log.Phase)
	require.Contains(t, logs.content, "Plan: 4 to add")
}

type refusingChangesStore struct{}

func (refusingChangesStore) PutPlanChanges(context.Context, domain.TenantID, domain.TemplateRunID, []byte) error {
	return errors.New("bucket unavailable")
}

// Once recorded, the upload is deleted: Postgres holds the list.
func TestFinishPlanDeletesTheRecordedUpload(t *testing.T) {
	t.Parallel()

	body, err := json.Marshal(domain.PlanChanges{Changes: []domain.PlanResourceChange{{Address: "x.a", Action: domain.PlanChangeCreate}}, Total: 1})
	require.NoError(t, err)
	reader := &memoryPlanChangesStore{}
	require.NoError(t, reader.PutPlanChanges(context.Background(), "tenant_123", "run_123", body))
	store := &controlStoreStub{}

	_, err = NewControlActivities(store, nil, nil).WithPlanChanges(reader).FinishPlan(context.Background(), domain.FinishPlanActivityInput{
		TenantID: "tenant_123", RunID: "run_123", HasChanges: true,
	})
	require.NoError(t, err)
	require.NotNil(t, store.planChanges)
	_, err = reader.GetPlanChanges(context.Background(), "tenant_123", "run_123")
	require.ErrorIs(t, err, os.ErrNotExist)
}

// FinishPlan records what the executor uploaded, checked again; anything it
// can't trust is recorded as unavailable, and only an unreachable store
// fails the activity.
func TestFinishPlanChecksTheUploadedChanges(t *testing.T) {
	t.Parallel()

	valid, err := json.Marshal(domain.PlanChanges{
		Changes: []domain.PlanResourceChange{
			{Address: "x.b", Action: domain.PlanChangeCreate},
			{Address: "x.a", Action: domain.PlanChangeDestroy},
		},
		Total: 2,
	})
	require.NoError(t, err)
	tooMany := `{"changes":[],"total":0,"truncated":true}`

	for name, test := range map[string]struct {
		body       []byte
		hasChanges bool
		readErr    error
		want       []string
		wantErr    bool
	}{
		"recorded, re-sorted":        {body: valid, hasChanges: true, want: []string{"x.a", "x.b"}},
		"none uploaded":              {hasChanges: true},
		"not JSON":                   {body: []byte("{"), hasChanges: true},
		"over the limits":            {body: []byte(tooMany), hasChanges: true},
		"too large to be real":       {body: []byte(strings.Repeat(" ", 2*domain.MaxPlanChangesBytes+1)), hasChanges: true},
		"a plan without changes":     {body: valid},
		"the store can't be reached": {hasChanges: true, readErr: errors.New("connection refused"), wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := &controlStoreStub{}
			reader := &memoryPlanChangesStore{}
			if test.body != nil {
				require.NoError(t, reader.PutPlanChanges(context.Background(), "tenant_123", "run_123", test.body))
			}
			var source PlanChangesReader = reader
			if test.readErr != nil {
				source = failingChangesReader{err: test.readErr}
			}
			control := NewControlActivities(store, nil, nil).WithPlanChanges(source)

			_, err := control.FinishPlan(context.Background(), domain.FinishPlanActivityInput{
				TenantID: "tenant_123", RunID: "run_123", HasChanges: test.hasChanges,
			})
			if test.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if test.want == nil {
				require.Nil(t, store.planChanges)
				return
			}
			require.NotNil(t, store.planChanges)
			var got []string
			for _, change := range store.planChanges.Changes {
				got = append(got, change.Address)
			}
			require.Equal(t, test.want, got)
		})
	}
}

func (store *memoryPlanChangesStore) DeletePlanChanges(_ context.Context, _ domain.TenantID, runID domain.TemplateRunID) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.body, runID)
	return nil
}

type failingChangesReader struct{ err error }

func (failingChangesReader) DeletePlanChanges(context.Context, domain.TenantID, domain.TemplateRunID) error {
	return nil
}

func (reader failingChangesReader) GetPlanChanges(context.Context, domain.TenantID, domain.TemplateRunID) ([]byte, error) {
	return nil, reader.err
}
