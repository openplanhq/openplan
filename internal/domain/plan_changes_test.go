package domain

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The control plane checks what the executor hands it, since that comes from
// the sandbox the template's code ran in.
func TestPlanChangesValidate(t *testing.T) {
	t.Parallel()

	valid := PlanChanges{Changes: []PlanResourceChange{{Address: "x.a", Action: PlanChangeCreate}}, Total: 1}
	require.NoError(t, valid.Validate())

	for name, changes := range map[string]PlanChanges{
		"total under the list":       {Changes: valid.Changes, Total: 0},
		"truncated without a cut":    {Changes: valid.Changes, Total: 1, Truncated: true},
		"cut without truncated":      {Changes: valid.Changes, Total: 2},
		"no address":                 {Changes: []PlanResourceChange{{Action: PlanChangeCreate}}, Total: 1},
		"no action":                  {Changes: []PlanResourceChange{{Address: "x.a"}}, Total: 1},
		"address too long":           {Changes: []PlanResourceChange{{Address: strings.Repeat("a", 4096), Action: PlanChangeCreate}}, Total: 1},
		"value not JSON":             {Changes: []PlanResourceChange{{Address: "x.a", Action: PlanChangeCreate, After: json.RawMessage(`{`)}}, Total: 1},
		"value over the limit":       {Changes: []PlanResourceChange{{Address: "x.a", Action: PlanChangeCreate, After: json.RawMessage(`"` + strings.Repeat("a", MaxPlanChangeValueBytes) + `"`)}}, Total: 1},
		"more rows than are allowed": {Changes: make([]PlanResourceChange, MaxPlanResourceChanges+1), Total: MaxPlanResourceChanges + 1},
	} {
		require.Error(t, changes.Validate(), name)
	}
}

func TestSortPlanResourceChangesReadsRemovalsFirst(t *testing.T) {
	t.Parallel()

	changes := []PlanResourceChange{
		{Address: "b", Action: PlanChangeCreate},
		{Address: "a", Action: "something-new"},
		{Address: "z", Action: PlanChangeReplace},
		{Address: "c", Action: PlanChangeUpdate},
		{Address: "a", Action: PlanChangeDestroy},
		{Address: "m", Action: PlanChangeMove},
	}
	SortPlanResourceChanges(changes)
	var order []string
	for _, change := range changes {
		order = append(order, string(change.Action)+" "+change.Address)
	}
	require.Equal(t, []string{"replace z", "destroy a", "update c", "create b", "move m", "something-new a"}, order)
}
