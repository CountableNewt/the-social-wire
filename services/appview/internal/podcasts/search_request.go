package podcasts

import (
	"strings"
	"unicode/utf8"
)

type SearchRequest struct {
	Query  string  `json:"query"`
	Scope  *string `json:"scope,omitempty"`
	Kind   *string `json:"kind,omitempty"`
	ShowID *string `json:"showId,omitempty"`
	Limit  *int    `json:"limit,omitempty"`
	Cursor *string `json:"cursor,omitempty"`
}

func (v *SearchRequest) UnmarshalJSON(data []byte) error {
	type plain SearchRequest
	var decoded plain
	if err := decodeRequired(data, &decoded, "query"); err != nil {
		return err
	}
	*v = SearchRequest(decoded)
	return nil
}

func (r SearchRequest) Validate() error {
	queryCount := utf8.RuneCountInString(strings.TrimSpace(r.Query))
	if queryCount < 2 || queryCount > 200 {
		return ErrInvalidRequest
	}
	if r.Scope != nil && *r.Scope != "library" && *r.Scope != "directory" {
		return ErrInvalidRequest
	}
	if r.Kind != nil && *r.Kind != "all" && *r.Kind != "shows" && *r.Kind != "episodes" {
		return ErrInvalidRequest
	}
	if r.Limit != nil && (*r.Limit < 1 || *r.Limit > 100) ||
		r.ShowID != nil && utf8.RuneCountInString(*r.ShowID) > 2048 ||
		r.Cursor != nil && utf8.RuneCountInString(*r.Cursor) > 4096 {
		return ErrInvalidRequest
	}
	return nil
}
