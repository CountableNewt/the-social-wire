package podcasts

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
)

type queryStore interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (s *Service) loadShows(ctx context.Context, db queryStore, viewer string) ([]map[string]any, error) {
	rows, err := db.QueryContext(ctx, `WITH catalog AS (
        SELECT s.id,CASE WHEN ? AND s.source_kind='rss' THEN
            json_set(s.show_json,'$.bridgeJobId',j.id,'$.bridgeStatus',j.status)
            ELSE s.show_json END AS payload,0 AS is_private,lower(json_extract(s.show_json,'$.title')) AS title
        FROM podcast_shows s JOIN podcast_subscriptions v ON v.show_id=s.id
        LEFT JOIN podcast_jobs j ON j.dedupe_key='bridge:'||s.id WHERE v.viewer_did=?
		UNION ALL SELECT id,show_data,1,'' FROM podcast_private_shows WHERE viewer_did=? AND ?
	) SELECT id,payload,is_private FROM catalog ORDER BY is_private,title,id`, s.BridgeEnabled, viewer, viewer, s.privateKey() != nil)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	shows := []map[string]any{}
	for rows.Next() {
		var id, raw string
		var private bool
		if err := rows.Scan(&id, &raw, &private); err != nil {
			return nil, err
		}
		var item map[string]any
		if private {
			item, err = s.decodePrivate(raw, viewer, "show", id)
		} else {
			err = json.Unmarshal([]byte(raw), &item)
		}
		if err != nil {
			return nil, err
		}
		if item == nil {
			return nil, httpx.Status(http.StatusInternalServerError)
		}
		shows = append(shows, visibleShow(item))
	}
	return shows, rows.Err()
}

func visibleShow(show map[string]any) map[string]any {
	if show["hosts"] == nil {
		show["hosts"] = []any{}
	}
	if show["visibility"] != "private" {
		return show
	}
	id, _ := show["id"].(string)
	for _, key := range []string{"sourceUri", "feedUrl", "guid", "episodeCollection", "bridgeJobId", "bridgeStatus"} {
		delete(show, key)
	}
	redact(show, "title")
	redact(show, "description")
	proxyImage(show, "artworkUrl", id, "artwork", 0, true)
	if hosts, ok := show["hosts"].([]any); ok {
		for i, value := range hosts {
			if host, ok := value.(map[string]any); ok {
				redact(host, "name")
				delete(host, "url")
				proxyImage(host, "imageUrl", id, "host", i, true)
			}
		}
	}
	return show
}

func (s *Service) shows(w http.ResponseWriter, r *http.Request) error {
	viewer := reqctx.ViewerDID(r.Context())
	if viewer == "" {
		return httpx.ErrUnauthorized
	}
	setPodcastPrivateCache(w)
	if err := s.hydrateSubscriptions(r.Context(), viewer); err != nil {
		return err
	}

	out := []map[string]any{}
	err := s.Database.Read(r.Context(), func(tx *sql.Tx) error {
		shows, err := s.loadShows(r.Context(), tx, viewer)
		if err != nil {
			return err
		}
		snapshot, err := readState(r.Context(), tx, viewer)
		if err != nil {
			return err
		}
		hidden := map[string]bool{}
		for _, link := range snapshot.State.ManualLinks {
			hidden[link.ProtocolShowID] = true
		}
		for _, show := range shows {
			id, _ := show["id"].(string)
			if !hidden[id] {
				out = append(out, show)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"shows": out})
}
