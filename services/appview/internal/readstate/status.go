package readstate

import "encoding/json"

// Authority names which side owns a viewer's read state.
type Authority string

// MigrationState is PDSReadStateStatus.MigrationState.
type MigrationState string

// Authorities and migration states.
const (
	AuthorityAppView    Authority      = "appview"
	AuthorityPDS        Authority      = "pds"
	MigrationNotStarted MigrationState = "notStarted"
	MigrationVerified   MigrationState = "verified"
)

// Status is PDSReadStateStatus, served by getReadStateStatus.
type Status struct {
	Authority       Authority      `json:"authority"`
	MigrationState  MigrationState `json:"migrationState"`
	LegacyRevision  int64          `json:"legacyRevision"`
	Manifest        *Manifest      `json:"manifest,omitempty"`
	ManifestCID     *string        `json:"manifestCid,omitempty"`
	ProjectionReady bool           `json:"projectionReady"`
}

// UnmarshalJSON defaults projectionReady to true when absent.
func (s *Status) UnmarshalJSON(data []byte) error {
	type plain Status
	// Absent or null keeps the preset default.
	value := plain{ProjectionReady: true}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*s = Status(value)
	return nil
}

// LegacyRowKind is ReadStateLegacyRow.Kind.
type LegacyRowKind string

// Legacy row kinds.
const (
	LegacyBoundary LegacyRowKind = "boundary"
	LegacyRead     LegacyRowKind = "read"
	LegacyUnread   LegacyRowKind = "unread"
)

// LegacyRow is one ordered baseline export row: boundaries, then unread
// overrides, then explicit reads.
type LegacyRow struct {
	Kind       LegacyRowKind `json:"kind"`
	ActedAt    string        `json:"actedAt"`
	Boundary   *Boundary     `json:"boundary,omitempty"`
	SubjectURI *string       `json:"subjectUri,omitempty"`
}

// ExportPage is PDSReadStateExportPage.
type ExportPage struct {
	LegacyRevision int64       `json:"legacyRevision"`
	Rows           []LegacyRow `json:"rows"`
	Cursor         *string     `json:"cursor,omitempty"`
}

// MarshalJSON always encodes rows as an array.
func (p ExportPage) MarshalJSON() ([]byte, error) {
	type plain ExportPage
	value := plain(p)
	if value.Rows == nil {
		value.Rows = []LegacyRow{}
	}
	return marshalNoEscape(value)
}
