package reader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/publications"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/readstate"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
)

type countingRecordGetter struct {
	calls atomic.Int32
	err   error
}

func (c *countingRecordGetter) GetRecord(context.Context, string, string, string, *string) ([]byte, error) {
	c.calls.Add(1)
	if c.err != nil {
		return nil, c.err
	}
	return []byte(`{"uri":"at://` + testViewer + `/` + readstate.ManifestCollection + `/self","cid":"cid","value":{}}`), nil
}

func serveAs(router http.Handler, method, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader("{}"))
	r = r.WithContext(reqctx.WithAuth(r.Context(), &reqctx.Auth{DID: testViewer}))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}

func TestReadStateGateRefusesProjectionReadsUntilReady(t *testing.T) {
	db := testReaderDB(t)
	if _, err := db.Writer().Exec(`INSERT INTO appview_pds_read_state_authority(viewer_did,manifest_cid,projection_ready)
		VALUES(?, 'cid', 0)`, testViewer); err != nil {
		t.Fatal(err)
	}
	router := httpx.NewRouter()
	publicationsService := publications.New(db, nil)
	gate := Register(router.Group(), db, publicationsService, nil, Options{})
	publications.Register(router.Group().Use(gate), publicationsService)
	for _, route := range [][2]string{
		{http.MethodGet, "/v1/appview/feed"},
		{http.MethodGet, "/v1/appview/bootstrap-stream"},
		{http.MethodGet, "/xrpc/app.thesocialwire.appview.getUnreadCounts"},
		{http.MethodPost, "/xrpc/app.thesocialwire.appview.putReadMark"},
		{http.MethodGet, "/v1/publications/sidebar"},
		{http.MethodPost, "/xrpc/app.thesocialwire.publication.refreshSidebar"},
	} {
		w := serveAs(router, route[0], route[1])
		if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"error":"ReadStateNotReady"`) {
			t.Fatalf("%s %s: got %d %s", route[0], route[1], w.Code, w.Body.String())
		}
	}
	for _, route := range [][2]string{
		{http.MethodGet, readStateStatusPath},
		{http.MethodPost, "/xrpc/app.thesocialwire.appview.exportReadState"},
		{http.MethodPost, "/v1/publications/resolve"},
	} {
		if w := serveAs(router, route[0], route[1]); w.Code == http.StatusServiceUnavailable {
			t.Fatalf("%s %s was gated: %s", route[0], route[1], w.Body.String())
		}
	}
}

func TestReconcileRefreshesReadyViewersAtMostOncePerInterval(t *testing.T) {
	getter := &countingRecordGetter{err: readstate.ErrIncompleteGeneration}
	s := &service{generationFetcher: readstate.NewLiveRecordFetcher(getter), recoverySlots: make(chan struct{}, 2)}
	cid := "cid"
	ready := readstate.Status{Authority: readstate.AuthorityPDS, ManifestCID: &cid, ProjectionReady: true}
	waitIdle := func() {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			s.reconciles.mu.Lock()
			idle := len(s.reconciles.running) == 0
			s.reconciles.mu.Unlock()
			if idle {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("reconcile did not finish")
			}
			time.Sleep(time.Millisecond)
		}
	}
	s.reconcile(testViewer, ready)
	waitIdle()
	s.reconcile(testViewer, ready)
	waitIdle()
	if calls := getter.calls.Load(); calls != 1 {
		t.Fatalf("failed check retried before its backoff: %d fetches", calls)
	}
	s.reconciles.mu.Lock()
	retryAt := s.reconciles.retryAfter[testViewer]
	s.reconciles.retryAfter[testViewer] = time.Now().Add(-time.Second)
	s.reconciles.mu.Unlock()
	if until := time.Until(retryAt); until <= 0 || until > readStateRetryInterval {
		t.Fatalf("retry backoff %v", until)
	}
	getter.err = nil
	s.reconcile(testViewer, ready)
	waitIdle()
	s.reconcile(testViewer, ready)
	waitIdle()
	s.reconciles.mu.Lock()
	_, retrying := s.reconciles.retryAfter[testViewer]
	refreshAt := s.reconciles.refreshAfter[testViewer]
	s.reconciles.mu.Unlock()
	if calls := getter.calls.Load(); calls != 2 || retrying || time.Until(refreshAt) <= readStateRetryInterval {
		t.Fatalf("unchanged manifest: %d fetches, retrying=%v, refresh in %v", calls, retrying, time.Until(refreshAt))
	}
	appView := readstate.Status{Authority: readstate.AuthorityAppView, ProjectionReady: true}
	s.reconcile("did:plc:legacy", appView)
	waitIdle()
	if calls := getter.calls.Load(); calls != 2 {
		t.Fatalf("AppView-authoritative viewer fetched a PDS manifest: %d fetches", calls)
	}
}

func TestBackoffStaysBounded(t *testing.T) {
	now := time.Now()
	backoff := map[string]time.Time{}
	for i := range readStateBackoffLimit + 50 {
		backoff = withBackoff(backoff, strings.Repeat("v", i+1), now, now.Add(time.Minute))
	}
	if len(backoff) > readStateBackoffLimit {
		t.Fatalf("backoff grew to %d entries", len(backoff))
	}
}
