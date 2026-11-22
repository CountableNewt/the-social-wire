package operations

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
)

func TestRequestTelemetryFlushesOwnSignalsBeforeInvocationReturns(t *testing.T) {
	exports, signals := 0, 0
	template := NewTelemetryBufferWithExporter(TelemetryBufferOptions{}, func(ctx context.Context, batch []Signal) error { exports++; signals += len(batch); return ctx.Err() })
	router := httpx.NewRouter()
	router.Use(RequestTelemetry(template), RequestTrace("appview", "dev", "function", template))
	router.Get("/sample", func(w http.ResponseWriter, r *http.Request) error {
		telemetryFor(r.Context(), template).Enqueue(Metric("sample", 1, nil))
		w.WriteHeader(http.StatusAccepted)
		return nil
	})
	for range 2 {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/sample", nil))
	}
	if exports != 2 || signals < 6 {
		t.Fatalf("signals not flushed synchronously per invocation: exports=%d signals=%d", exports, signals)
	}
	if template.PendingCount() != 0 {
		t.Fatal("request telemetry escaped into global queue")
	}
}

func TestRequestTelemetryDoesNotExportAfterRequestCancellation(t *testing.T) {
	exports := 0
	template := NewTelemetryBufferWithExporter(TelemetryBufferOptions{}, func(context.Context, []Signal) error { exports++; return nil })
	ctx, cancel := context.WithCancel(context.Background())
	handler := RequestTelemetry(template)(func(_ http.ResponseWriter, r *http.Request) error {
		telemetryFor(r.Context(), template).Enqueue(Metric("sample", 1, nil))
		cancel()
		return nil
	})
	if err := handler(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/sample", nil).WithContext(ctx)); err != nil {
		t.Fatal(err)
	}
	if exports != 0 {
		t.Fatal("canceled request spawned telemetry export")
	}
}
