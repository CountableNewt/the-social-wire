package thinappviewcore

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestRSSSourceFailurePreservesExactIdentityWithoutDiagnosticLeakage(t *testing.T) {
	first := "https://publisher.social/private-feed?token=FIRST"
	second := "https://publisher.social/other-feed?token=SECOND"
	cause := errors.New("publisher unavailable")
	source := func(feed string) error {
		return WrapRSSSourceFailure(feed, WrapEnrollmentFailure("rss_fetch", feed, 503, cause))
	}
	err := fmt.Errorf("enrollment failed: %w", errors.Join(source(second), source(first), source(first)))
	want := []string{RSSPublicationID(first), RSSPublicationID(second)}
	sort.Strings(want)
	if got := UnavailableRSSPublicationIDs(err); !reflect.DeepEqual(got, want) {
		t.Fatalf("source identities=%v want=%v", got, want)
	}
	if !errors.Is(err, cause) {
		t.Fatal("source wrapper lost error identity")
	}
	counts := EnrollmentFailureCounts(err)
	if len(counts) != 1 || counts[0].Host != "publisher.social" || counts[0].Count != 3 {
		t.Fatalf("diagnostics changed: %+v", counts)
	}
	diagnostics, _ := json.Marshal(counts)
	for _, output := range []string{string(diagnostics), err.Error()} {
		for _, forbidden := range append(want, "private-feed", "other-feed", "FIRST", "SECOND", "token=") {
			if strings.Contains(output, forbidden) {
				t.Fatalf("source identity leaked to diagnostics: %s", output)
			}
		}
	}
}

func TestRSSSourceFailureOmitsHealthyAndUnidentifiedSources(t *testing.T) {
	if WrapRSSSourceFailure("https://publisher.social/healthy", nil) != nil {
		t.Fatal("healthy source became a failure")
	}
	cause := errors.New("source unavailable")
	if WrapRSSSourceFailure("not a valid feed URL", cause) != cause {
		t.Fatal("invalid feed acquired an identity")
	}
	if ids := UnavailableRSSPublicationIDs(errors.Join(cause, nil)); len(ids) != 0 {
		t.Fatalf("unidentified failure acquired source IDs: %v", ids)
	}
	feed := "https://publisher.social/feed"
	err := WrapRSSSourceFailure("http://publisher.social/feed/", cause)
	if ids := UnavailableRSSPublicationIDs(err); !reflect.DeepEqual(ids, []string{RSSPublicationID(feed)}) {
		t.Fatalf("source identity differs from canonical subscription: %v", ids)
	}
}
