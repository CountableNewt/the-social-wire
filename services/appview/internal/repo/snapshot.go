package repo

// Snapshot is a verified point-in-time view of selected repository collections.
type Snapshot struct {
	PDSBase   string
	CommitCID string
	Revision  string
	Records   []SnapshotRecord
}

// SnapshotRecord is one verified record: its AT-URI, CID string, and the record
// value as sorted-key ATProto JSON ($bytes/$link encoding).
type SnapshotRecord struct {
	URI   string
	CID   string
	Value []byte
}

// snapshotStatus decodes com.atproto.sync.getRepoStatus strictly (both fields required, typed).
type snapshotStatus struct {
	DID    *string `json:"did"`
	Active *bool   `json:"active"`
}
