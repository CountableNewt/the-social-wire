package atproto

import "time"

// PdsCachedRepoRecordPayload is a cached opaque com.atproto.repo.getRecord
// response JSON (stringified). CID "" means absent.
type PdsCachedRepoRecordPayload struct {
	CID      string
	JSONBody string
	CachedAt time.Time
}
