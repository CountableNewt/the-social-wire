package operations

import (
	"context"
	"net/http"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/isotime"
)

// FreshnessResponse is ServiceFreshnessResponse.
type FreshnessResponse struct {
	Service      string       `json:"service"`
	Freshness    HealthState  `json:"freshness"`
	Completeness HealthState  `json:"completeness"`
	CheckedAt    isotime.Time `json:"checkedAt"`
}

// EvaluateFreshness is ServiceFreshnessResponse.evaluate. A nil store reports
// Unknown; otherwise freshness follows the last Jetstream commit (healthy
// within 300 seconds) and completeness degrades on any confirmed gap.
func EvaluateFreshness(ctx context.Context, service string, store *Store, now time.Time) (FreshnessResponse, error) {
	response := FreshnessResponse{Service: service, Freshness: Unknown, Completeness: Unknown, CheckedAt: isotime.Time{Time: now}}
	if store == nil {
		return response, nil
	}
	stream, err := store.FetchStreamState(ctx, "jetstream")
	if err != nil {
		return response, err
	}
	confirmed, err := store.HasConfirmedGap(ctx, 100)
	if err != nil {
		return response, err
	}
	if stream != nil && stream.LastCommittedAt != nil {
		response.Freshness = Degraded
		if now.Sub(*stream.LastCommittedAt) <= 300*time.Second {
			response.Freshness = Healthy
		}
	}
	response.Completeness = Healthy
	if confirmed {
		response.Completeness = Degraded
	}
	return response, nil
}

// FreshnessHandler serves GET /freshness. Pass a nil store unless global
// ingestion is enabled (AppViewRouterBuilder passes the store only when
// GLOBAL_INGESTION_ENABLED is true).
func FreshnessHandler(service string, store *Store) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		response, err := EvaluateFreshness(r.Context(), service, store, time.Now())
		if err != nil {
			return err
		}
		return httpx.OK(w, response)
	}
}
