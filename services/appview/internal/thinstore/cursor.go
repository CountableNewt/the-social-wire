package thinstore

import (
	"strings"
	"time"
)

// CursorTimestamp keeps the database's microsecond boundary through scanned
// batches and client pages (ThinAppViewCursor.timestamp).
func CursorTimestamp(t time.Time) string { return microsTimestamp(t) }

// EncodeCursor builds the `timestamp|uri` keyset cursor.
func EncodeCursor(createdAt time.Time, uri string) string {
	return microsTimestamp(createdAt) + "|" + uri
}

// DecodeCursor parses an EncodeCursor value; ok is false for malformed cursors.
func DecodeCursor(cursor string) (createdAt time.Time, uri string, ok bool) {
	pipe := strings.IndexByte(cursor, '|')
	if pipe < 0 {
		return time.Time{}, "", false
	}
	iso, rest := cursor[:pipe], cursor[pipe+1:]
	if rest == "" {
		return time.Time{}, "", false
	}
	date, parsed := dateFromISO(iso)
	if !parsed {
		return time.Time{}, "", false
	}
	return date, rest, true
}

// cursorPosition is a decoded keyset position.
type cursorPosition struct {
	createdAt time.Time
	uri       string
}

func decodeCursorPointer(cursor *string) *cursorPosition {
	if cursor == nil {
		return nil
	}
	createdAt, uri, ok := DecodeCursor(*cursor)
	if !ok {
		return nil
	}
	return &cursorPosition{createdAt: createdAt, uri: uri}
}
