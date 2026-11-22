package thinstore

import (
	"fmt"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
	"testing"
	"time"
)

func TestRSSFeedBatchCommitsRowsAndValidatorsTogether(t *testing.T) {
	db, err := sqlitedb.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	store := SQLiteIngestStore{DB: db}
	now := time.Date(2026, 11, 22, 1, 2, 3, 456789000, time.UTC)
	feed, tag := "https://publisher.example/rss", "initial"
	items := make([]IndexedContentItem, 130)
	for i := range items {
		items[i] = IndexedContentItem{URI: fmt.Sprintf("rssentry:batch-%d", i), CID: "original", AuthorDID: RssAuthorDID, Collection: "app.skyreader.feed.entry", CreatedAt: now, IndexedAt: now, ExpiresAt: now.Add(time.Hour), PublicationSite: &feed, Render: ContentRenderFields{Title: fmt.Sprint(i), PublishedAt: now.Format(time.RFC3339Nano)}}
	}
	metadata := RssFeedFetchMetadata{NormalizedFeedURL: feed, ETag: &tag, LastPollAt: &now}
	if err := store.StoreRSSFeed(t.Context(), items, metadata); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.Reader().QueryRow("SELECT count(*) FROM content_items").Scan(&count); err != nil || count != 130 {
		t.Fatalf("rows=%d err=%v", count, err)
	}
	tag = "updated"
	items[0].CID = "updated"
	items[0].Render.Title = "Updated"
	if err := store.StoreRSSFeed(t.Context(), items[:1], metadata); err != nil {
		t.Fatal(err)
	}
	var cid, render string
	if err := db.Reader().QueryRow("SELECT cid,render_json FROM content_items WHERE uri=?", items[0].URI).Scan(&cid, &render); err != nil || cid != "updated" {
		t.Fatalf("cid=%s err=%v", cid, err)
	}
	decoded, ok := DecodeRenderFields(render)
	if !ok || decoded.Title != "Updated" {
		t.Fatal("render changed")
	}
	got, err := store.FetchRssFeedMetadata(t.Context(), feed)
	if err != nil || got == nil || got.ETag == nil || *got.ETag != "updated" {
		t.Fatalf("metadata=%+v err=%v", got, err)
	}
	// Reject chunk two after chunk one executed; roll back rows and checkpoint.
	if _, err := db.Writer().Exec(`CREATE TRIGGER reject_rss_batch BEFORE INSERT ON content_items WHEN NEW.cid='reject' BEGIN SELECT RAISE(ABORT,'fixture rejection'); END;`); err != nil {
		t.Fatal(err)
	}
	for i := range items {
		items[i].URI = fmt.Sprintf("rssentry:new-%d", i)
		items[i].CID = "new"
	}
	items[64].CID = "reject"
	tag = "must-not-commit"
	if err := store.StoreRSSFeed(t.Context(), items, metadata); err == nil {
		t.Fatal("failed chunk accepted")
	}
	if err := db.Reader().QueryRow("SELECT count(*) FROM content_items").Scan(&count); err != nil || count != 130 {
		t.Fatalf("partial batch count=%d err=%v", count, err)
	}
	got, err = store.FetchRssFeedMetadata(t.Context(), feed)
	if err != nil || *got.ETag != "updated" {
		t.Fatal("failed checkpoint committed")
	}
	tag = "not-modified"
	if err := store.StoreRSSFeed(t.Context(), nil, metadata); err != nil {
		t.Fatal(err)
	}
	got, err = store.FetchRssFeedMetadata(t.Context(), feed)
	if err != nil || *got.ETag != "not-modified" {
		t.Fatal("304 checkpoint missing")
	}
}
