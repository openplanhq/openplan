package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// PlanChangeAction is what a plan does to one resource, folded from the
// actions list tofu writes: a replacement is one action here, not a delete
// and a create, because that is how a person reads it.
//
// Actions the parser does not know are kept as tofu's own word rather than
// refused: the plan format may add actions in a minor version, and OpenTofu
// and Terraform each have some the other lacks.
type PlanChangeAction string

const (
	PlanChangeCreate  PlanChangeAction = "create"
	PlanChangeUpdate  PlanChangeAction = "update"
	PlanChangeReplace PlanChangeAction = "replace"
	PlanChangeDestroy PlanChangeAction = "destroy"
	// PlanChangeForget is OpenTofu's and Terraform's `removed` block without
	// destroy: the resource leaves the state and keeps running.
	PlanChangeForget PlanChangeAction = "forget"
	// PlanChangeImport and PlanChangeMove are a resource the plan changes in
	// nothing but its record: imported into the state, or moved to a new
	// address by a `moved` block. tofu calls both a no-op, yet a reviewer
	// should see them.
	PlanChangeImport PlanChangeAction = "import"
	PlanChangeMove   PlanChangeAction = "move"
)

// rank orders actions the way a reviewer reads them: what removes something
// first, then what changes it, then what adds it, then the rest.
func (action PlanChangeAction) rank() int {
	switch action {
	case PlanChangeReplace:
		return 0
	case PlanChangeDestroy:
		return 1
	case PlanChangeUpdate:
		return 2
	case PlanChangeCreate:
		return 3
	case PlanChangeForget, PlanChangeImport, PlanChangeMove:
		return 4
	default:
		return 5
	}
}

// Markers put in place of a value the plan can't show: one not known until
// apply, or one too large to store.
const (
	PlanValueKnownAfter     = "(known after apply)"
	PlanValueTooLargeToShow = "(too large to show)"
)

// Limits on one run's resource changes. The changes come from a sandbox that
// runs the template's own code, so the control plane enforces these again on
// what it is handed rather than trusting the executor to have.
const (
	// MaxPlanResourceChanges is how many rows one run keeps. The rest are
	// counted in PlanChanges.Total and read from the plan log.
	MaxPlanResourceChanges = 1000
	// MaxPlanChangeValueBytes caps one before or after value; a bigger one is
	// stored as PlanValueTooLargeToShow.
	MaxPlanChangeValueBytes = 16 << 10
	// MaxPlanChangesBytes caps the encoded changes, so they fit the sandbox's
	// 1 MiB result frame with room for the rest of the result.
	MaxPlanChangesBytes = 768 << 10
	// maxPlanChangeTextBytes caps every string field: an address, a reason.
	maxPlanChangeTextBytes = 2048
	// maxPlanChangesTotal bounds the count of a plan's changes, listed or
	// not, well inside Postgres's integer: no real plan comes near it.
	maxPlanChangesTotal = 1 << 24
)

// PlanResourceChange is one resource a saved plan changes, as the run's
// Changes view shows it. Before and After are the resource's values with every
// unknown one shown as PlanValueKnownAfter.
type PlanResourceChange struct {
	// Position is the change's place in its run's reading order, set when
	// it is read back from the database; the API pages by it.
	Position        int              `json:"-"`
	Address         string           `json:"address"`
	PreviousAddress string           `json:"previous_address,omitempty"`
	ModuleAddress   string           `json:"module_address,omitempty"`
	Mode            string           `json:"mode"`
	Type            string           `json:"type"`
	Name            string           `json:"name"`
	ProviderName    string           `json:"provider_name"`
	Deposed         string           `json:"deposed,omitempty"`
	Action          PlanChangeAction `json:"action"`
	// CreateBeforeDestroy is set on a replacement that creates the new object
	// before destroying the old one, so there is no gap between them.
	CreateBeforeDestroy bool            `json:"create_before_destroy,omitempty"`
	ActionReason        string          `json:"action_reason,omitempty"`
	ReplacePaths        json.RawMessage `json:"replace_paths,omitempty"`
	ImportingID         string          `json:"importing_id,omitempty"`
	Before              json.RawMessage `json:"before,omitempty"`
	After               json.RawMessage `json:"after,omitempty"`
}

// PlanChanges is a plan's resource changes, in reading order. Total counts
// every change the plan has; when it is more than len(Changes) the list was
// cut to fit the limits above, and Truncated says so.
type PlanChanges struct {
	Changes   []PlanResourceChange `json:"changes"`
	Total     int                  `json:"total"`
	Truncated bool                 `json:"truncated,omitempty"`
}

// PlanChangesState says what a run's resource changes are, for the UI to tell
// an empty list from one it never got.
type PlanChangesState string

const (
	// PlanChangesComplete: every change the plan has is listed.
	PlanChangesComplete PlanChangesState = "complete"
	// PlanChangesTruncated: the plan has more changes than were kept.
	PlanChangesTruncated PlanChangesState = "truncated"
	// PlanChangesUnavailable: the plan finished with changes but none could
	// be listed, such as a plan format this openplan does not read. The plan
	// log still has them.
	PlanChangesUnavailable PlanChangesState = "unavailable"
)

// SortPlanResourceChanges puts changes in reading order: by action, removals
// first, then by address. Truncation keeps the head, so if a plan is cut, its
// creates go before any destroy does.
func SortPlanResourceChanges(changes []PlanResourceChange) {
	sort.SliceStable(changes, func(i, j int) bool {
		if ri, rj := changes[i].Action.rank(), changes[j].Action.rank(); ri != rj {
			return ri < rj
		}
		if changes[i].Address != changes[j].Address {
			return changes[i].Address < changes[j].Address
		}
		return changes[i].Deposed < changes[j].Deposed
	})
}

// Validate checks changes handed to the control plane against the limits,
// so a hostile template's executor cannot store more, or larger, than a real
// plan would.
func (changes PlanChanges) Validate() error {
	if len(changes.Changes) > MaxPlanResourceChanges {
		return fmt.Errorf("%d resource changes is over the limit of %d", len(changes.Changes), MaxPlanResourceChanges)
	}
	if changes.Total > maxPlanChangesTotal {
		return fmt.Errorf("total %d is over the limit of %d", changes.Total, maxPlanChangesTotal)
	}
	if changes.Total < len(changes.Changes) {
		return fmt.Errorf("total %d is less than the %d changes listed", changes.Total, len(changes.Changes))
	}
	if changes.Truncated != (changes.Total > len(changes.Changes)) {
		return errors.New("truncated does not match the total")
	}
	for i, change := range changes.Changes {
		if err := change.validate(); err != nil {
			return fmt.Errorf("resource change %d: %w", i, err)
		}
	}
	return nil
}

func (change PlanResourceChange) validate() error {
	if change.Address == "" {
		return errors.New("address is required")
	}
	if change.Action == "" {
		return errors.New("action is required")
	}
	for _, text := range []string{
		change.Address, change.PreviousAddress, change.ModuleAddress, change.Mode, change.Type, change.Name,
		change.ProviderName, change.Deposed, string(change.Action), change.ActionReason, change.ImportingID,
	} {
		// Postgres text can't hold a NUL; the parser scrubs them, so one here
		// was put there by something else.
		if len(text) > maxPlanChangeTextBytes || !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
			return errors.New("a text field is too long, not UTF-8 or holds a NUL")
		}
	}
	for _, value := range []json.RawMessage{change.ReplacePaths, change.Before, change.After} {
		if len(value) == 0 {
			continue
		}
		if len(value) > MaxPlanChangeValueBytes || !json.Valid(value) || holdsNUL(value) {
			return errors.New("a value is too large, not JSON or holds a NUL")
		}
	}
	return nil
}

// holdsNUL reports whether a JSON value has a NUL in any string or key,
// which jsonb refuses. It decodes rather than searching for the escape, so
// a literal backslash followed by u0000 isn't mistaken for one.
func holdsNUL(value json.RawMessage) bool {
	var decoded any
	if json.Unmarshal(value, &decoded) != nil {
		return true
	}
	return anyNUL(decoded)
}

func anyNUL(value any) bool {
	switch typed := value.(type) {
	case string:
		return strings.ContainsRune(typed, 0)
	case map[string]any:
		for key, child := range typed {
			if strings.ContainsRune(key, 0) || anyNUL(child) {
				return true
			}
		}
	case []any:
		return slices.ContainsFunc(typed, anyNUL)
	}
	return false
}
