package atproto

import "strings"

// CursorOutcome classifies a listRecords page cursor.
type CursorOutcome int

// Cursor outcomes (PDSListRecordsCursor.ParseResult).
const (
	CursorEnd CursorOutcome = iota
	CursorNext
	CursorInvalid
)

// CursorResult is the parsed cursor: Value is the next cursor for CursorNext
// or the rejection reason for CursorInvalid.
type CursorResult struct {
	Outcome CursorOutcome
	Value   string
}

// ParseListRecordsCursor validates a listRecords page cursor against the
// current cursor ("" for the first page) and every cursor already seen.
func ParseListRecordsCursor(json map[string]any, current string, seen map[string]struct{}, pageIsEmpty bool) CursorResult {
	raw, present := json["cursor"]
	if !present {
		return CursorResult{Outcome: CursorEnd}
	}
	text, ok := raw.(string)
	if !ok {
		return CursorResult{CursorInvalid, "cursor_not_string"}
	}
	cursor := strings.TrimSpace(text)
	if len(cursor) > 2048 {
		return CursorResult{CursorInvalid, "cursor_too_long"}
	}
	if cursor == "" {
		return CursorResult{CursorInvalid, "cursor_empty"}
	}
	if _, repeated := seen[cursor]; cursor == current || repeated {
		return CursorResult{CursorInvalid, "cursor_cycle"}
	}
	if pageIsEmpty {
		return CursorResult{CursorInvalid, "empty_page_with_cursor"}
	}
	return CursorResult{CursorNext, cursor}
}
