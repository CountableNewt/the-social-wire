package thinstore

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// FinanceSelectionMutation is one validated app.thesocialwire.finance.selection commit.
type FinanceSelectionMutation struct {
	ViewerDID string
	RecordKey string
	Kind      *string
	Reference *string
	EventAt   time.Time
	RepoRev   string
}

// IsDeleted reports a delete (no kind).
func (m FinanceSelectionMutation) IsDeleted() bool { return m.Kind == nil }

// SportsSelectionMutation is one validated app.thesocialwire.sports.selection commit.
type SportsSelectionMutation struct {
	ViewerDID string
	RecordKey string
	Action    *string
	Reference *string
	EventAt   time.Time
	RepoRev   string
}

// IsDeleted reports a delete (no action).
func (m SportsSelectionMutation) IsDeleted() bool { return m.Action == nil }

func isLowerHexKey(recordKey string) bool {
	if len(recordKey) != 64 {
		return false
	}
	for _, c := range recordKey {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func hasDIDPrefix(value string) bool { return len(value) >= 4 && value[:4] == "did:" }

// ParseFinanceSelectionMutation validates a finance selection commit; the record key
// must be the SHA-256 of "kind:reference".
func ParseFinanceSelectionMutation(viewerDID, recordKey, operation string, record map[string]any,
	eventAt time.Time, repoRev *string) (FinanceSelectionMutation, bool) {
	if !hasDIDPrefix(viewerDID) || !isLowerHexKey(recordKey) {
		return FinanceSelectionMutation{}, false
	}
	rev := stringValue(repoRev)
	if operation == "delete" {
		return FinanceSelectionMutation{ViewerDID: viewerDID, RecordKey: recordKey, EventAt: eventAt, RepoRev: rev}, true
	}
	if operation != "create" && operation != "update" {
		return FinanceSelectionMutation{}, false
	}
	if recordType, _ := record["$type"].(string); recordType != "app.thesocialwire.finance.selection" {
		return FinanceSelectionMutation{}, false
	}
	kind, _ := record["kind"].(string)
	if kind != "instrument" && kind != "sector" {
		return FinanceSelectionMutation{}, false
	}
	reference, _ := record["reference"].(string)
	if reference == "" || len(reference) > 128 {
		return FinanceSelectionMutation{}, false
	}
	if sha256Hex(kind+":"+reference) != recordKey {
		return FinanceSelectionMutation{}, false
	}
	return FinanceSelectionMutation{ViewerDID: viewerDID, RecordKey: recordKey, Kind: &kind,
		Reference: &reference, EventAt: eventAt, RepoRev: rev}, true
}

// ParseSportsSelectionMutation validates a sports selection commit; the record key
// must be the SHA-256 of the reference.
func ParseSportsSelectionMutation(viewerDID, recordKey, operation string, record map[string]any,
	eventAt time.Time, repoRev *string) (SportsSelectionMutation, bool) {
	if !hasDIDPrefix(viewerDID) || !isLowerHexKey(recordKey) {
		return SportsSelectionMutation{}, false
	}
	rev := stringValue(repoRev)
	if operation == "delete" {
		return SportsSelectionMutation{ViewerDID: viewerDID, RecordKey: recordKey, EventAt: eventAt, RepoRev: rev}, true
	}
	if operation != "create" && operation != "update" {
		return SportsSelectionMutation{}, false
	}
	if recordType, _ := record["$type"].(string); recordType != "app.thesocialwire.sports.selection" {
		return SportsSelectionMutation{}, false
	}
	action, _ := record["action"].(string)
	if action != "follow" && action != "mute" {
		return SportsSelectionMutation{}, false
	}
	reference, _ := record["reference"].(string)
	if reference == "" || len(reference) > 128 {
		return SportsSelectionMutation{}, false
	}
	if sha256Hex(reference) != recordKey {
		return SportsSelectionMutation{}, false
	}
	return SportsSelectionMutation{ViewerDID: viewerDID, RecordKey: recordKey, Action: &action,
		Reference: &reference, EventAt: eventAt, RepoRev: rev}, true
}
