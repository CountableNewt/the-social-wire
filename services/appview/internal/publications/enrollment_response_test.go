package publications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stygian-tech/the-social-wire/packages/go/thinappviewcore"
)

func TestEnrollmentResponseIdentifiesOnlyReconciledUpstreamRSSFailures(t *testing.T) {
	source := func(category string, cause error) error {
		return thinappviewcore.WrapEnrollmentFailure(category, "https://user:SECRET@publisher.social/private?token=SECRET", 503, cause)
	}
	upstream := errors.New("Bearer SECRET did:plc:private-account private article")
	for _, tc := range []struct {
		name       string
		err        error
		requestErr error
		partial    bool
	}{
		{"backoff", source("rss_backoff", upstream), nil, true},
		{"fetch", source("rss_fetch", upstream), nil, true},
		{"parse", source("rss_parse", upstream), nil, true},
		{"upstream timeout", source("rss_fetch", context.DeadlineExceeded), nil, true},
		{"multiple sources", errors.Join(source("rss_fetch", upstream), source("rss_parse", upstream)), nil, true},
		{"wrapped source", fmt.Errorf("source failed: %w", source("rss_fetch", upstream)), nil, true},
		{"mixed bare timeout", errors.Join(source("rss_fetch", upstream), context.DeadlineExceeded), nil, false},
		{"mixed unknown", errors.Join(source("rss_fetch", upstream), upstream), nil, false},
		{"request canceled", source("rss_fetch", upstream), context.Canceled, false},
		{"request timeout", source("rss_fetch", upstream), context.DeadlineExceeded, false},
		{"canceled source", source("rss_fetch", context.Canceled), nil, false},
		{"metadata", errors.Join(source("rss_fetch", upstream), source("rss_metadata", upstream)), nil, false},
		{"database cause", source("rss_fetch", &pgconn.PgError{Code: "08006", Message: "SECRET"}), nil, false},
		{"store", source("rss_store", upstream), nil, false},
		{"lease", source("rss_lease", upstream), nil, false},
		{"admission", source("rss_admission", upstream), nil, false},
		{"pds", errors.Join(source("rss_fetch", upstream), source("pds_fetch", upstream)), nil, false},
		{"counter", source("counter_recount", upstream), nil, false},
		{"unknown", upstream, nil, false},
		{"empty", nil, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			failEnrollment(w, tc.err, tc.requestErr)
			if w.Code != http.StatusServiceUnavailable || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d headers=%v", w.Code, w.Header())
			}
			var response struct {
				Error           string                                   `json:"error"`
				Message         string                                   `json:"message"`
				ArticlesUpdated bool                                     `json:"articlesUpdated"`
				SourceFailures  []thinappviewcore.EnrollmentFailureCount `json:"sourceFailures"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Error != "Service Unavailable" || response.Message != "Enrollment unavailable" || response.ArticlesUpdated != tc.partial {
				t.Fatalf("response=%+v", response)
			}
			if (len(response.SourceFailures) > 0) != tc.partial {
				t.Fatalf("unexpected source failures: %+v", response.SourceFailures)
			}
			for _, failure := range response.SourceFailures {
				if failure.Host != "publisher.social" || failure.Status != 503 || failure.Count != 1 {
					t.Fatalf("failure=%+v", failure)
				}
			}
			for _, forbidden := range []string{"SECRET", "private", "did:", "https:", "?", "Bearer"} {
				if strings.Contains(w.Body.String(), forbidden) {
					t.Fatalf("leaked %s", forbidden)
				}
			}
		})
	}
}

func TestEnrollmentResponseReturnsExactUnavailablePublicationIDs(t *testing.T) {
	first := "https://publisher.social/first?token=FIRST"
	second := "https://publisher.social/second?token=SECOND"
	healthy := "https://publisher.social/healthy"
	source := func(feed string) error {
		return thinappviewcore.WrapRSSSourceFailure(feed, thinappviewcore.WrapEnrollmentFailure("rss_fetch", feed, 503, errors.New("publisher unavailable")))
	}
	w := httptest.NewRecorder()
	failEnrollment(w, errors.Join(source(first), source(second), source(first), thinappviewcore.WrapRSSSourceFailure(healthy, nil)), nil)
	var response struct {
		ArticlesUpdated           bool     `json:"articlesUpdated"`
		UnavailablePublicationIDs []string `json:"unavailablePublicationIds"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	want := []string{thinappviewcore.RSSPublicationID(first), thinappviewcore.RSSPublicationID(second)}
	sort.Strings(want)
	if !response.ArticlesUpdated || !reflect.DeepEqual(response.UnavailablePublicationIDs, want) {
		t.Fatalf("response=%+v", response)
	}
	for _, private := range []string{first, second, "token=", "FIRST", "SECOND"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatalf("response exposed raw source URL: %s", w.Body.String())
		}
	}
	for _, err := range []error{errors.Join(source(first), context.DeadlineExceeded), errors.Join(source(first), errors.New("database unavailable"))} {
		w = httptest.NewRecorder()
		failEnrollment(w, err, nil)
		if strings.Contains(w.Body.String(), "unavailablePublicationIds") || strings.Contains(w.Body.String(), "articlesUpdated") {
			t.Fatalf("mixed failure exposed partial source identities: %s", w.Body.String())
		}
	}
}
