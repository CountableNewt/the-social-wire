package syncapi

import (
	"context"
	"log/slog"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

func logRecordFailure(ctx context.Context, phase string, err error) {
	kind, code := sqlitedb.ErrorDiagnostic(err)
	slog.ErrorContext(ctx, "PDS record cache request failed", "phase", phase, "error_kind", kind, "error_code", code)
}
