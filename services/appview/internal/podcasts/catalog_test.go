package podcasts

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

func newPodcastTestService(t *testing.T) (*Service, *httpx.Router) {
	t.Helper()
	database, err := sqlitedb.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := Migrate(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	service := &Service{Database: database, PrivateStorageKey: base64.StdEncoding.EncodeToString(make([]byte, 32))}
	router := httpx.NewRouter()
	service.Register(router.Group(), router)
	return service, router
}

func callPodcast(t *testing.T, router http.Handler, viewer, method, endpoint, body string, want int) string {
	t.Helper()
	request := httptest.NewRequest(method, rpcPrefix+endpoint, strings.NewReader(body))
	request = request.WithContext(reqctx.WithAuth(request.Context(), &reqctx.Auth{DID: viewer}))
	result := httptest.NewRecorder()
	router.ServeHTTP(result, request)
	if result.Code != want {
		t.Fatalf("%s %s returned %d, want %d (%s)", method, endpoint, result.Code, want, result.Body.String())
	}
	return result.Body.String()
}

func TestPrivateCatalogIsolationAndRedaction(t *testing.T) {
	service, router := newPodcastTestService(t)
	owner := "did:plc:owner"
	feed := "https://publisher.example/feed?token=secret"
	showID := "private-podcast:show:" + Identity(owner+"|"+feed)
	episodeID := "private-podcast:episode:fixture"
	result := resolveResult{
		Show: Show{ID: showID, Title: "A Private Show", SourceKind: "private-rss", FeedURL: &feed, Visibility: pointer("private")},
		Episodes: []Episode{{ID: episodeID, ShowID: showID, Title: "A Private Episode", AudioURL: feed, PublishedAt: "2026-11-20T10:00:00Z",
			Visibility: pointer("private"), Description: pointer("Notes at " + feed), Transcripts: []Transcript{{URL: feed, Type: "text/plain"}}}},
	}
	if err := service.savePrivateCatalog(t.Context(), service.privateKey(), owner, feed, result, false); err != nil {
		t.Fatal(err)
	}
	body := callPodcast(t, router, owner, http.MethodGet, "getEpisodes?episodeId="+episodeID, "", 200)
	if strings.Contains(body, "secret") || !strings.Contains(body, "getMedia") || !strings.Contains(body, "Private Link") {
		t.Fatalf("unauthorized source data or missing redaction: %s", body)
	}
	callPodcast(t, router, "did:plc:other", http.MethodGet, "getEpisodes?episodeId="+episodeID, "", 404)
	raw := callPodcast(t, router, owner, http.MethodGet, "getShows", "", 200)
	if strings.Contains(raw, "secret") || !strings.Contains(raw, "A Private Show") {
		t.Fatalf("unexpected show %s", raw)
	}
	callPodcast(t, router, "did:plc:other", http.MethodPost, "deletePrivateSubscription", `{"showId":"`+showID+`"}`, 404)
	callPodcast(t, router, owner, http.MethodPost, "deletePrivateSubscription", `{"showId":"`+showID+`"}`, 200)
	callPodcast(t, router, owner, http.MethodGet, "getEpisodes?episodeId="+episodeID, "", 404)
}

func TestPublicClipLifecycleEnqueuesSharedWorkerJobs(t *testing.T) {
	service, router := newPodcastTestService(t)
	viewer := "did:plc:owner"
	result := resolveResult{
		Show:     Show{ID: "show", Title: "Show", SourceKind: "rss"},
		Episodes: []Episode{{ID: "episode", ShowID: "show", Title: "Episode", PublishedAt: "2026-11-20T10:00:00Z", AudioURL: "https://publisher.example/media.mp3"}},
	}
	if err := service.savePublicCatalog(t.Context(), &result); err != nil {
		t.Fatal(err)
	}
	body := callPodcast(t, router, viewer, http.MethodPost, "createClip", `{"episodeId":"episode","startSeconds":1,"endSeconds":10,"includeCaptions":false}`, 200)
	var clip struct {
		ClipID string `json:"clipId"`
		JobID  string `json:"jobId"`
	}
	if err := json.Unmarshal([]byte(body), &clip); err != nil || clip.ClipID == "" || clip.JobID == "" {
		t.Fatalf("invalid clip: %s", body)
	}
	callPodcast(t, router, viewer, http.MethodGet, "getJob?jobId="+clip.JobID, "", 200)
	callPodcast(t, router, "did:plc:other", http.MethodGet, "getJob?jobId="+clip.JobID, "", 404)
	callPodcast(t, router, viewer, http.MethodPost, "deleteClip", `{"clipId":"`+clip.ClipID+`"}`, 200)
	var count int
	if err := service.Database.Reader().QueryRow("SELECT count(*) FROM podcast_jobs WHERE kind='cleanup' AND dedupe_key=?", "cleanup:"+clip.ClipID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("cleanup jobs = %d, %v", count, err)
	}
}

func TestDirectoryQueryRejectsPrivateInputs(t *testing.T) {
	scope := "directory"
	for _, query := range []string{"https://publisher.example/?token=private", "bearer ABCDEFGH", "secret: test", "me@example.org"} {
		if _, err := ValidateDirectoryQuery(SearchRequest{Scope: &scope, Query: query}); err != ErrDirectoryInvalidQuery {
			t.Errorf("query accepted a private-looking input: %v", err)
		}
	}
	if _, err := ValidateDirectoryQuery(SearchRequest{Scope: &scope, Query: "History of Print"}); err != nil {
		t.Fatal(err)
	}
}
