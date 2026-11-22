package operations

import (
	"log/slog"
	"strconv"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

func logTelemetryFailure(logger *slog.Logger, message string, err error, count int) {
	kind, code := sqlitedb.ErrorDiagnostic(err)
	logger.Error(message, "error_type", ErrorCategory(err), "error_kind", kind, "error_code", code, "batch_size", strconv.Itoa(count))
}
