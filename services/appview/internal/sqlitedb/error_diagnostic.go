package sqlitedb

import (
	"context"
	"errors"
	"strconv"

	turso "turso.tech/database/tursogo-serverless"
)

// ErrorDiagnostic exposes bounded machine codes, never provider messages, SQL,
// request URLs or credentials. Unknown or malformed codes remain undisclosed.
func ErrorDiagnostic(err error) (kind, code string) {
	switch {
	case err == nil:
		return "none", ""
	case errors.Is(err, context.Canceled):
		return "context", "CANCELED"
	case errors.Is(err, context.DeadlineExceeded):
		return "context", "DEADLINE_EXCEEDED"
	}
	var server *turso.Error
	if errors.As(err, &server) {
		if validDiagnosticCode(server.Code) {
			return "turso", server.Code
		}
		return "turso", "UNKNOWN"
	}
	var httpError *turso.HTTPError
	if errors.As(err, &httpError) {
		if httpError.StatusCode >= 100 && httpError.StatusCode <= 599 {
			return "http", strconv.Itoa(httpError.StatusCode)
		}
		return "http", "UNKNOWN"
	}
	return "unknown", "UNKNOWN"
}

func validDiagnosticCode(code string) bool {
	if len(code) == 0 || len(code) > 64 {
		return false
	}
	for _, char := range code {
		if (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '_' {
			return false
		}
	}
	return true
}
