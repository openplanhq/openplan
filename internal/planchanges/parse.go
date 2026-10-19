// Package planchanges reads a saved plan's `tofu show -json` into the
// resource changes a run's Changes view lists.
//
// It runs on the executor, inside the sandbox that ran the plan: the JSON
// holds variable values, the prior state and sensitive values in plaintext,
// so it never leaves that sandbox. Only what Parse returns does, with every
// sensitive value already replaced.
package planchanges

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/vishu42/openplan/internal/domain"
)

// Parse reads a plan's JSON into its resource changes, in reading order and
// cut to the domain limits. It accepts the plan format both OpenTofu and
// Terraform write, any 1.x version; it refuses another major version, as the
// format's rules say a reader must.
//
// Reads and no-ops are left out, as tofu's own "Plan:" line leaves them out,
// except a no-op that imports or moves a resource, which a reviewer should
// still see.
func Parse(body []byte) (domain.PlanChanges, error) {
	var plan tfjson.Plan
	// Numbers stay as written: a large integer read as a float would show
	// as a different value than the one being applied.
	plan.UseJSONNumber(true)
	if err := json.Unmarshal(body, &plan); err != nil {
		return domain.PlanChanges{}, fmt.Errorf("read plan JSON: %w", err)
	}

	changes := make([]domain.PlanResourceChange, 0, len(plan.ResourceChanges))
	for _, resource := range plan.ResourceChanges {
		if resource == nil || resource.Change == nil {
			continue
		}
		change, ok, err := resourceChange(resource)
		if err != nil {
			return domain.PlanChanges{}, fmt.Errorf("read change to %s: %w", resource.Address, err)
		}
		if ok {
			changes = append(changes, change)
		}
	}
	domain.SortPlanResourceChanges(changes)
	return fit(changes), nil
}

// resourceChange is one resource's row, or false for a change nobody needs
// to review.
func resourceChange(resource *tfjson.ResourceChange) (domain.PlanResourceChange, bool, error) {
	change := resource.Change
	words := actionWords(change.Actions)
	action, createBeforeDestroy := foldActions(words)
	importing := change.Importing != nil
	moved := resource.PreviousAddress != "" && resource.PreviousAddress != resource.Address
	switch words {
	case readActions:
		return domain.PlanResourceChange{}, false, nil
	case noOpActions, "":
		switch {
		case importing:
			action = domain.PlanChangeImport
		case moved:
			action = domain.PlanChangeMove
		default:
			return domain.PlanResourceChange{}, false, nil
		}
	}

	row := domain.PlanResourceChange{
		Address:             resource.Address,
		PreviousAddress:     resource.PreviousAddress,
		ModuleAddress:       resource.ModuleAddress,
		Mode:                string(resource.Mode),
		Type:                resource.Type,
		Name:                resource.Name,
		ProviderName:        resource.ProviderName,
		Deposed:             resource.DeposedKey,
		Action:              action,
		CreateBeforeDestroy: createBeforeDestroy,
		ActionReason:        string(resource.ActionReason),
	}
	if importing {
		row.ImportingID = change.Importing.ID
	}
	var err error
	if len(change.ReplacePaths) > 0 {
		if row.ReplacePaths, err = encodeValue(change.ReplacePaths); err != nil {
			return domain.PlanResourceChange{}, false, err
		}
	}
	if change.Before != nil {
		if row.Before, err = encodeValue(mask(change.Before, change.BeforeSensitive)); err != nil {
			return domain.PlanResourceChange{}, false, err
		}
	}
	// after is unset for a destroy or a forget, though tofu still writes an
	// empty after_unknown for one. An unknown value is absent from after, so
	// it is put back, marked, before anything is masked.
	if change.After != nil || anyMarked(change.AfterUnknown) {
		after := markUnknown(change.After, change.AfterUnknown)
		if row.After, err = encodeValue(mask(after, change.AfterSensitive)); err != nil {
			return domain.PlanResourceChange{}, false, err
		}
	}
	return row, true, nil
}

// tofu's action lists that resourceChange settles before folding: a read
// is never listed, and a no-op only when it imports or moves the resource.
const (
	readActions = "read"
	noOpActions = "no-op"
)

// actionWords is tofu's action list as one comma-joined string, the form
// the switches here match on.
func actionWords(actions tfjson.Actions) string {
	words := make([]string, len(actions))
	for i, action := range actions {
		words[i] = string(action)
	}
	return strings.Join(words, ",")
}

// foldActions turns tofu's action list, as actionWords joins it, into one
// action. Both replace orders are a replace; createBeforeDestroy says which
// order. A list it doesn't recognise keeps its words, so an action added in
// a later version still shows as what tofu called it.
func foldActions(words string) (domain.PlanChangeAction, bool) {
	switch words {
	case "create":
		return domain.PlanChangeCreate, false
	case "update":
		return domain.PlanChangeUpdate, false
	case "delete":
		return domain.PlanChangeDestroy, false
	case "delete,create":
		return domain.PlanChangeReplace, false
	case "create,delete":
		return domain.PlanChangeReplace, true
	case "forget":
		return domain.PlanChangeForget, false
	default:
		return domain.PlanChangeAction(strings.ReplaceAll(words, ",", "-")), false
	}
}

// mask replaces every value its sensitivity marks as sensitive. The marks
// mirror the value's shape with true on sensitive leaves, and true on a whole
// object or list marks all of it.
func mask(value, sensitive any) any {
	if marked, ok := sensitive.(bool); ok {
		if marked {
			return domain.PlanValueSensitive
		}
		return value
	}
	switch typed := value.(type) {
	case map[string]any:
		marks, _ := sensitive.(map[string]any)
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			out[key] = mask(child, marks[key])
		}
		return out
	case []any:
		marks, _ := sensitive.([]any)
		out := make([]any, len(typed))
		for i, child := range typed {
			var mark any
			if i < len(marks) {
				mark = marks[i]
			}
			out[i] = mask(child, mark)
		}
		return out
	default:
		return value
	}
}

// markUnknown puts back, as PlanValueKnownAfter, every value after_unknown
// marks: tofu leaves those out of after, which would read as removed.
func markUnknown(value, unknown any) any {
	if marked, ok := unknown.(bool); ok {
		if marked {
			return domain.PlanValueKnownAfter
		}
		return value
	}
	switch marks := unknown.(type) {
	case map[string]any:
		object, _ := value.(map[string]any)
		out := make(map[string]any, len(object)+len(marks))
		maps.Copy(out, object)
		for key, mark := range marks {
			out[key] = markUnknown(out[key], mark)
		}
		return out
	case []any:
		list, _ := value.([]any)
		length := max(len(list), len(marks))
		out := make([]any, length)
		for i := range out {
			var child, mark any
			if i < len(list) {
				child = list[i]
			}
			if i < len(marks) {
				mark = marks[i]
			}
			out[i] = markUnknown(child, mark)
		}
		return out
	default:
		return value
	}
}

// anyMarked reports whether marks, a sensitivity or unknown structure, marks
// anything at all.
func anyMarked(marks any) bool {
	switch typed := marks.(type) {
	case bool:
		return typed
	case map[string]any:
		for _, mark := range typed {
			if anyMarked(mark) {
				return true
			}
		}
	case []any:
		return slices.ContainsFunc(typed, anyMarked)
	}
	return false
}

// encodeValue encodes a value for storage, or the too-large mark when it is
// over the per-value limit.
func encodeValue(value any) (json.RawMessage, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(encoded) > domain.MaxPlanChangeValueBytes {
		return json.Marshal(domain.PlanValueTooLargeToShow)
	}
	return encoded, nil
}

// fit keeps the head of the sorted changes within the row and byte limits.
// Sorting first means a cut drops creates before it drops any destroy.
func fit(changes []domain.PlanResourceChange) domain.PlanChanges {
	kept := 0
	size := 0
	for _, change := range changes {
		if kept == domain.MaxPlanResourceChanges {
			break
		}
		encoded, err := json.Marshal(change)
		if err != nil || size+len(encoded) > domain.MaxPlanChangesBytes {
			break
		}
		size += len(encoded)
		kept++
	}
	return domain.PlanChanges{
		Changes:   changes[:kept],
		Total:     len(changes),
		Truncated: kept < len(changes),
	}
}
