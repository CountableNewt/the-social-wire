package thinstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

// Only an already-initialized disposable probe may execute this opt-in test.
// All fixture rows and trigger DDL roll back in the production write wrapper.
func TestLiveTursoRSSBatchRollback(t *testing.T) {
	dir := os.Getenv("TSW_TURSO_PROBE_DIR")
	if dir == "" {
		t.Skip("set disposable probe profile for RSS batch engine acceptance")
	}
	read := func(name string) string {
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			t.Fatal("probe profile must be owner-only regular files")
		}
		value, err := os.ReadFile(path)
		if err != nil {
			t.Fatal("probe profile unavailable")
		}
		return strings.TrimSpace(string(value))
	}
	endpoint, token := read("appview-url"), read("appview-token")
	parsed, err := url.Parse(endpoint)
	if err != nil || !strings.Contains(parsed.Hostname(), "-probe-") {
		t.Fatal("RSS batch acceptance requires disposable probe database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	db, err := sqlitedb.OpenTurso(ctx, endpoint, token)
	if err != nil {
		t.Fatal("probe database unavailable")
	}
	defer db.Close()
	now := time.Now().UTC()
	identity := fmt.Sprintf("rss_bulk_probe_%d", now.UnixNano())
	feed, tag := "https://"+identity+".invalid/rss", "probe"
	items := make([]IndexedContentItem, 130)
	for i := range items {
		items[i] = IndexedContentItem{URI: fmt.Sprintf("rssentry:%s-%d", identity, i), CID: "original", AuthorDID: RssAuthorDID, Collection: "app.skyreader.feed.entry", CreatedAt: now, IndexedAt: now, ExpiresAt: now.Add(time.Hour), PublicationSite: &feed, Render: ContentRenderFields{Title: "Synthetic Probe", PublishedAt: now.Format(time.RFC3339Nano)}}
	}
	metadata := RssFeedFetchMetadata{NormalizedFeedURL: feed, ETag: &tag, LastPollAt: &now}
	rollback := errors.New("intentional RSS probe rollback")
	phase := "transaction begin"
	started := time.Now()
	err = db.Write(ctx, func(tx *sql.Tx) error {
		phase = "130-row initial batch"
		if err := storeRSSFeed(ctx, tx, items, metadata); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM content_items WHERE publication_site=?", feed).Scan(&count); err != nil {
			return err
		}
		if count != 130 {
			return errors.New("probe batch row mismatch")
		}
		phase = "upsert"
		items[0].CID = "updated"
		if err := storeRSSFeed(ctx, tx, items[:1], metadata); err != nil {
			return err
		}
		var cid string
		if err := tx.QueryRowContext(ctx, "SELECT cid FROM content_items WHERE uri=?", items[0].URI).Scan(&cid); err != nil {
			return err
		}
		if cid != "updated" {
			return errors.New("probe upsert mismatch")
		}
		phase = "empty batch metadata"
		if err := storeRSSFeed(ctx, tx, nil, metadata); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM rss_feed_fetch_metadata WHERE feed_url=?", feed).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return errors.New("probe metadata mismatch")
		}
		// Verify the engine still executes row triggers for multi-row statements.
		phase = "synthetic row trigger"
		_, err := tx.ExecContext(ctx, "CREATE TRIGGER "+identity+" BEFORE INSERT ON content_items WHEN NEW.cid='"+identity+"' BEGIN SELECT RAISE(ABORT,'synthetic RSS rejection'); END")
		if err != nil {
			return err
		}
		phase = "trigger-enforced batch rollback"
		items[64].CID = identity
		if err := storeRSSFeed(ctx, tx, items, metadata); err == nil {
			return errors.New("probe row trigger bypassed")
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		var chain []string
		for cause := err; cause != nil; cause = errors.Unwrap(cause) {
			chain = append(chain, fmt.Sprintf("%T: %s", cause, cause))
		}
		_ = os.WriteFile(filepath.Join(dir, "rss-batch-diagnostic.log"), []byte(phase+"\n"+strings.Join(chain, "\n")), 0600)
		t.Fatalf("probe RSS batch failed during %s; private diagnostic saved", phase)
	}
	var count int
	if err := db.Reader().QueryRowContext(ctx, "SELECT count(*) FROM content_items WHERE publication_site=?", feed).Scan(&count); err != nil || count != 0 {
		t.Fatal("probe content rollback failed")
	}
	if err := db.Reader().QueryRowContext(ctx, "SELECT count(*) FROM rss_feed_fetch_metadata WHERE feed_url=?", feed).Scan(&count); err != nil || count != 0 {
		t.Fatal("probe metadata rollback failed")
	}
	if err := db.Reader().QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE name=?", identity).Scan(&count); err != nil || count != 0 {
		t.Fatal("probe trigger rollback failed")
	}
	t.Logf("130-row RSS chunks, upsert, validators, trigger enforcement and full rollback passed in %s", time.Since(started).Round(time.Millisecond))
}
