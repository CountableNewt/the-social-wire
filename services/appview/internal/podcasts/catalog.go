package podcasts

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
)

func privateID(id string) bool { return strings.HasPrefix(id, "private-podcast:") }

func (s *Service) privateKey() cipher.AEAD {
	key, err := base64.StdEncoding.DecodeString(s.PrivateStorageKey)
	if err != nil || len(key) != 32 {
		return nil
	}
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	return gcm
}

func openPrivate(gcm cipher.AEAD, data, viewer, entity, id string) ([]byte, error) {
	unavailable := httpx.Errorf(http.StatusServiceUnavailable, "Private Podcast Storage Is Unavailable")
	if gcm == nil || !strings.HasPrefix(data, "v1.") {
		return nil, unavailable
	}
	sealed, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(data, "v1."))
	if err != nil || len(sealed) < gcm.NonceSize() {
		return nil, unavailable
	}
	clear, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], privateAAD(viewer, entity, id))
	if err != nil {
		return nil, unavailable
	}
	return clear, nil
}

func privateAAD(viewer, entity, id string) []byte {
	return []byte(fmt.Sprintf("podcast-private:v1:%d:%s:%s:%s", len(viewer), viewer, entity, id))
}

func (s *Service) decodePrivate(data, viewer, entity, id string) (map[string]any, error) {
	clear, err := openPrivate(s.privateKey(), data, viewer, entity, id)
	var result map[string]any
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(clear, &result) != nil || result["id"] != id {
		return nil, httpx.Errorf(http.StatusServiceUnavailable, "Private Podcast Storage Is Unavailable")
	}
	result["visibility"] = "private"
	return result, nil
}

func (s *Service) loadEpisode(ctx context.Context, viewer, id string) (map[string]any, error) {
	var raw string
	if privateID(id) {
		err := s.Database.Reader().QueryRowContext(ctx, `SELECT episode_data FROM podcast_private_episodes WHERE viewer_did=? AND id=?`, viewer, id).Scan(&raw)
		if err != nil {
			return nil, podcastMissing(err)
		}
		return s.decodePrivate(raw, viewer, "episode", id)
	}
	err := s.Database.Reader().QueryRowContext(ctx, `SELECT episode_json FROM podcast_episodes WHERE id=?
		OR id=(SELECT canonical_id FROM podcast_aliases WHERE alias=? AND entity_kind='episode') LIMIT 1`, id, id).Scan(&raw)
	if err != nil {
		return nil, podcastMissing(err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil || result == nil {
		return nil, httpx.Status(http.StatusInternalServerError)
	}
	return result, nil
}

func podcastMissing(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return httpx.ErrNotFound
	}
	return err
}

var privateLink = regexp.MustCompile(`(?i)https?://[^\s<>"']+`)

func redact(item map[string]any, key string) {
	if text, ok := item[key].(string); ok {
		item[key] = privateLink.ReplaceAllString(text, "[Private Link]")
	}
}

func proxyImage(item map[string]any, key, id, kind string, index int, show bool) {
	if src, _ := item[key].(string); src != "" {
		idField := "episodeId"
		if show {
			idField = "showId"
		}
		item[key] = rpcPrefix + "getImage?" + url.Values{idField: {id}, "kind": {kind}, "index": {strconv.Itoa(index)}}.Encode()
	}
}

func visibleEpisode(item map[string]any) map[string]any {
	delete(item, "chapterSourceUrl")
	if item["chapters"] == nil {
		item["chapters"] = []any{}
	}
	if item["visibility"] != "private" {
		return item
	}
	id, _ := item["id"].(string)
	delete(item, "sourceUri")
	delete(item, "guid")
	redact(item, "title")
	redact(item, "description")
	item["audioUrl"] = rpcPrefix + "getMedia?episodeId=" + url.QueryEscape(id)
	proxyImage(item, "artworkUrl", id, "artwork", 0, false)
	proxyImage(item, "showArtworkUrl", id, "showArtwork", 0, false)
	if chapters, ok := item["chapters"].([]any); ok {
		for i, value := range chapters {
			if chapter, ok := value.(map[string]any); ok {
				redact(chapter, "title")
				delete(chapter, "url")
				proxyImage(chapter, "artworkUrl", id, "chapter", i, false)
			}
		}
	}
	if transcripts, ok := item["transcripts"].([]any); ok {
		for _, value := range transcripts {
			if transcript, ok := value.(map[string]any); ok {
				transcript["url"] = ""
				redact(transcript, "text")
				if cues, ok := transcript["cues"].([]any); ok {
					for _, value := range cues {
						if cue, ok := value.(map[string]any); ok {
							redact(cue, "text")
						}
					}
				}
			}
		}
	}
	return item
}

func (s *Service) episodes(w http.ResponseWriter, r *http.Request) error {
	viewer := reqctx.ViewerDID(r.Context())
	if viewer == "" {
		return httpx.ErrUnauthorized
	}
	query := r.URL.Query()
	if id := query.Get("episodeId"); id != "" {
		item, err := s.loadEpisode(r.Context(), viewer, id)
		if err != nil {
			return err
		}
		s.enrichEpisodePage(r.Context(), viewer, []map[string]any{item}, true)
		return httpx.OK(w, map[string]any{"episodes": []any{visibleEpisode(item)}})
	}
	limit, err := strconv.Atoi(query.Get("limit"))
	if err != nil {
		limit = 50
	}
	limit = min(max(1, limit), 100)
	show, cursor := query.Get("showId"), query.Get("cursor")
	const tail = ` SELECT id,payload,is_private FROM catalog
		WHERE (?='' OR (published_at,id)<(SELECT published_at,id FROM catalog WHERE id=?))
		ORDER BY published_at DESC,id DESC LIMIT ?`
	var rows *sql.Rows
	if query.Get("queue") == "true" {
		return s.queue(w, r, viewer)
	}
	db := s.Database.Reader()
	switch {
	case privateID(show):
		if s.privateKey() == nil {
			return httpx.Errorf(http.StatusServiceUnavailable, "Private Podcast Storage Is Unavailable")
		}
		var exists bool
		if err := db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM podcast_private_shows WHERE viewer_did=? AND id=?)`, viewer, show).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return httpx.ErrNotFound
		}
		rows, err = db.QueryContext(r.Context(), `WITH catalog AS(SELECT id,published_at,episode_data AS payload,1 AS is_private
			FROM podcast_private_episodes WHERE viewer_did=? AND show_id=?)`+tail, viewer, show, cursor, cursor, limit)
	case show != "":
		var canonical string
		err := db.QueryRowContext(r.Context(), `SELECT id FROM podcast_shows WHERE id=? OR feed_url=? OR source_uri=?
			OR id=(SELECT canonical_id FROM podcast_aliases WHERE alias=? AND entity_kind='show') LIMIT 1`, show, show, show, show).Scan(&canonical)
		if err != nil {
			return podcastMissing(err)
		}
		rows, err = db.QueryContext(r.Context(), `
            WITH links AS (
                SELECT json_extract(value,'$.protocolShowId') AS show_id
                FROM podcast_viewer_state,json_each(state_json,'$.manualLinks')
                WHERE viewer_did=? AND json_extract(value,'$.rssShowId')=?
            ), candidates AS (
                SELECT e.id,e.published_at,e.episode_json AS payload,e.guid,
                    CASE WHEN e.show_id=? THEN e.id ELSE coalesce((
                        SELECT source.id FROM podcast_episodes source WHERE source.show_id=? AND source.guid=e.guid LIMIT 1
                    ),e.id) END AS mapped_id
                FROM podcast_episodes e
                WHERE e.show_id=? OR e.show_id IN(SELECT show_id FROM links)
            ), ranked AS (
                SELECT *,row_number() OVER (PARTITION BY coalesce(guid,id) ORDER BY published_at DESC,id DESC) AS duplicate
                FROM candidates
            ), catalog AS (
                SELECT mapped_id AS id,published_at,json_set(payload,'$.id',mapped_id) AS payload,0 AS is_private
                FROM ranked WHERE duplicate=1
            )`+tail, viewer, canonical, canonical, canonical, canonical, cursor, cursor, limit)
		if err != nil {
			return err
		}
	default:
		rows, err = db.QueryContext(r.Context(), `WITH catalog AS(SELECT e.id,e.published_at,e.episode_json AS payload,0 AS is_private
			FROM podcast_episodes e JOIN podcast_subscriptions s ON s.show_id=e.show_id WHERE s.viewer_did=?
			UNION ALL SELECT id,published_at,episode_data,1 FROM podcast_private_episodes WHERE viewer_did=? AND ?)`+tail, viewer, viewer, s.privateKey() != nil, cursor, cursor, limit)
	}
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	last := ""
	for rows.Next() {
		var raw, id string
		var private bool
		if err := rows.Scan(&id, &raw, &private); err != nil {
			return err
		}
		var item map[string]any
		if private {
			item, err = s.decodePrivate(raw, viewer, "episode", id)
		} else {
			err = json.Unmarshal([]byte(raw), &item)
		}
		if err != nil || item == nil {
			return httpx.Status(http.StatusInternalServerError)
		}
		items = append(items, item)
		last = id
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	s.enrichEpisodePage(r.Context(), viewer, items, false)
	for _, item := range items {
		visibleEpisode(item)
	}

	result := map[string]any{"episodes": items}
	if len(items) == limit {
		result["cursor"] = last
	}
	return httpx.OK(w, result)
}

func (s *Service) queue(w http.ResponseWriter, r *http.Request, viewer string) error {
	var raw string
	err := s.Database.Reader().QueryRowContext(r.Context(), `SELECT state_json FROM podcast_viewer_state WHERE viewer_did=?`, viewer).Scan(&raw)
	var state struct {
		Queue []string `json:"queue"`
	}
	if !errors.Is(err, sql.ErrNoRows) {
		if err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(raw), &state); err != nil {
			return err
		}
	}
	items := []map[string]any{}
	for _, id := range state.Queue {
		item, err := s.loadEpisode(r.Context(), viewer, id)
		if errors.Is(err, httpx.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		items = append(items, item)
	}
	s.enrichEpisodePage(r.Context(), viewer, items, false)
	for _, item := range items {
		visibleEpisode(item)
	}

	return httpx.OK(w, map[string]any{"episodes": items})
}

func (s *Service) media(w http.ResponseWriter, r *http.Request) error {
	viewer, id := reqctx.ViewerDID(r.Context()), r.URL.Query().Get("episodeId")
	if viewer == "" || id == "" {
		return httpx.ErrNotFound
	}
	item, err := s.loadEpisode(r.Context(), viewer, id)
	if err != nil {
		return err
	}
	source, _ := item["audioUrl"].(string)
	reply, err := atproto.PublicMedia.Stream(r.Context(), source, r.Header.Get("Range"))
	if err != nil {
		return httpx.Errorf(http.StatusBadGateway, "Podcast Media Could Not Be Loaded")
	}
	defer reply.Body.Close()
	if item["visibility"] == "private" && reply.StatusCode != http.StatusOK && reply.StatusCode != http.StatusPartialContent && reply.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		return httpx.Errorf(http.StatusBadGateway, "Private Podcast Media Could Not Be Loaded")
	}

	w.Header().Set("Cache-Control", "private, no-store")
	for _, key := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges"} {
		if value := reply.Header.Get(key); value != "" {
			w.Header().Set(key, value)
		}
	}
	w.WriteHeader(reply.StatusCode)
	if r.Method != http.MethodHead {
		_, err = io.Copy(w, reply.Body)
	}
	return err
}
