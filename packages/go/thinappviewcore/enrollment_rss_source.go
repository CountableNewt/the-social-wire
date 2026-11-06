package thinappviewcore

import "sort"

// The publication identity is only for the authenticated enrollment response.
// Keep it out of error text and operational diagnostics: RSS IDs encode feed URLs
// and may contain private subscription paths or query parameters.
type enrollmentRSSSourceFailure struct {
	publicationID string
	cause         error
}

func (e *enrollmentRSSSourceFailure) Error() string { return e.cause.Error() }
func (e *enrollmentRSSSourceFailure) Unwrap() error { return e.cause }

// WrapRSSSourceFailure must receive only a feed selected from the viewer's
// subscriptions, after admission has settled that feed's ingestion attempt.
func WrapRSSSourceFailure(feed string, cause error) error {
	if cause == nil {
		return nil
	}
	normalized := NormalizeFeedURL(feed)
	if normalized == nil {
		return cause
	}
	return &enrollmentRSSSourceFailure{publicationID: RSSPublicationID(*normalized), cause: cause}
}

// UnavailableRSSPublicationIDs is for an authenticated viewer response, never
// logs. Unlike host diagnostics, these IDs distinguish feeds on the same host.
func UnavailableRSSPublicationIDs(err error) []string {
	ids := map[string]struct{}{}
	var visit func(error)
	visit = func(err error) {
		if err == nil {
			return
		}
		if source, ok := err.(*enrollmentRSSSourceFailure); ok {
			ids[source.publicationID] = struct{}{}
		}
		if joined, ok := err.(interface{ Unwrap() []error }); ok {
			for _, child := range joined.Unwrap() {
				visit(child)
			}
		} else if wrapped, ok := err.(interface{ Unwrap() error }); ok {
			visit(wrapped.Unwrap())
		}
	}
	visit(err)
	result := make([]string, 0, len(ids))
	for id := range ids {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}
