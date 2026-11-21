package reader

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/publications"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/readstate"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
	"github.com/fxamacker/cbor/v2"
)

func TestVerifiedMigrationKeepsReadStateAndLegacyFence(t *testing.T) {
	db := testReaderDB(t)
	ids := seedItems(t, db)
	ctx := context.Background()
	actedAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := changeReadMark(ctx, db, testViewer, ids[2], actedAt, true); err != nil {
		t.Fatal(err)
	}
	legacy, err := pdsStatus(ctx, db.Reader(), testViewer)
	if err != nil || legacy.Authority != readstate.AuthorityAppView {
		t.Fatalf("initial status %+v: %v", legacy, err)
	}
	op := readstate.NewExactOperation("legacy-1", 1, readstate.StateRead, thinstore.CursorTimestamp(actedAt), []string{ids[2]}, nil)
	projection, err := readstate.NewProjection([]readstate.Operation{op}, 1, readstate.ProjectionSource{})
	if err != nil {
		t.Fatal(err)
	}
	manifest := readstate.NewManifestV1("migration", 1, &readstate.Reference{
		URI: "at://" + testViewer + "/" + readstate.ChunkCollection + "/chunk", CID: "chunk-cid",
	}, nil)
	s := service{db: db}
	wrongRevision := legacy.LegacyRevision + 1
	if _, err := s.activateGeneration(ctx, testViewer, "cid", manifest, projection, &wrongRevision); httpx.StatusOf(err) != http.StatusConflict {
		t.Fatalf("stale legacy revision accepted: %v", err)
	}
	status, err := s.activateGeneration(ctx, testViewer, "cid", manifest, projection, &legacy.LegacyRevision)
	if err != nil {
		t.Fatal(err)
	}
	if status.Authority != readstate.AuthorityPDS || !status.ProjectionReady || status.ManifestCID == nil || *status.ManifestCID != "cid" {
		t.Fatalf("activation status: %+v", status)
	}
	pub := testPub
	scope := thinstore.NewPublicationScope(testViewer, pub, testAuthor, &pub, nil, nil, nil, time.Now())
	page, err := listEntries(ctx, db.Reader(), testViewer, []thinstore.PublicationScope{scope}, thinstore.FilterRead, nil, 50, true)
	if err != nil || len(page.Entries) != 1 || page.Entries[0].EntryID != ids[2] {
		t.Fatalf("migrated read projection: %+v; %v", page, err)
	}
	if err := changeReadMark(ctx, db, testViewer, ids[2], actedAt, false); httpx.StatusOf(err) != http.StatusConflict {
		t.Fatalf("migrated state mutation received %v", err)
	}
	router := httpx.NewRouter()
	Register(router.Group(), db, publications.New(db, nil), nil, Options{})
	r := httptest.NewRequest(http.MethodGet, "/xrpc/app.thesocialwire.appview.getReadStateStatus", nil)
	r = r.WithContext(reqctx.WithAuth(r.Context(), &reqctx.Auth{DID: testViewer}))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"authority":"pds"`) {
		t.Fatalf("status response: %d %s", w.Code, w.Body.String())
	}
}

type staticRecordGetter struct {
	payload []byte
}

func (s staticRecordGetter) GetRecord(context.Context, string, string, string, *string) ([]byte, error) {
	return s.payload, nil
}

func TestConfirmRejectsUnverifiedRecordAndActivatesVerifiedManifest(t *testing.T) {
	db := testReaderDB(t)
	ctx := context.Background()
	raw := map[string]any{"$type": readstate.ManifestCollection, "version": 1, "generation": "new", "lastSequence": 0}
	mode, err := cbor.CanonicalEncOptions().EncMode()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := mode.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	prefix := []byte{1, 0x71, 0x12, 0x20}
	cid := "b" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(append(prefix, digest[:]...)))
	recordJSON := func() []byte {
		t.Helper()
		data, err := json.Marshal(map[string]any{"uri": "at://" + testViewer + "/" + readstate.ManifestCollection + "/self", "cid": cid, "value": raw})
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	verified := recordJSON()
	raw["generation"] = "tampered"
	s := service{db: db, generationFetcher: readstate.NewLiveRecordFetcher(staticRecordGetter{recordJSON()}),
		confirmationSlots: make(chan struct{}, 1), verifiedChunks: readstate.NewDefaultVerifiedRecordCache()}
	var revision int64
	if _, err := s.confirmGeneration(ctx, testViewer, cid, &revision); httpx.StatusOf(err) != http.StatusConflict {
		t.Fatalf("tampered manifest was not rejected: %v", err)
	}
	s.generationFetcher = readstate.NewLiveRecordFetcher(staticRecordGetter{verified})
	status, err := s.confirmGeneration(ctx, testViewer, cid, &revision)
	if err != nil || status.Authority != readstate.AuthorityPDS || status.ManifestCID == nil || *status.ManifestCID != cid {
		t.Fatalf("verified manifest: %+v %v", status, err)
	}
}
