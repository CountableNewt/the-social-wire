package podcasts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
	"github.com/google/uuid"
)

type subscriptionRefresh struct {
	done chan struct{}
	err  error
}

// Hydration preserves the last complete catalog on PDS failures and coalesces
// overlapping requests before they can write projections out of order.
func (s *Service) hydrateSubscriptions(ctx context.Context, viewer string) error {
	refresh := &subscriptionRefresh{done: make(chan struct{})}
	if existing, loaded := s.subscriptionRefreshes.LoadOrStore(viewer, refresh); loaded {
		pending := existing.(*subscriptionRefresh)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-pending.done:
			return pending.err
		}
	}
	defer s.subscriptionRefreshes.Delete(viewer)
	defer close(refresh.done)
	s.scheduleStalePrivate(ctx, viewer)
	refresh.err = s.loadSubscriptions(ctx, viewer)
	return refresh.err
}

func (s *Service) loadSubscriptions(ctx context.Context, viewer string) error {
	client, ok := s.Repo.(repoCatalog)
	if !ok {
		return nil
	}
	type candidate struct{ key, uri string }
	wanted := []candidate{}
	err := walkPodcastRecords(ctx, client, viewer, "app.skyreader.feed.subscription", func(row atproto.RepoRecord) {
		uri, ok := atproto.ParseRecordURI(row.URI)
		if !ok || uri.Repo != viewer || uri.Collection != "app.skyreader.feed.subscription" {
			return
		}
		feed, _ := row.Value["feedUrl"].(string)
		key, _ := row.Value["externalRef"].(string)
		if feed != "" {
			if !publicRSSAllowed(feed) {
				return
			}
			if normalized, ok := thinstore.NormalizeFeedURL(feed); ok {
				key = normalized
			}
		}
		if publicRSSAllowed(key) || strings.HasPrefix(key, "at://") {
			wanted = append(wanted, candidate{key: key, uri: row.URI})
		}
	})
	if err != nil {
		return ctx.Err()
	}
	unknown := []string{}
	err = s.Database.Write(ctx, func(tx *sql.Tx) error {
		resolved := map[string]string{}
		for _, candidate := range wanted {
			var showID, kind string
			var feed sql.NullString
			err := tx.QueryRowContext(ctx, `SELECT id,source_kind,feed_url FROM podcast_shows WHERE id=? OR feed_url=? OR source_uri=?
				OR id=(SELECT canonical_id FROM podcast_aliases WHERE alias=? AND entity_kind='show') LIMIT 1`,
				candidate.key, candidate.key, candidate.key, candidate.key).Scan(&showID, &kind, &feed)
			if errors.Is(err, sql.ErrNoRows) {
				unknown = append(unknown, candidate.key)
				continue
			}
			if err != nil {
				return err
			}
			if kind == "rss" && feed.Valid && !publicRSSAllowed(feed.String) {
				continue
			}
			resolved[showID] = candidate.uri
			if s.BridgeEnabled && kind == "rss" && feed.Valid {
				if err := enqueueBridge(ctx, tx, showID, feed.String); err != nil {
					return err
				}
			}
		}
		ids := make([]string, 0, len(resolved))
		for id, uri := range resolved {
			ids = append(ids, id)
			if _, err := tx.ExecContext(ctx, `INSERT INTO podcast_subscriptions(viewer_did,show_id,source_uri,updated_at)
				VALUES(?,?,?,?) ON CONFLICT(viewer_did,show_id) DO UPDATE SET source_uri=excluded.source_uri,updated_at=excluded.updated_at`,
				viewer, id, uri, podcastNow()); err != nil {
				return err
			}
		}
		raw, err := json.Marshal(ids)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM podcast_subscriptions WHERE viewer_did=? AND show_id NOT IN(SELECT value FROM json_each(?))`, viewer, string(raw))
		return err
	})
	if err == nil {
		s.scheduleCatalogs(unknown)
	}
	return err
}

func enqueueBridge(ctx context.Context, tx *sql.Tx, showID, feed string) error {
	payload, err := json.Marshal(map[string]string{"showId": showID, "feedUrl": feed})
	if err != nil {
		return err
	}
	now := podcastNow()
	_, err = tx.ExecContext(ctx, `INSERT INTO podcast_jobs(id,kind,dedupe_key,payload_json,available_at,created_at,updated_at)
		VALUES(?,'bridge',?,?,?,?,?) ON CONFLICT(dedupe_key) DO NOTHING`, uuid.NewString(), "bridge:"+showID, string(payload), now, now, now)
	return err
}

type catalogQueue struct {
	mu     sync.Mutex
	active map[string]bool
	retry  map[string]time.Time
}

func (s *Service) scheduleCatalogs(keys []string) {
	for _, key := range keys {
		s.scheduleCatalogRefresh("public:"+key, 6*time.Hour, func(ctx context.Context) error {
			_, err := s.resolve(ctx, key)
			return err
		})
	}
}

func (s *Service) scheduleStalePrivate(ctx context.Context, viewer string) {
	key := s.privateKey()
	if key == nil {
		return
	}
	rows, err := s.Database.Reader().QueryContext(ctx, `SELECT id,feed_data FROM podcast_private_shows
        WHERE viewer_did=? AND updated_at<? ORDER BY updated_at,id LIMIT 2`, viewer, podcastNow()-900)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, data string
		if rows.Scan(&id, &data) != nil {
			return
		}
		s.scheduleCatalogRefresh(viewer+"\x00"+id, 15*time.Minute, func(ctx context.Context) error {
			clear, err := openPrivate(key, data, viewer, "feed", id)
			if err != nil {
				return err
			}
			_, err = s.resolvePrivate(ctx, viewer, string(clear), true)
			return err
		})
	}
}

func (s *Service) scheduleCatalogRefresh(key string, backoff time.Duration, execute func(context.Context) error) {
	q := &s.catalogQueue
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.active == nil {
		q.active = map[string]bool{}
		q.retry = map[string]time.Time{}
	}
	now := time.Now()
	if len(q.active) >= 4 || q.active[key] || q.retry[key].After(now) {
		return
	}
	if len(q.retry) >= 2000 {
		for key, when := range q.retry {
			if !when.After(now) {
				delete(q.retry, key)
			}
		}
	}
	delete(q.retry, key)
	q.active[key] = true
	go func() {
		parent := s.Background
		if parent == nil {
			parent = context.Background()
		}
		ctx, cancel := context.WithTimeout(parent, 30*time.Second)
		defer cancel()
		err := execute(ctx)
		q.mu.Lock()
		defer q.mu.Unlock()
		delete(q.active, key)
		if err != nil && len(q.retry) < 2000 {
			q.retry[key] = time.Now().Add(backoff)
		}
	}()
}

func setPodcastPrivateCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "private, no-store")
}

func publicCatalogUnavailable() error {
	return httpx.Errorf(http.StatusBadGateway, "Podcast Repository Could Not Be Loaded")
}
