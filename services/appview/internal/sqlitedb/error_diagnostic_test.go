package sqlitedb

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	turso "turso.tech/database/tursogo-serverless"
)

func TestErrorDiagnosticDoesNotExposeProviderMessages(t *testing.T) {
	for _, test := range []struct {
		name       string
		err        error
		kind, code string
	}{
		{"nil", nil, "none", ""},
		{"wrapped busy", remoteError(&turso.Error{Code: "SQLITE_BUSY", Message: "private SQL and secret"}), "turso", "SQLITE_BUSY"},
		{"private code", &turso.Error{Code: "SELECT private_secret", Message: "private SQL"}, "turso", "UNKNOWN"},
		{"control code", &turso.Error{Code: "BUSY\nPRIVATE"}, "turso", "UNKNOWN"},
		{"oversized code", &turso.Error{Code: strings.Repeat("A", 65)}, "turso", "UNKNOWN"},
		{"HTTP", remoteError(&turso.HTTPError{StatusCode: 503, Message: "secret URL"}), "http", "503"},
		{"invalid HTTP", &turso.HTTPError{StatusCode: 999, Message: "private"}, "http", "UNKNOWN"},
		{"canceled", fmt.Errorf("private: %w", context.Canceled), "context", "CANCELED"},
		{"deadline", context.DeadlineExceeded, "context", "DEADLINE_EXCEEDED"},
		{"lost session", remoteError(turso.ErrSessionLost), "turso", "SESSION_LOST"},
		{"unknown", errors.New("private payload"), "unknown", "UNKNOWN"},
	} {
		t.Run(test.name, func(t *testing.T) {
			kind, code := ErrorDiagnostic(test.err)
			if kind != test.kind || code != test.code {
				t.Fatalf("unexpected safe diagnostic: %q %q", kind, code)
			}
		})
	}
}
