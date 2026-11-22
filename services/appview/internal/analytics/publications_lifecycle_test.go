package analytics

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
)

func TestPublicationSummaryCancelsAndJoinsDocumentInventoryOnEarlyReturn(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("publicationFailure=%t", fail), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			started, finished := make(chan struct{}), make(chan struct{})
			transport := testTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host == "plc.directory" {
					return testResponse(`{"service":[{"id":"#atproto_pds","type":"AtprotoPersonalDataServer","serviceEndpoint":"https://pds.socialwire.dev"}]}`)
				}
				switch r.URL.Query().Get("collection") {
				case articleCollection:
					close(started)
					<-r.Context().Done()
					defer close(finished)
					return nil, r.Context().Err()
				case publicationCollection:
					select {
					case <-started:
					case <-ctx.Done():
						return nil, ctx.Err()
					}
					if fail {
						return nil, fmt.Errorf("publication inventory unavailable")
					}
					return testResponse(`{"records":[]}`)
				}
				return nil, fmt.Errorf("unexpected request %s", r.URL)
			})
			repo := atproto.NewRepoClient(&http.Client{Transport: transport}, "https://plc.directory", nil)
			_, err := (Provider{repo: repo}).publications(ctx, "did:plc:publication-author", time.Now())
			if (err != nil) != fail {
				t.Fatalf("summary error: %v", err)
			}
			select {
			case <-finished:
			default:
				t.Fatal("summary returned before document inventory stopped")
			}
			if ctx.Err() != nil {
				t.Fatalf("early return consumed the parent deadline: %v", ctx.Err())
			}
		})
	}
}
