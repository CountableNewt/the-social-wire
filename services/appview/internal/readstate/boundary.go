package readstate

import "encoding/json"

// Boundary is a frozen mark-all floor for one publication scope.
type Boundary struct {
	Scope     Scope  `json:"scope"`
	CreatedAt string `json:"createdAt"`
	// EntryID omitted means an inclusive timestamp, matching legacy AppView floors.
	EntryID *string `json:"entryId,omitempty"`
}

// UnmarshalJSON decodes with exact keys.
func (b *Boundary) UnmarshalJSON(data []byte) error {
	value, err := decodeBoundary(data)
	if err != nil {
		return err
	}
	*b = value
	return nil
}

func decodeBoundary(raw json.RawMessage) (Boundary, error) {
	value, err := parseObject(raw)
	if err != nil {
		return Boundary{}, err
	}
	scopeRaw, err := value.required("scope")
	if err != nil {
		return Boundary{}, err
	}
	scope, err := decodeScope(scopeRaw)
	if err != nil {
		return Boundary{}, err
	}
	createdAt, err := value.str("createdAt")
	if err != nil {
		return Boundary{}, err
	}
	entryID, err := value.optStr("entryId")
	if err != nil {
		return Boundary{}, err
	}
	return Boundary{Scope: scope, CreatedAt: createdAt, EntryID: entryID}, nil
}
