package publications

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/stygian-tech/the-social-wire/packages/go/thinappviewcore"
)

// Call only after enrollment counters have been refreshed. An upstream RSS
// failure can leave healthy sources committed; infrastructure failures cannot
// establish that the reader's articles and counts are ready to reconcile.
func failEnrollment(w http.ResponseWriter, err, requestErr error) {
	failures := thinappviewcore.EnrollmentFailureCounts(err)
	partial := err != nil && requestErr == nil && !errors.Is(err, context.Canceled) && allEnrollmentFailuresTyped(err) && len(failures) > 0
	for _, failure := range failures {
		switch failure.Category {
		case "rss_backoff", "rss_fetch", "rss_parse":
		default:
			partial = false
		}
		if failure.Kind == "database" {
			partial = false
		}
	}
	if !partial {
		fail(w, http.StatusServiceUnavailable, "Enrollment unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusServiceUnavailable)
	json.NewEncoder(w).Encode(struct {
		Error                     string                                   `json:"error"`
		Message                   string                                   `json:"message"`
		ArticlesUpdated           bool                                     `json:"articlesUpdated"`
		SourceFailures            []thinappviewcore.EnrollmentFailureCount `json:"sourceFailures"`
		UnavailablePublicationIDs []string                                 `json:"unavailablePublicationIds,omitempty"`
	}{http.StatusText(http.StatusServiceUnavailable), "Enrollment unavailable", true, failures, thinappviewcore.UnavailableRSSPublicationIDs(err)})
}

// Diagnostics intentionally omit bare context leaves. Do not let that omission
// turn a mixed request/infrastructure failure into a partial source result.
func allEnrollmentFailuresTyped(err error) bool {
	if _, ok := err.(*thinappviewcore.EnrollmentFailure); ok {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if child == nil || !allEnrollmentFailuresTyped(child) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		child := wrapped.Unwrap()
		return child != nil && allEnrollmentFailuresTyped(child)
	}
	return false
}
