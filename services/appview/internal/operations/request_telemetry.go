package operations

import (
	"context"
	"net/http"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
)

type requestTelemetryKey struct{}

func telemetryFor(ctx context.Context, fallback *TelemetryBuffer) *TelemetryBuffer {
	if buffer, ok := ctx.Value(requestTelemetryKey{}).(*TelemetryBuffer); ok {
		return buffer
	}
	return fallback
}

// RequestTelemetry owns and flushes signals before the handler invocation ends.
// No exporter goroutine or cross-request retry queue is required in Functions.
func RequestTelemetry(template *TelemetryBuffer) httpx.Middleware {
	return func(next httpx.Handler) httpx.Handler {
		return func(w http.ResponseWriter, r *http.Request) error {
			if template == nil {
				return next(w, r)
			}
			buffer := NewTelemetryBufferWithExporter(TelemetryBufferOptions{
				Capacity: template.capacity, BatchSize: template.batchSize,
				MaxRetryAttempts: 1, BatchDelay: -1, Logger: template.logger,
			}, template.exporter)
			ctx := context.WithValue(r.Context(), requestTelemetryKey{}, buffer)
			err := next(w, r.WithContext(ctx))
			flush, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			for range 4 {
				if flush.Err() != nil || buffer.PendingCount() == 0 || buffer.FlushOnce(flush) == 0 {
					break
				}
			}
			return err
		}
	}
}
