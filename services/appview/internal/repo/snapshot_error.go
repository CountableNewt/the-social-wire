package repo

import "errors"

// SnapshotError mirrors Swift's RepositorySnapshotError.invalid(reason).
type SnapshotError struct {
	Reason string
}

func (e *SnapshotError) Error() string { return e.Reason }

// Is matches any SnapshotError with the same reason.
func (e *SnapshotError) Is(target error) bool {
	var other *SnapshotError
	return errors.As(target, &other) && other.Reason == e.Reason
}

// Invalid builds a SnapshotError with the given reason.
func Invalid(reason string) error { return &SnapshotError{Reason: reason} }

// IsSnapshotError reports whether err is (or wraps) a SnapshotError.
func IsSnapshotError(err error) bool {
	var target *SnapshotError
	return errors.As(err, &target)
}
