package analytics

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

var ErrRateLimit = errors.New("analytics daily observation limit reached")

type Store struct {
	db     *sqlitedb.DB
	secret string
}

func NewStore(db *sqlitedb.DB, secret string) *Store { return &Store{db: db, secret: secret} }

func (s Store) Save(ctx context.Context, author string, items []Item, complete bool, now time.Time) error {
	tx, e := s.db.Writer().BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var previous float64
	e = tx.QueryRowContext(ctx, `SELECT refreshed_at FROM creator_analytics_refresh WHERE author_did=?`, author).Scan(&previous)
	if e != nil && e != sql.ErrNoRows {
		return e
	}
	if previous > unixSeconds(now) {
		return nil
	}
	if complete {
		if _, e = tx.ExecContext(ctx, `DELETE FROM creator_analytics_items WHERE author_did=?`, author); e != nil {
			return e
		}
	}
	for _, it := range items {
		raw, e := json.Marshal(it)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO creator_analytics_items(author_did,subject_uri,item_json,refreshed_at) VALUES(?,?,?,?) ON CONFLICT(author_did,subject_uri) DO UPDATE SET item_json=EXCLUDED.item_json,refreshed_at=EXCLUDED.refreshed_at`, author, it.URI, string(raw), unixSeconds(now)); e != nil {
			return e
		}
		if it.Engagement != nil {
			g := it.Engagement
			if _, e = tx.ExecContext(ctx, `INSERT INTO creator_analytics_snapshots(author_did,subject_uri,day,likes,replies,reposts,quotes) VALUES(?,?,?,?,?,?,?) ON CONFLICT(author_did,subject_uri,day) DO UPDATE SET likes=EXCLUDED.likes,replies=EXCLUDED.replies,reposts=EXCLUDED.reposts,quotes=EXCLUDED.quotes`, author, it.URI, now.UTC().Format("2006-01-02"), g.Likes, g.Replies, g.Reposts, g.Quotes); e != nil {
				return e
			}
		}
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO creator_analytics_refresh(author_did,refreshed_at,inventory_complete) VALUES(?,?,?) ON CONFLICT(author_did) DO UPDATE SET refreshed_at=EXCLUDED.refreshed_at,inventory_complete=EXCLUDED.inventory_complete`, author, unixSeconds(now), complete); e != nil {
		return e
	}
	return tx.Commit()
}
func (s Store) Record(ctx context.Context, viewer, author string, event Event, now time.Time) error {
	if viewer == author {
		return nil
	}
	if len(s.secret) < 32 {
		return errors.New("analytics pseudonym key unavailable")
	}
	day := now.UTC().Format("2006-01-02")
	mac := hmac.New(sha256.New, []byte(s.secret))
	mac.Write([]byte("creator-analytics\x00" + day + "\x00" + viewer))
	key := hex.EncodeToString(mac.Sum(nil))
	tx, e := s.db.Writer().BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var count int
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM creator_analytics_events WHERE day=? AND viewer_key=?`, day, key).Scan(&count); e != nil {
		return e
	}
	if count >= 1000 {
		return ErrRateLimit
	}
	if event.Event == "outbound_click" {
		var seen bool
		if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM creator_analytics_events WHERE day=? AND viewer_key=? AND subject_uri=? AND event='impression')`, day, key, event.SubjectURI).Scan(&seen); e != nil {
			return e
		}
		if !seen {
			return nil
		}
	}
	result, e := tx.ExecContext(ctx, `INSERT INTO creator_analytics_events(author_did,subject_uri,day,viewer_key,event,surface) VALUES(?,?,?,?,?,?) ON CONFLICT DO NOTHING`, author, event.SubjectURI, day, key, event.Event, event.Surface)
	if e != nil {
		return e
	}
	inserted, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if inserted == 0 {
		return tx.Commit()
	}
	impressions, views, clicks := 0, 0, 0
	switch event.Event {
	case "impression":
		impressions = 1
	case "view":
		views = 1
	case "outbound_click":
		clicks = 1
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO creator_analytics_tracking(author_did,subject_uri,day,impressions,views,outbound_clicks) VALUES(?,?,?,?,?,?) ON CONFLICT(author_did,subject_uri,day) DO UPDATE SET impressions=creator_analytics_tracking.impressions+EXCLUDED.impressions,views=creator_analytics_tracking.views+EXCLUDED.views,outbound_clicks=creator_analytics_tracking.outbound_clicks+EXCLUDED.outbound_clicks`, author, event.SubjectURI, day, impressions, views, clicks)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s Store) Summary(ctx context.Context, author string, days, limit int, cursor string, now time.Time) (Summary, error) {
	if days < 1 || days > 90 || limit < 1 || limit > 100 {
		return Summary{}, errors.New("invalid analytics range")
	}
	tx, err := s.db.Reader().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Summary{}, err
	}
	defer tx.Rollback()
	out := Summary{Items: []Item{}, Series: []Day{}, Coverage: Coverage{TrackingScope: "social-wire", SnapshotSemantics: "cumulative-last-observation-per-day"}}
	since := now.UTC().AddDate(0, 0, -days+1).Format("2006-01-02")
	var complete bool
	var sampledAt float64
	e := tx.QueryRowContext(ctx, `SELECT inventory_complete,refreshed_at FROM creator_analytics_refresh WHERE author_did=?`, author).Scan(&complete, &sampledAt)
	if e != nil && e != sql.ErrNoRows {
		return out, e
	}
	out.Coverage.InventoryComplete = complete
	if e == nil {
		formatted := timestamp(time.UnixMicro(int64(math.Round(sampledAt * 1e6))))
		out.Coverage.SampledAt = &formatted
	}
	out.Coverage.ExcludesSelf = true
	out.Coverage.TrackingRetentionDays = 90
	rows, e := tx.QueryContext(ctx, `SELECT item_json FROM creator_analytics_items WHERE author_did=? AND subject_uri>? ORDER BY subject_uri LIMIT ?`, author, cursor, limit+1)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var raw []byte
		if e = rows.Scan(&raw); e != nil {
			rows.Close()
			return out, e
		}
		var it Item
		if e = json.Unmarshal(raw, &it); e != nil {
			rows.Close()
			return out, e
		}
		out.Items = append(out.Items, it)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		out.Cursor = out.Items[len(out.Items)-1].URI
	}
	tracking := map[string]Tracking{}
	rows, e = tx.QueryContext(ctx, `SELECT subject_uri,sum(impressions),sum(views),sum(outbound_clicks) FROM creator_analytics_tracking WHERE author_did=? AND day >= ? GROUP BY subject_uri`, author, since)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var uri string
		var t Tracking
		if e = rows.Scan(&uri, &t.Impressions, &t.Views, &t.OutboundClicks); e != nil {
			rows.Close()
			return out, e
		}
		t.CalculateCTR()
		tracking[uri] = t
		out.Totals.Tracking.Impressions += t.Impressions
		out.Totals.Tracking.Views += t.Views
		out.Totals.Tracking.OutboundClicks += t.OutboundClicks
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	out.Totals.Tracking.CalculateCTR()
	for i := range out.Items {
		out.Items[i].Tracking = tracking[out.Items[i].URI]
	}
	rows, e = tx.QueryContext(ctx, `SELECT item_json FROM creator_analytics_items WHERE author_did=?`, author)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var raw []byte
		if e = rows.Scan(&raw); e != nil {
			rows.Close()
			return out, e
		}
		var it Item
		if e = json.Unmarshal(raw, &it); e != nil {
			rows.Close()
			return out, e
		}
		out.Coverage.TotalItems++
		if it.Engagement != nil {
			out.Coverage.EngagementKnownItems++
			if out.Totals.Engagement == nil {
				out.Totals.Engagement = &Engagement{}
			}
			g := out.Totals.Engagement
			g.Likes += it.Engagement.Likes
			g.Replies += it.Engagement.Replies
			g.Reposts += it.Engagement.Reposts
			g.Quotes += it.Engagement.Quotes
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	var first sql.NullString
	e = tx.QueryRowContext(ctx, `SELECT min(day) FROM creator_analytics_snapshots WHERE author_did=?`, author).Scan(&first)
	if e != nil {
		return out, e
	}
	if first.Valid {
		out.Coverage.SnapshotStart = &first.String
	}
	var trackingFirst sql.NullString
	if e = tx.QueryRowContext(ctx, `SELECT min(day) FROM creator_analytics_tracking WHERE author_did=?`, author).Scan(&trackingFirst); e != nil {
		return out, e
	}
	if trackingFirst.Valid {
		out.Coverage.TrackingStart = &trackingFirst.String
	}
	rows, e = tx.QueryContext(ctx, `WITH days AS (SELECT day FROM creator_analytics_snapshots WHERE author_did=? AND day >= ? UNION SELECT day FROM creator_analytics_tracking WHERE author_did=? AND day >= ?), engagement AS (SELECT day,sum(likes) likes,sum(replies) replies,sum(reposts) reposts,sum(quotes) quotes FROM creator_analytics_snapshots WHERE author_did=? AND day >= ? GROUP BY day), tracking AS (SELECT day,sum(impressions) impressions,sum(views) views,sum(outbound_clicks) clicks FROM creator_analytics_tracking WHERE author_did=? AND day >= ? GROUP BY day) SELECT d.day,e.likes,e.replies,e.reposts,e.quotes,t.impressions,t.views,t.clicks FROM days d LEFT JOIN engagement e USING(day) LEFT JOIN tracking t USING(day) ORDER BY d.day`, author, since, author, since, author, since, author, since)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var d Day
		if e = rows.Scan(&d.Day, &d.Likes, &d.Replies, &d.Reposts, &d.Quotes, &d.Impressions, &d.Views, &d.OutboundClicks); e != nil {
			rows.Close()
			return out, e
		}
		if d.Impressions != nil && d.OutboundClicks != nil {
			t := Tracking{Impressions: *d.Impressions, OutboundClicks: *d.OutboundClicks}
			t.CalculateCTR()
			d.CTR = t.CTR
		}
		out.Series = append(out.Series, d)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	return out, tx.Commit()
}

func (s Store) Articles(ctx context.Context, author string) ([]Item, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
SELECT item_json FROM creator_analytics_items
WHERE author_did=? AND json_extract(item_json,'$.kind')='article'
ORDER BY json_extract(item_json,'$.publishedAt') DESC, subject_uri LIMIT 1000`, author)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Item, 0)
	for rows.Next() {
		var raw []byte
		var item Item
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s Store) Cleanup(ctx context.Context, now time.Time) error {
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		for _, table := range []struct {
			name string
			days int
		}{
			{"creator_analytics_events", 30},
			{"creator_analytics_tracking", 90},
			{"creator_analytics_snapshots", 90},
		} {
			cutoff := now.UTC().AddDate(0, 0, -table.days).Format("2006-01-02")
			query := "DELETE FROM " + table.name + " WHERE rowid IN (SELECT rowid FROM " + table.name + " WHERE day<? ORDER BY day LIMIT 1000)"
			if _, err := tx.ExecContext(ctx, query, cutoff); err != nil {
				return err
			}
		}
		for _, table := range []string{"creator_analytics_items", "creator_analytics_refresh"} {
			query := "DELETE FROM " + table + " WHERE rowid IN (SELECT rowid FROM " + table + " WHERE refreshed_at<? ORDER BY refreshed_at LIMIT 1000)"
			if _, err := tx.ExecContext(ctx, query, unixSeconds(now.UTC().AddDate(0, 0, -90))); err != nil {
				return err
			}
		}
		return nil
	})
}
