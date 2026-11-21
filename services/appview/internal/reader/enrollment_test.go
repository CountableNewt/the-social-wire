package reader

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

func TestPartialEnrollmentReportsDistinctUnavailableSources(t *testing.T) {
	response := httptest.NewRecorder()
	err := enrollmentPartialResponse(response, &thinstore.EnrollmentPartialFailure{Failures: []*thinstore.RSSSourceFailure{
		{PublicationID: "rss:b", Category: "rss_fetch", Host: "fixture.example"},
		{PublicationID: "rss:a", Category: "rss_fetch", Host: "fixture.example"},
		{PublicationID: "rss:b", Category: "rss_fetch", Host: "fixture.example"},
	}})
	if err != nil || response.Code != http.StatusServiceUnavailable || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected partial response: %d %v", response.Code, err)
	}
	var body struct {
		ArticlesUpdated           bool               `json:"articlesUpdated"`
		SourceFailures            []enrollmentSource `json:"sourceFailures"`
		UnavailablePublicationIDs []string           `json:"unavailablePublicationIds"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.ArticlesUpdated || !slices.Equal(body.UnavailablePublicationIDs, []string{"rss:a", "rss:b"}) ||
		len(body.SourceFailures) != 1 || body.SourceFailures[0].Count != 2 {
		t.Fatalf("unexpected partial response body: %+v", body)
	}
}
