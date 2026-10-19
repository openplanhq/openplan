package planchanges

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishu42/openplan/internal/domain"
)

// The fixtures are real plans, from OpenTofu 1.12 and Terraform 1.13, of the
// same change (testdata/before.tf to testdata/after.tf; generate.sh makes
// them). Both tools must read the same, since neither is second-class.
func TestParseRealPlans(t *testing.T) {
	t.Parallel()

	for _, tool := range []string{"tofu", "terraform"} {
		t.Run(tool, func(t *testing.T) {
			t.Parallel()
			body, err := os.ReadFile("testdata/" + tool + ".plan.json")
			require.NoError(t, err)

			changes, err := Parse(body)
			require.NoError(t, err)

			type row struct {
				address string
				action  domain.PlanChangeAction
			}
			got := make([]row, len(changes.Changes))
			for i, change := range changes.Changes {
				got[i] = row{change.Address, change.Action}
			}
			// Removals first, then updates, creates, and the record-only
			// changes, each kind by address.
			assert.Equal(t, []row{
				{"terraform_data.replaced", domain.PlanChangeReplace},
				{"terraform_data.replaced_cbd", domain.PlanChangeReplace},
				{"terraform_data.destroyed", domain.PlanChangeDestroy},
				{"terraform_data.secret", domain.PlanChangeUpdate},
				{"terraform_data.updated", domain.PlanChangeUpdate},
				{"terraform_data.created", domain.PlanChangeCreate},
				{"terraform_data.forgotten", domain.PlanChangeForget},
				{"terraform_data.new_name", domain.PlanChangeMove},
			}, got)
			assert.Equal(t, 8, changes.Total)
			assert.False(t, changes.Truncated)

			byAddress := map[string]domain.PlanResourceChange{}
			for _, change := range changes.Changes {
				byAddress[change.Address] = change
			}

			replaced := byAddress["terraform_data.replaced"]
			assert.False(t, replaced.CreateBeforeDestroy)
			assert.Equal(t, "replace_because_cannot_update", replaced.ActionReason)
			assert.JSONEq(t, `[["triggers_replace"]]`, string(replaced.ReplacePaths))
			assert.True(t, byAddress["terraform_data.replaced_cbd"].CreateBeforeDestroy)

			destroyed := byAddress["terraform_data.destroyed"]
			assert.Equal(t, "delete_because_no_resource_config", destroyed.ActionReason)
			assert.Nil(t, destroyed.After, "a destroy has nothing after")

			assert.Equal(t, "terraform_data.old_name", byAddress["terraform_data.new_name"].PreviousAddress)

			// The sensitive input is masked on both sides; the output tofu
			// can't know yet is marked rather than dropped.
			secret := byAddress["terraform_data.secret"]
			var before, after map[string]any
			require.NoError(t, json.Unmarshal(secret.Before, &before))
			require.NoError(t, json.Unmarshal(secret.After, &after))
			assert.Equal(t, domain.PlanValueSensitive, before["input"])
			assert.Equal(t, domain.PlanValueSensitive, after["input"])
			assert.Equal(t, domain.PlanValueKnownAfter, after["output"])

			// Nested values and numbers come through as written.
			var created map[string]any
			require.NoError(t, json.Unmarshal(byAddress["terraform_data.created"].After, &created))
			assert.Equal(t, map[string]any{"name": "alarm", "nested": []any{1.0, 2.0}}, created["input"])
			assert.Equal(t, domain.PlanValueKnownAfter, created["output"])

			// The variable's current value is in the plan, under variables
			// and the resource's sensitive input, and must reach neither.
			// The old output is not marked sensitive by either tool, and
			// tofu's own plan log prints it too, so it is shown as tofu
			// shows it.
			require.Contains(t, string(body), "hunter3")
			encoded, err := json.Marshal(changes)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), "hunter3")
			assert.Equal(t, "hunter2", before["output"])
		})
	}
}

// The format's rule: a reader refuses a major version it doesn't know. The
// plan's counts still stand: only the list is refused.
func TestReadRefusesAnotherMajorVersionsList(t *testing.T) {
	t.Parallel()

	body := []byte(`{"format_version":"2.0","resource_changes":[{"change":{"actions":["delete","create"]}}]}`)
	summary, changes, err := Read(body)
	require.NoError(t, err)
	assert.Equal(t, domain.PlanSummary{Add: 1, Destroy: 1}, summary)
	assert.Nil(t, changes)
	_, err = Parse(body)
	require.Error(t, err)
}

// The counts come from the same read as the rows, the way tofu's own Plan:
// line counts: a replace is one add and one destroy; a forget is nothing.
func TestReadCountsLikeTofu(t *testing.T) {
	t.Parallel()

	for _, tool := range []string{"tofu", "terraform"} {
		body, err := os.ReadFile("testdata/" + tool + ".plan.json")
		require.NoError(t, err)
		summary, _, err := Read(body)
		require.NoError(t, err)
		assert.Equal(t, domain.PlanSummary{Add: 3, Change: 2, Destroy: 3}, summary, tool)
	}
}

func TestReadFailsOnJSONItCannotDecode(t *testing.T) {
	t.Parallel()

	_, _, err := Read([]byte(`{`))
	require.Error(t, err)
}

// Postgres can't store a NUL, so any in tofu's strings are replaced rather
// than failing the plan when FinishPlan stores them.
func TestParseScrubsNULs(t *testing.T) {
	t.Parallel()

	changes, err := Parse(plan(`{"address":"x.a\u0000","mode":"managed","type":"x","name":"a","change":{"actions":["create"],"after":{"k\u0000":"v\u0000"}}}`))
	require.NoError(t, err)
	require.Len(t, changes.Changes, 1)
	assert.Equal(t, "x.a\uFFFD", changes.Changes[0].Address)
	assert.JSONEq(t, "{\"k\uFFFD\":\"v\uFFFD\"}", string(changes.Changes[0].After))
	require.NoError(t, changes.Validate())
}

func TestParseKeepsActionsItDoesNotName(t *testing.T) {
	t.Parallel()

	changes, err := Parse(plan(`{"address":"x.a","mode":"managed","type":"x","name":"a","change":{"actions":["create","forget"],"after":{}}}`))
	require.NoError(t, err)
	require.Len(t, changes.Changes, 1)
	assert.Equal(t, domain.PlanChangeAction("create-forget"), changes.Changes[0].Action)
}

// A no-op is left out, unless it imports the resource, which a reviewer
// should still see; reads are always left out.
func TestParseImportsAndSkipsNoOpsAndReads(t *testing.T) {
	t.Parallel()

	changes, err := Parse(plan(
		`{"address":"aws_instance.web","mode":"managed","type":"aws_instance","name":"web","change":{"actions":["no-op"],"before":{"id":"i-1"},"after":{"id":"i-1"},"importing":{"id":"i-1"}}}`,
		`{"address":"aws_instance.same","mode":"managed","type":"aws_instance","name":"same","change":{"actions":["no-op"],"before":{},"after":{}}}`,
		`{"address":"data.aws_ami.ubuntu","mode":"data","type":"aws_ami","name":"ubuntu","change":{"actions":["read"],"after":{}}}`,
	))
	require.NoError(t, err)
	require.Len(t, changes.Changes, 1)
	assert.Equal(t, domain.PlanChangeImport, changes.Changes[0].Action)
	assert.Equal(t, "i-1", changes.Changes[0].ImportingID)
	assert.Equal(t, 1, changes.Total)
}

// A mark of true on an object or a list masks all of it, and marks inside a
// list mask by index.
func TestParseMasksWholeObjectsAndListItems(t *testing.T) {
	t.Parallel()

	changes, err := Parse(plan(`{"address":"x.a","mode":"managed","type":"x","name":"a","change":{"actions":["update"],
		"before":{"block":{"password":"old","user":"u"},"keys":["k1","k2"]},
		"after":{"block":{"password":"new","user":"u"},"keys":["k1","k3"]},
		"before_sensitive":{"block":true,"keys":[false,true]},
		"after_sensitive":{"block":true,"keys":[false,true]}}}`))
	require.NoError(t, err)
	require.Len(t, changes.Changes, 1)
	assert.JSONEq(t, `{"block":"(sensitive)","keys":["k1","(sensitive)"]}`, string(changes.Changes[0].Before))
	assert.JSONEq(t, `{"block":"(sensitive)","keys":["k1","(sensitive)"]}`, string(changes.Changes[0].After))
}

// Over the row limit, the sort decides what's cut: creates go, destroys stay.
func TestParseCutsCreatesBeforeDestroys(t *testing.T) {
	t.Parallel()

	var resources []string
	for i := range domain.MaxPlanResourceChanges {
		resources = append(resources, fmt.Sprintf(`{"address":"x.create_%04d","mode":"managed","type":"x","name":"c","change":{"actions":["create"],"after":{}}}`, i))
	}
	resources = append(resources, `{"address":"x.zz_destroy","mode":"managed","type":"x","name":"d","change":{"actions":["delete"],"before":{}}}`)

	changes, err := Parse(plan(resources...))
	require.NoError(t, err)
	assert.Len(t, changes.Changes, domain.MaxPlanResourceChanges)
	assert.Equal(t, domain.MaxPlanResourceChanges+1, changes.Total)
	assert.True(t, changes.Truncated)
	assert.Equal(t, "x.zz_destroy", changes.Changes[0].Address)
	require.NoError(t, changes.Validate())
}

func TestParseMarksAValueTooLargeToShow(t *testing.T) {
	t.Parallel()

	big := strings.Repeat("a", domain.MaxPlanChangeValueBytes)
	changes, err := Parse(plan(fmt.Sprintf(`{"address":"x.a","mode":"managed","type":"x","name":"a","change":{"actions":["create"],"after":{"blob":%q}}}`, big)))
	require.NoError(t, err)
	assert.JSONEq(t, `"(too large to show)"`, string(changes.Changes[0].After))
}

func plan(resources ...string) []byte {
	return []byte(`{"format_version":"1.2","resource_changes":[` + strings.Join(resources, ",") + `]}`)
}
