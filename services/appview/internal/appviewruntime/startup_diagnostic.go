package appviewruntime

import (
	"log/slog"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

// Startup runs before HTTP admission, so request timing cannot reveal these
// failures. Only fixed stage names and sanitized machine codes cross this log.
func startupStage(logger *slog.Logger, surface Surface, stage string, run func() error) error {
	started := time.Now()
	err := run()
	kind, code := sqlitedb.ErrorDiagnostic(err)
	level := slog.LevelInfo
	if err != nil {
		level = slog.LevelError
	}
	logger.Log(nil, level, "AppView startup stage", "surface", surface, "stage", stage,
		"duration_ms", time.Since(started).Milliseconds(), "error_kind", kind, "error_code", code)
	return err
}
