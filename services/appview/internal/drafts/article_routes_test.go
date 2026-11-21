package drafts

import (
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

func TestArticleRevisionReplayAndOwnerIsolation(t *testing.T) {
	db, err := sqlitedb.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	service, err := newService(db, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	router := httpx.NewRouter()
	service.Register(router.Group())
	const id = "00000000-0000-0000-0000-000000000001"
	const payload = `{"expectedRevision":0,"mutationId":"00000000-0000-0000-0000-000000000002","attachments":[],"draft":{"id":"` + id + `","title":"Draft","markdown":"Hello","excerpt":"","path":"","tags":[],"publicationUri":"","createdAt":"2026-11-20T00:00:00Z","updatedAt":"2026-11-20T00:00:00Z","assets":[]}}`
	call := func(method, route, owner, body string, status int) string {
		t.Helper()
		req := httptest.NewRequest(method, "/xrpc/app.thesocialwire.articleDraft."+route, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(reqctx.WithAuth(req.Context(), &reqctx.Auth{DID: owner}))
		result := httptest.NewRecorder()
		router.ServeHTTP(result, req)
		if result.Code != status {
			t.Fatalf("%s %s: got %d want %d (%s)", method, route, result.Code, status, result.Body.String())
		}
		return result.Body.String()
	}
	// This fixture was sealed with CryptoKit's AES.GCM using the same AAD
	// and sorted JSONEncoder output as Swift's SQLite draft service.
	legacyPayload, err := base64.StdEncoding.DecodeString("AAAAAAAAAAAAAAAAtYUhTj4FHx0ldJ6OltH+ahcBd69T515W64DHvkcwGL/sZ4MYGXrRQXHYYxR3VhTbZSSzguTJD0RDRj/Bhy5Te/L+rUMRmB0HUKWczXGhV5CRn3mA8OOWHdaMMVZkbi2dm8pH/V616wZzktCkl0eYRs1kw5EKf4WfD+iVIrFnAWYrQPiZRPTWwUKehF1P/afvznoX5ItQUorHIddDyWKmW4F6tpHRI4RA0z2XuVMUr6uVFgByQH8QFM0qkE26JgZcVLlI8n151C/YyDexNpNl9oCNIBTGpFsWGD0ULhXo+zhhBDgO8lNr")
	if err != nil {
		t.Fatal(err)
	}
	legacyHash, err := hex.DecodeString("77461de09adf9d4882c81981f5db5703ed172d45a5492eaa9ae25b13b61f4706")
	if err != nil {
		t.Fatal(err)
	}
	const owner = "did:plc:viewer"
	if _, err = db.Writer().ExecContext(t.Context(), `INSERT INTO private_social_draft_accounts(owner) VALUES(?)`, owner); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Writer().ExecContext(t.Context(), `INSERT INTO private_article_drafts(owner,draft_id,revision,deleted,mutation_id,mutation_hash,payload,payload_bytes,updated_at)
        VALUES(?,?,1,0,?,?,?,215,0)`, owner, id, "00000000-0000-0000-0000-000000000002", legacyHash, legacyPayload); err != nil {
		t.Fatal(err)
	}

	first := call(http.MethodPost, "put", "did:plc:viewer", payload, 200)
	replay := call(http.MethodPost, "put", "did:plc:viewer", payload, 200)
	if first != replay {
		t.Fatalf("replay changed response: %s != %s", first, replay)
	}
	call(http.MethodPost, "put", "did:plc:viewer", strings.Replace(payload, "Hello", "Changed", 1), 409)
	call(http.MethodGet, "get?draftId="+id, "did:plc:other", "", 404)
	call(http.MethodGet, "get?draftId="+id, "did:plc:viewer", "", 200)
	call(http.MethodPost, "delete", "did:plc:viewer", `{"draftId":"`+id+`","expectedRevision":1,"mutationId":"00000000-0000-0000-0000-000000000003"}`, 200)
}
