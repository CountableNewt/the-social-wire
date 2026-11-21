package operations

import (
	"errors"
	"fmt"
)

// Store errors, mirroring OperationsStoreError for the operations this port keeps.
var (
	ErrMissingCreatedRecord = errors.New("operations: missing created record")
	ErrLeaseConflict        = errors.New("operations: lease conflict")
	ErrInvalidProgress      = errors.New("operations: invalid progress")
	ErrNotFound             = errors.New("operations: not found")
)

// EnvironmentMismatchError rejects a record scoped to another environment.
type EnvironmentMismatchError struct {
	Expected string
	Actual   string
}

func (e *EnvironmentMismatchError) Error() string {
	return fmt.Sprintf("operations: environment mismatch (expected %q, actual %q)", e.Expected, e.Actual)
}

// Is matches any EnvironmentMismatchError with the same fields.
func (e *EnvironmentMismatchError) Is(target error) bool {
	other, ok := target.(*EnvironmentMismatchError)
	return ok && other.Expected == e.Expected && other.Actual == e.Actual
}
