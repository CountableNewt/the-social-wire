package analytics

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testResponse(body string) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
}

func TestPublicationProviderKeepsUnknownCountsDistinctFromZero(t *testing.T) {
	const author = "did:plc:publication-author"
	const publication = "at://" + author + "/site.standard.publication/publication"
	const article = "at://" + author + "/site.standard.document/article"
	transport := testTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" {
			t.Error("viewer credentials leaked to analytics provider")
		}
		switch r.URL.Host {
		case "plc.directory":
			return testResponse(`{"service":[{"id":"#atproto_pds","type":"AtprotoPersonalDataServer","serviceEndpoint":"https://pds.socialwire.dev"}]}`)
		case "pds.socialwire.dev":
			q := r.URL.Query()
			if q.Get("repo") != author || q.Get("limit") != "100" {
				t.Errorf("wrong repository query: %s", r.URL.RawQuery)
			}
			switch q.Get("collection") {
			case publicationCollection:
				return testResponse(`{"records":[{"uri":"` + publication + `","value":{"$type":"site.standard.publication","name":"Publication","url":"https://publication.social"}}]}`)
			case articleCollection:
				return testResponse(`{"records":[{"uri":"` + article + `","value":{"$type":"site.standard.document","site":"` + publication + `"}}]}`)
			}
		case "constellation.microcosm.blue":
			q := r.URL.Query()
			if r.URL.Path != "/links/count/distinct-dids" {
				t.Errorf("unexpected count URL: %s", r.URL)
			}
			switch q.Get("collection") {
			case "site.standard.graph.subscription":
				if q.Get("target") != publication || q.Get("path") != ".publication" {
					t.Errorf("unexpected subscriber query: %s", r.URL)
				}
				return testResponse(`{"total":0}`)
			case "site.standard.graph.recommend":
				if q.Get("target") != article || q.Get("path") != ".document" {
					t.Errorf("unexpected recommendation query: %s", r.URL)
				}
				return testResponse(`{}`)
			}
		}
		return nil, fmt.Errorf("unexpected analytics request %s", r.URL)
	})
	repo := atproto.NewRepoClient(&http.Client{Transport: transport}, "https://plc.directory", nil)
	provider := Provider{repo: repo}
	summary, err := provider.publications(t.Context(), author, time.Now())
	if err != nil || !summary.InventoryComplete || len(summary.Publications) != 1 {
		t.Fatalf("unexpected summary: %+v, %v", summary, err)
	}
	row := summary.Publications[0]
	if row.Subscribers == nil || *row.Subscribers != 0 || row.Recommendations != nil ||
		row.RecommendationCoverageComplete || row.Documents != 1 {
		t.Fatalf("unknown metrics were reported as zero: %+v", row)
	}
}

func TestBlueskyProviderReportsFeedAndSharesFromCorrectOrigins(t *testing.T) {
	const author = "did:plc:bluesky-author"
	const collaborator = "did:plc:bluesky-collaborator"
	const article = "at://" + author + "/site.standard.document/article"
	const articleURL = "https://my.publication/article"
	at := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	post := func(did, rkey string, reply bool, likes int) string {
		replyField := ""
		if reply {
			replyField = `,"reply":{}`
		}
		return fmt.Sprintf(`{"uri":"at://%s/app.bsky.feed.post/%s","author":{"did":"%s"},"record":{"text":"post","createdAt":"2026-11-19T00:00:00Z"%s},"likeCount":%d,"replyCount":0,"repostCount":0,"quoteCount":0}`, did, rkey, did, replyField, likes)
	}
	transport := testTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/xrpc/app.bsky.actor.getProfile":
			if r.URL.Host != "public.api.bsky.app" || r.URL.Query().Get("actor") != author {
				t.Errorf("wrong profile request: %s", r.URL)
			}
			return testResponse(`{"did":"` + author + `","followersCount":0,"followsCount":-1,"postsCount":2}`)
		case "/xrpc/app.bsky.feed.getAuthorFeed":
			if r.URL.Host != "public.api.bsky.app" || r.URL.Query().Get("includePins") != "false" {
				t.Errorf("wrong author feed request: %s", r.URL)
			}
			return testResponse(`{"feed":[{"post":` + post(author, "post", false, 1) + `},{"post":` + post(author, "reply", true, 2) + `},{"post":` + post(collaborator, "repost", false, 20) + `,"reason":{"$type":"app.bsky.feed.defs#reasonRepost"}}]}`)
		case "/xrpc/app.bsky.feed.searchPosts":
			if r.URL.Host != "api.bsky.app" || r.URL.Query().Get("url") != articleURL {
				t.Errorf("wrong public search provider: %s", r.URL)
			}
			return testResponse(`{"posts":[` + post(collaborator, "share", false, 1) + `]}`)
		}
		return nil, fmt.Errorf("unexpected analytics request %s", r.URL)
	})
	repo := atproto.NewRepoClient(&http.Client{Transport: transport}, "https://plc.directory", nil)
	provider := Provider{repo: repo}
	summary := provider.bluesky(t.Context(), author, 7, []Item{{URI: article, Kind: "article", URL: articleURL}}, at)
	if summary.Profile.Followers == nil || *summary.Profile.Followers != 0 || summary.Profile.Following != nil ||
		summary.Activity == nil || summary.Activity.Posts != 1 || summary.Activity.Replies != 1 ||
		len(summary.TopPosts) != 2 || !strings.HasSuffix(summary.TopPosts[0].URI, "/reply") ||
		!summary.Coverage.FeedComplete || summary.Coverage.FeedPostsExamined != 3 ||
		!summary.Coverage.SharesAvailable || summary.Coverage.ArticlesSearched != 1 || len(summary.Shares) != 1 ||
		len(summary.Shares[0].Posts) != 1 {
		t.Fatalf("unexpected Bluesky summary: %+v", summary)
	}
}
