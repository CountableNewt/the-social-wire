package readstate

import (
	"reflect"
)

// OperationState is ReadStateOperation.State.
type OperationState string

// Selection is ReadStateOperation.Selection.
type Selection string

// Operation states and selections.
const (
	StateRead           OperationState = "read"
	StateUnread         OperationState = "unread"
	SelectionBoundaries Selection      = "boundaries"
	SelectionExact      Selection      = "exact"
)

// Operation is one committed read-state action (or a fragment of one).
// Boundaries and SubjectURIs are nil when absent; an empty non-nil slice is a
// present but empty array, which validation rejects.
type Operation struct {
	ActionID string
	// Sequence is assigned against the current manifest, not a device wall clock.
	Sequence    int64
	State       OperationState
	ActedAt     string
	Selection   Selection
	Boundaries  []Boundary
	SubjectURIs []string
	Calendar    *CalendarSelection
}

// NewBoundaryOperation builds a boundaries selection.
func NewBoundaryOperation(actionID string, sequence int64, state OperationState, actedAt string, boundaries []Boundary) Operation {
	if boundaries == nil {
		boundaries = []Boundary{}
	}
	return Operation{ActionID: actionID, Sequence: sequence, State: state, ActedAt: actedAt,
		Selection: SelectionBoundaries, Boundaries: boundaries}
}

// NewExactOperation builds an exact selection.
func NewExactOperation(actionID string, sequence int64, state OperationState, actedAt string,
	subjectURIs []string, calendar *CalendarSelection) Operation {
	if subjectURIs == nil {
		subjectURIs = []string{}
	}
	return Operation{ActionID: actionID, Sequence: sequence, State: state, ActedAt: actedAt,
		Selection: SelectionExact, SubjectURIs: subjectURIs, Calendar: calendar}
}

// Equal compares every field, distinguishing absent and empty arrays.
func (o Operation) Equal(other Operation) bool { return reflect.DeepEqual(o, other) }

type operationWire struct {
	ActionID    string             `json:"actionId"`
	Sequence    int64              `json:"sequence"`
	State       OperationState     `json:"state"`
	ActedAt     string             `json:"actedAt"`
	Selection   Selection          `json:"selection"`
	Boundaries  *[]Boundary        `json:"boundaries,omitempty"`
	SubjectURIs *[]string          `json:"subjectUris,omitempty"`
	Calendar    *CalendarSelection `json:"calendar,omitempty"`
}

func (o Operation) wire() operationWire {
	wire := operationWire{ActionID: o.ActionID, Sequence: o.Sequence, State: o.State,
		ActedAt: o.ActedAt, Selection: o.Selection, Calendar: o.Calendar}
	if o.Boundaries != nil {
		wire.Boundaries = &o.Boundaries
	}
	if o.SubjectURIs != nil {
		wire.SubjectURIs = &o.SubjectURIs
	}
	return wire
}

// MarshalJSON uses Swift's synthesized key order and omits absent optionals.
func (o Operation) MarshalJSON() ([]byte, error) { return marshalNoEscape(o.wire()) }

// UnmarshalJSON decodes with exact keys.
func (o *Operation) UnmarshalJSON(data []byte) error {
	value, err := parseObject(data)
	if err != nil {
		return err
	}
	operation, err := decodeOperation(value)
	if err != nil {
		return err
	}
	*o = operation
	return nil
}

func decodeOperation(value object) (Operation, error) {
	var result Operation
	var err error
	if result.ActionID, err = value.str("actionId"); err != nil {
		return Operation{}, err
	}
	if result.Sequence, err = value.int64("sequence"); err != nil {
		return Operation{}, err
	}
	state, err := value.str("state")
	if err != nil {
		return Operation{}, err
	}
	if state != string(StateRead) && state != string(StateUnread) {
		return Operation{}, decodeErr("unknown state")
	}
	result.State = OperationState(state)
	if result.ActedAt, err = value.str("actedAt"); err != nil {
		return Operation{}, err
	}
	selection, err := value.str("selection")
	if err != nil {
		return Operation{}, err
	}
	if selection != string(SelectionBoundaries) && selection != string(SelectionExact) {
		return Operation{}, decodeErr("unknown selection")
	}
	result.Selection = Selection(selection)
	if value.present("boundaries") {
		items, err := value.array("boundaries")
		if err != nil {
			return Operation{}, err
		}
		result.Boundaries = make([]Boundary, 0, len(items))
		for _, item := range items {
			boundary, err := decodeBoundary(item)
			if err != nil {
				return Operation{}, err
			}
			result.Boundaries = append(result.Boundaries, boundary)
		}
	}
	if value.present("subjectUris") {
		if result.SubjectURIs, err = value.stringArray("subjectUris"); err != nil {
			return Operation{}, err
		}
	}
	if value.present("calendar") {
		calendar, err := decodeCalendar(value["calendar"])
		if err != nil {
			return Operation{}, err
		}
		result.Calendar = &calendar
	}
	return result, nil
}
