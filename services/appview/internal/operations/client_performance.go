package operations

import (
	"math"
	"net/http"
	"slices"
	"strconv"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
)

// clientPerformanceEvents are ClientPerformanceEvent's raw values.
var clientPerformanceEvents = []string{
	"cached_feed_paint", "uncached_feed_paint", "feed_switch", "fresh_merge", "feed_error",
}

// clientPerformanceRequest is ClientPerformanceRequest; every field is required.
type clientPerformanceRequest struct {
	Event       *string  `json:"event"`
	DurationMs  *float64 `json:"durationMs"`
	FeedType    *string  `json:"feedType"`
	CacheState  *string  `json:"cacheState"`
	Outcome     *string  `json:"outcome"`
	Environment *string  `json:"environment"`
}

// RegisterClientPerformance registers POST /v1/telemetry/client-performance
// (ClientPerformanceTelemetryRoutes) on the authenticated group. environment
// is the operations telemetry environment; telemetry may be nil.
func RegisterClientPerformance(g *httpx.Group, telemetry *TelemetryBuffer, environment string) {
	g.Post("/v1/telemetry/client-performance", func(w http.ResponseWriter, r *http.Request) error {
		if reqctx.AuthFrom(r.Context()) == nil {
			return httpx.ErrUnauthorized
		}
		var body clientPerformanceRequest
		if err := httpx.DecodeJSON(r, &body, 0); err != nil {
			return err
		}
		if body.Event == nil || body.DurationMs == nil || body.FeedType == nil ||
			body.CacheState == nil || body.Outcome == nil || body.Environment == nil ||
			!slices.Contains(clientPerformanceEvents, *body.Event) {
			return httpx.Errorf(http.StatusBadRequest, "Invalid request body")
		}
		duration := *body.DurationMs
		if math.IsNaN(duration) || math.IsInf(duration, 0) || duration < 0 || duration > 60_000 ||
			!slices.Contains([]string{"aggregate", "publication"}, *body.FeedType) ||
			!slices.Contains([]string{"hit", "miss"}, *body.CacheState) ||
			!slices.Contains([]string{"success", "error"}, *body.Outcome) ||
			!slices.Contains([]string{"local", "dev", "test", "production"}, *body.Environment) {
			return httpx.Errorf(http.StatusBadRequest, "Invalid client performance sample")
		}
		dimensions := map[string]string{
			"event":             *body.Event,
			"feed_type":         *body.FeedType,
			"cache_state":       *body.CacheState,
			"outcome":           *body.Outcome,
			"environment":       environment,
			"deployment_region": deploymentRegion(),
		}
		telemetry.Enqueue(Metric("socialwire.reader.client.duration_ms", duration, dimensions))
		for _, upper := range feedDurationBucketsMs {
			if duration <= float64(upper) {
				telemetry.Enqueue(Metric("socialwire.reader.client.duration_bucket", 1,
					cloneWith(dimensions, "le_ms", strconv.Itoa(upper))))
			}
		}
		w.WriteHeader(http.StatusAccepted)
		return nil
	})
}
