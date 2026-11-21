package publications

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

func TestRSSPublicationKeepsExistingIdentifierAndFiltersPodcasts(t *testing.T) {
	record := atproto.RepoRecord{
		URI:   "at://did:plc:viewer/app.skyreader.feed.subscription/feed",
		Value: map[string]any{"feedUrl": "https://example.org/feed.xml", "title": "Feed", "customTitle": "Favorite Feed"},
	}
	row, ok := rssRow(record, "2026-01-01T00:00:00Z")
	if !ok || row.PublicationID != "rss:aHR0cHM6Ly9leGFtcGxlLm9yZy9mZWVkLnhtbA" ||
		row.Scope.AuthorDID != atproto.RSSAuthorDID || row.Title != "Favorite Feed" ||
		row.SubscriptionPublicationID != record.URI || row.IconURL != "https://example.org/favicon.ico" {
		t.Fatalf("RSS publication: %+v", row)
	}
	record.Value["category"] = " Podcast "
	if _, ok := rssRow(record, "2026-01-01T00:00:00Z"); ok {
		t.Fatal("podcast escaped filtering")
	}
}

func TestProjectionWritesFolderFeedAndReadsSwiftBootstrapSnapshot(t *testing.T) {
	ctx := context.Background()
	db, err := sqlitedb.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, migrate := range []func(context.Context, *sqlitedb.DB) error{thinstore.Migrate, Migrate} {
		if err := migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	service := New(db, nil)
	sidebar := newSidebar("did:plc:viewer")
	row := standardRow(atproto.RepoRecord{
		URI:   "at://did:plc:author/site.standard.publication/news",
		Value: map[string]any{"title": "News", "site": "https://example.org"},
	}, sidebar.RefreshedAt)
	folder := atproto.RepoRecord{URI: "at://did:plc:viewer/app.thesocialwire.folder/inbox", Value: map[string]any{"name": "Inbox"}}
	pref := atproto.RepoRecord{URI: "at://did:plc:viewer/app.thesocialwire.publicationPrefs/news",
		Value: map[string]any{"publicationId": row.PublicationID, "folderId": folder.URI}}
	assembleSidebar(sidebar, map[string]Row{row.PublicationID: row}, map[string]bool{row.PublicationID: true},
		[]atproto.RepoRecord{folder}, []atproto.RepoRecord{pref})
	if len(sidebar.FolderSections) != 1 || len(sidebar.FolderSections[0].Publications) != 1 || len(sidebar.UnfolderedRows) != 0 {
		t.Fatalf("folder membership: %+v", sidebar)
	}
	if len(sidebar.Priority().AllRows) != 0 || len(sidebar.Priority().FolderSections[0].Publications) != 0 {
		t.Fatal("priority projection contains folder articles")
	}
	if err := service.persist(ctx, sidebar); err != nil {
		t.Fatal(err)
	}
	for _, feed := range []struct{ kind, id string }{{"folder", "inbox"}, {"subscribed", ""}, {"publication", row.PublicationID}} {
		var found string
		if err := db.Reader().QueryRow("SELECT publication_id FROM appview_feed_publications WHERE viewer_did=? AND feed_kind=? AND feed_id=?",
			sidebar.ViewerDID, feed.kind, feed.id).Scan(&found); err != nil || found != row.PublicationID {
			t.Fatalf("%s feed scope: %v, %s", feed.kind, err, found)
		}
	}
	swiftSnapshot, err := json.Marshal(map[string]any{"version": 1, "priority": sidebar.Priority(), "folderPayload": map[string]any{
		"folderSections": sidebar.FolderSections, "allPublicationRows": sidebar.AllRows,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Writer().Exec("UPDATE sidebar_projection_cache SET json_body=?, expires_at=? WHERE viewer_did=?",
		swiftSnapshot, time.Now().Add(time.Hour).Format(time.RFC3339), sidebar.ViewerDID); err != nil {
		t.Fatal(err)
	}
	cached, err := service.Cached(ctx, sidebar.ViewerDID)
	if err != nil || cached == nil || len(cached.AllRows) != 1 || len(cached.FolderSections[0].Publications) != 1 {
		t.Fatalf("existing Swift cache: %+v %v", cached, err)
	}
	empty := newSidebar(sidebar.ViewerDID)
	if err := service.persist(ctx, empty); err != nil {
		t.Fatal(err)
	}
	var found string
	if err := db.Reader().QueryRow("SELECT publication_id FROM appview_publication_scopes WHERE viewer_did=?", sidebar.ViewerDID).Scan(&found); err != sql.ErrNoRows {
		t.Fatalf("obsolete scope remained: %v, %s", err, found)
	}
}
