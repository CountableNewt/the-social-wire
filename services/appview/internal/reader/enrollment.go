package reader

import (
	"cmp"
	"maps"
	"net/http"
	"slices"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

type enrollmentSource struct {
	Category string `json:"category"`
	Kind     string `json:"kind"`
	Host     string `json:"host"`
	Count    int    `json:"count"`
}

func enrollmentPartialResponse(w http.ResponseWriter, partial *thinstore.EnrollmentPartialFailure) error {
	type sourceKey struct{ category, host string }
	groups := make(map[sourceKey]map[string]struct{})
	publications := make(map[string]struct{})
	for _, failure := range partial.Failures {
		key := sourceKey{failure.Category, failure.Host}
		if groups[key] == nil {
			groups[key] = make(map[string]struct{})
		}
		publications[failure.PublicationID] = struct{}{}
		groups[key][failure.PublicationID] = struct{}{}
	}
	sources := make([]enrollmentSource, 0, len(groups))
	for key, ids := range groups {
		sources = append(sources, enrollmentSource{Category: key.category, Host: key.host, Kind: "other", Count: len(ids)})
	}
	slices.SortFunc(sources, func(a, b enrollmentSource) int {
		return cmp.Or(cmp.Compare(a.Category, b.Category), cmp.Compare(a.Host, b.Host))
	})
	w.Header().Set("Cache-Control", "no-store")
	return httpx.JSON(w, http.StatusServiceUnavailable, map[string]any{
		"error": "Service Unavailable", "message": "Enrollment unavailable", "articlesUpdated": true,
		"sourceFailures": sources, "unavailablePublicationIds": slices.Sorted(maps.Keys(publications)),
	})
}
