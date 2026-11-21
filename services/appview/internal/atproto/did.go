package atproto

import "strings"

// NormalizeDID trims whitespace and validates the did:<method>:<id> shape.
func NormalizeDID(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	rest, ok := strings.CutPrefix(trimmed, "did:")
	if !ok {
		return "", ErrInvalidDID
	}
	method, id, found := strings.Cut(rest, ":")
	if !found || method == "" || id == "" {
		return "", ErrInvalidDID
	}
	return trimmed, nil
}

// IsValidDID reports whether raw normalizes as a DID.
func IsValidDID(raw string) bool {
	_, err := NormalizeDID(raw)
	return err == nil
}
