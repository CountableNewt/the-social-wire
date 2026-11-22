package operations

import (
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

// EvaluateFreshness reports unknown ingestion health because this service no
// longer follows a global ingestion stream.
func EvaluateFreshness(service string, now time.Time) FreshnessResponse {
	return FreshnessResponse{Service: service, Freshness: Unknown, Completeness: Unknown, CheckedAt: isotime.Time{Time: now}}
}

// FreshnessHandler serves GET /freshness without consulting retired stream state.
func FreshnessHandler(service string) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		return httpx.OK(w, EvaluateFreshness(service, time.Now()))
	}
}
