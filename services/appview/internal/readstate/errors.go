package readstate

// Error mirrors ReadStateError. Its text is the Swift case name so messages
// that interpolate the error match.
type Error string

func (e Error) Error() string { return string(e) }

// ReadStateError cases.
const (
	ErrInvalidRecord        Error = "invalidRecord"
	ErrInvalidReference     Error = "invalidReference"
	ErrIncompleteGeneration Error = "incompleteGeneration"
	ErrConflictingSequence  Error = "conflictingSequence"
	ErrSizeLimit            Error = "sizeLimit"
)

// StorageError mirrors PDSReadStateStorageError.
type StorageError string

func (e StorageError) Error() string { return string(e) }

// PDSReadStateStorageError cases.
const (
	ErrRevisionChanged        StorageError = "revisionChanged"
	ErrAlreadyMigrated        StorageError = "alreadyMigrated"
	ErrLegacyScopeUnavailable StorageError = "legacyScopeUnavailable"
	ErrLegacyScopeOverlap     StorageError = "legacyScopeOverlap"
	ErrParityMismatch         StorageError = "parityMismatch"
	ErrInvalidCursor          StorageError = "invalidCursor"
	ErrProjectionNotReady     StorageError = "projectionNotReady"
	ErrStaleGeneration        StorageError = "staleGeneration"
)

// IngestionError mirrors AppViewIngestionInboxStoreError, which the
// repository recovery store raises for fenced or malformed rows.
type IngestionError string

func (e IngestionError) Error() string { return string(e) }

// AppViewIngestionInboxStoreError cases.
const (
	ErrStaleLease IngestionError = "staleLease"
	ErrInvalidRow IngestionError = "invalidRow"
)

// DecodeError reports a record whose JSON does not match the typed shape. It
// corresponds to Swift's DecodingError and is deliberately not an Error, so
// callers that map ReadStateError to 409 keep treating it as unexpected.
type DecodeError struct{ Reason string }

func (e *DecodeError) Error() string { return "readstate: decode: " + e.Reason }

func decodeErr(reason string) error { return &DecodeError{Reason: reason} }
