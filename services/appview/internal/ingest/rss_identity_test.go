package ingest

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
	"github.com/mmcdole/gofeed"
)

func TestLegacyRSSKeyResolvesExistingPersistedIdentity(t *testing.T) {
	db, err := sqlitedb.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := t.Context()
	if err := thinstore.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	store := thinstore.SQLiteIngestStore{DB: db}
	const feed = "https://publisher.example/rss"
	item := &gofeed.Item{Title: "An Article", Link: "https://publisher.example/article", GUID: "opaque"}
	content, key := feedContent(feed, item, time.Now())
	legacy := `{"k":"link:https:\/\/publisher.example\/article","f":"https:\/\/publisher.example\/rss"}`
	content.URI = "rssentry:" + base64.RawURLEncoding.EncodeToString([]byte(legacy))
	content.CID = "fixture-swift-existing-cid"
	if err := store.UpsertContentItem(ctx, content); err != nil {
		t.Fatal(err)
	}
	got, err := store.ExistingRSSIdentities(ctx, feed)
	if err != nil {
		t.Fatal(err)
	}
	if got[key].URI != content.URI || got[key].CID != content.CID {
		t.Fatalf("stored identity = %#v", got)
	}
}
