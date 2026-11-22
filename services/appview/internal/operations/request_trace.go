package operations

import (
	"errors"
	"math/rand/v2"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
)

var feedRoutes = []string{
	"/v1/appview/feed",
	"/v1/appview/entries",
	"/xrpc/app.thesocialwire.appview.getFeed",
	"/xrpc/app.thesocialwire.appview.listEntries",
}

var feedDurationBucketsMs = []int{50, 100, 150, 250, 500, 1_000, 2_000, 5_000}

// RequestTrace is RequestTraceMiddleware. It assigns a request ID (a safe
// incoming X-Request-ID or a new UUID) and a trace context (a child of a valid
// incoming traceparent, else a new root sampled at 5%, or 10% for
// bootstrap-stream), stores both in the request context via reqctx, echoes
// them as X-Request-ID and traceparent response headers, and records request
// metrics and sampled or failed spans into telemetry (which may be nil).
func RequestTrace(service, environment, instanceID string, telemetry *TelemetryBuffer) httpx.Middleware {
	return func(next httpx.Handler) httpx.Handler {
		return func(w http.ResponseWriter, r *http.Request) error {
			recorder := httpx.Record(w)
			requestID, ok := safeRequestID(r.Header.Get("X-Request-ID"))
			if !ok {
				requestID = newUUID()
			}
			path := r.URL.EscapedPath()
			var trace TraceContext
			if incoming, ok := ParseTraceparent(r.Header.Get("traceparent")); ok {
				trace = incoming.Child()
			} else {
				rate := 5
				if strings.Contains(path, "bootstrap-stream") {
					rate = 10
				}
				trace = NewTraceContext(rand.IntN(100) < rate)
			}
			ctx := reqctx.WithRequestID(r.Context(), requestID)
			ctx = reqctx.WithTrace(ctx, reqctx.Trace{TraceID: trace.TraceID, SpanID: trace.SpanID, Sampled: trace.Sampled})
			// Go handlers stream their own responses, so the echo headers are
			// set before the handler writes rather than after it returns.
			recorder.Header().Set("X-Request-ID", requestID)
			recorder.Header().Set("traceparent", trace.Traceparent())

			startedAt := time.Now()
			err := next(recorder, r.WithContext(ctx))
			duration := time.Since(startedAt)

			status := recorder.Status()
			if status == 0 {
				status = http.StatusOK
			}
			var errorType string
			if err != nil && !recorder.Written() {
				status = httpx.StatusOf(err)
				if status >= 500 {
					errorType = ErrorCategory(err)
				}
				var httpErr *httpx.Error
				if !errors.As(err, &httpErr) {
					// Only HTTP errors carried the echo headers in Swift.
					recorder.Header().Del("X-Request-ID")
					recorder.Header().Del("traceparent")
				}
			}
			recordRequest(telemetryFor(ctx, telemetry), service, environment, instanceID, r, path, requestID, trace,
				startedAt, duration, status, errorType)
			return err
		}
	}
}

func recordRequest(telemetry *TelemetryBuffer, service, environment, instanceID string, r *http.Request,
	path, requestID string, trace TraceContext, startedAt time.Time, duration time.Duration, status int, errorType string) {
	if telemetry == nil {
		return
	}
	route := RouteTemplate(path)
	dimensions := map[string]string{
		"service":           service,
		"environment":       environment,
		"deployment_region": deploymentRegion(),
		"route_template":    route,
		"method":            r.Method,
		"status_class":      strconv.Itoa(status/100) + "xx",
	}
	if kind, ok := feedKind(r, path); ok {
		dimensions["feed_kind"] = kind
		dimensions["page_kind"] = "pagination"
		if _, hasCursor := r.URL.Query()["cursor"]; !hasCursor {
			dimensions["page_kind"] = "first_page"
		}
	}
	seconds := duration.Seconds()
	telemetry.Enqueue(Metric("socialwire.http.server.requests_total", 1, dimensions))
	telemetry.Enqueue(Metric("socialwire.http.server.duration_seconds", seconds, dimensions))
	durationMs := seconds * 1_000
	if slices.Contains(feedRoutes, route) {
		for _, upper := range feedDurationBucketsMs {
			if durationMs <= float64(upper) {
				bucket := cloneWith(dimensions, "le_ms", strconv.Itoa(upper))
				telemetry.Enqueue(Metric("socialwire.appview.feed.duration_bucket", 1, bucket))
			}
		}
	}
	isError := status >= 500
	if !trace.Sampled && !isError {
		return
	}
	attributes := cloneWith(dimensions, "request_id", requestID)
	if errorType != "" {
		attributes["error_type"] = errorType
	}
	attributes["deployment_instance"] = instanceID
	spanStatus, retention := "ok", 7*24*time.Hour
	if isError {
		spanStatus, retention = "error", 30*24*time.Hour
	}
	telemetry.Enqueue(SpanSignal(TraceSpan{
		ID:          newUUID(),
		Environment: environment,
		TraceID:     trace.TraceID,
		Service:     service,
		Name:        service + ".request",
		StartedAt:   startedAt,
		DurationMs:  durationMs,
		Status:      spanStatus,
		Attributes:  attributes,
		ExpiresAt:   startedAt.Add(retention),
	}))
}

func cloneWith(dimensions map[string]string, key, value string) map[string]string {
	result := make(map[string]string, len(dimensions)+1)
	for k, v := range dimensions {
		result[k] = v
	}
	result[key] = value
	return result
}

// safeRequestID accepts a trimmed, non-empty, at most 128-character value of
// printable ASCII (0x21-0x7E).
func safeRequestID(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || utf8.RuneCountInString(trimmed) > 128 {
		return "", false
	}
	for _, r := range trimmed {
		if r < 0x21 || r > 0x7E {
			return "", false
		}
	}
	return trimmed, true
}

// RouteTemplate bounds a request path to a low-cardinality route label.
func RouteTemplate(path string) string {
	components := splitPath(path)
	if len(components) < 2 || components[0] != "v1" {
		return normalizedFallbackPath(path)
	}
	if len(components) >= 4 && components[1] == "operations" &&
		slices.Contains([]string{"gaps", "backfills", "alerts", "traces"}, components[2]) {
		components[3] = ":id"
	} else if len(components) >= 4 && components[1] == "latr" && components[2] == "saves" {
		components[3] = ":rkey"
	}
	return "/" + strings.Join(components, "/")
}

func normalizedFallbackPath(path string) string {
	components := splitPath(path)
	if len(components) > 6 {
		components = components[:6]
	}
	for i, value := range components {
		if utf8.RuneCountInString(value) > 48 || strings.HasPrefix(value, "did:") || strings.HasPrefix(value, "at:") {
			components[i] = ":id"
		}
	}
	return prefixRunes("/"+strings.Join(components, "/"), 160)
}

func splitPath(path string) []string {
	return strings.FieldsFunc(path, func(r rune) bool { return r == '/' })
}

func feedKind(r *http.Request, path string) (string, bool) {
	switch path {
	case "/v1/appview/feed", "/xrpc/app.thesocialwire.appview.getFeed":
		switch kind := httpx.Query(r, "kind"); kind {
		case "subscribed", "following", "folder", "publication":
			return kind, true
		}
		return "invalid", true
	case "/v1/appview/entries", "/xrpc/app.thesocialwire.appview.listEntries":
		return "publication", true
	}
	return "", false
}
