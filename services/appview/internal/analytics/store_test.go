package analytics

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sqlitedb.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	return NewStore(db, "0123456789abcdef0123456789abcdef")
}

func TestInventorySnapshotsAndCursorRemainCompatible(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	const creator = "did:plc:creator"
	const post = "at://did:plc:creator/app.bsky.feed.post/p"
	const article = "at://did:plc:creator/site.standard.document/a"
	now := time.Date(2026, 11, 20, 18, 1, 0, 100_000_000, time.UTC)
	old := now.Add(-24 * time.Hour)
	if err := s.Save(ctx, creator, []Item{{URI: post, Kind: "post", Engagement: &Engagement{Likes: 1}, RefreshedAt: timestamp(old)}}, true, old); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, creator, []Item{
		{URI: post, Kind: "post", Engagement: &Engagement{Likes: 7}, RefreshedAt: timestamp(now)},
		{URI: article, Kind: "article", PublishedAt: "2026-11-20T13:00:00Z", RefreshedAt: timestamp(now)},
	}, false, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, creator, nil, true, old); err != nil {
		t.Fatal(err)
	}
	output, err := s.Summary(ctx, creator, 7, 1, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Items) != 1 || output.Items[0].URI != post || output.Cursor != post ||
		output.Coverage.TotalItems != 2 || output.Coverage.EngagementKnownItems != 1 ||
		output.Coverage.InventoryComplete || output.Coverage.SampledAt == nil ||
		*output.Coverage.SampledAt != timestamp(now) || output.Totals.Engagement.Likes != 7 {
		t.Fatalf("unexpected inventory summary: %+v", output)
	}
	if len(output.Series) != 2 || *output.Series[0].Likes != 1 || *output.Series[1].Likes != 7 {
		t.Fatalf("daily cumulative snapshots mismatch: %+v", output.Series)
	}
	next, err := s.Summary(ctx, creator, 7, 1, output.Cursor, now)
	if err != nil || len(next.Items) != 1 || next.Items[0].URI != article || next.Cursor != "" {
		t.Fatalf("second page: %+v, %v", next, err)
	}
	articles, err := s.Articles(ctx, creator)
	if err != nil || len(articles) != 1 || articles[0].URI != article {
		t.Fatalf("articles: %+v, %v", articles, err)
	}
	if err := s.Save(ctx, creator, nil, true, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	after, err := s.Summary(ctx, creator, 7, 10, "", now)
	if err != nil || len(after.Items) != 0 || len(after.Series) != 2 || !after.Coverage.InventoryComplete {
		t.Fatalf("complete replacement must preserve snapshots: %+v, %v", after, err)
	}
	if err := Migrate(ctx, s.db); err != nil {
		t.Fatalf("existing GRDB migration should be skipped: %v", err)
	}
}

func TestObservationsRequireImpressionAndDeduplicateAcrossSurfaces(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	const viewer = "did:plc:viewer"
	const creator = "did:plc:creator"
	const uri = "at://did:plc:creator/app.bsky.feed.post/p"
	now := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	for _, observation := range []struct{ viewer, event, surface string }{
		{viewer, "outbound_click", "social-feed"},
		{creator, "impression", "social-feed"},
		{viewer, "impression", "social-feed"},
		{viewer, "impression", "messages"},
		{viewer, "outbound_click", "messages"},
		{viewer, "view", "post-viewer"},
	} {
		err := s.Record(ctx, observation.viewer, creator, Event{SubjectURI: uri, Event: observation.event, Surface: observation.surface}, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	output, err := s.Summary(ctx, creator, 7, 10, "", now)
	if err != nil {
		t.Fatal(err)
	}
	counts := output.Totals.Tracking
	if counts.Impressions != 1 || counts.Views != 1 || counts.OutboundClicks != 1 || counts.CTR == nil || *counts.CTR != 100 ||
		len(output.Series) != 1 || output.Series[0].Likes != nil || len(output.Items) != 0 {
		t.Fatalf("unexpected tracking-only summary: %+v", output)
	}
	mac := hmac.New(sha256.New, []byte(s.secret))
	mac.Write([]byte("creator-analytics\x002026-11-20\x00" + viewer))
	var key string
	if err := s.db.Reader().QueryRow("SELECT viewer_key FROM creator_analytics_events LIMIT 1").Scan(&key); err != nil || key != hex.EncodeToString(mac.Sum(nil)) {
		t.Fatalf("stored viewer key did not match Swift derivation: %q, %v", key, err)
	}
	if err := s.Cleanup(ctx, now.AddDate(0, 0, 31)); err != nil {
		t.Fatal(err)
	}
	var rows int
	if err := s.db.Reader().QueryRow("SELECT count(*) FROM creator_analytics_events").Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("old viewer keys retained: %d, %v", rows, err)
	}
	if err := s.db.Reader().QueryRow("SELECT count(*) FROM creator_analytics_tracking").Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("aggregate removed with viewer keys: %d, %v", rows, err)
	}
}
