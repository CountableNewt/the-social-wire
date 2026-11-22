package jobs

import (
	"log/slog"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

func logFailure(job, phase string, err error) {
	if err == nil {
		return
	}
	kind, code := sqlitedb.ErrorDiagnostic(err)
	slog.Error("Maintenance job failed", "job", job, "phase", phase, "error_kind", kind, "error_code", code)
}
