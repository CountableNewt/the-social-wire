package podcasts

import (
	"context"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
)

func sealPrivate(gcm cipher.AEAD, data []byte, viewer, entity, id string) (string, error) {
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, data, privateAAD(viewer, entity, id))
	return "v1." + base64.StdEncoding.EncodeToString(sealed), nil
}

func (s *Service) privateSubscribe(w http.ResponseWriter, r *http.Request) error {
	viewer := reqctx.ViewerDID(r.Context())
	if viewer == "" {
		return httpx.ErrUnauthorized
	}
	var input struct {
		URL string `json:"url"`
	}
	if err := httpx.DecodeJSON(r, &input, 0); err != nil {
		return err
	}
	result, err := s.resolvePrivate(r.Context(), viewer, strings.TrimSpace(input.URL), false)
	if err != nil {
		return err
	}
	return httpx.OK(w, result)
}

func (s *Service) privateRefresh(w http.ResponseWriter, r *http.Request) error {
	viewer := reqctx.ViewerDID(r.Context())
	if viewer == "" {
		return httpx.ErrUnauthorized
	}
	var input struct {
		ShowID string `json:"showId"`
	}
	if err := httpx.DecodeJSON(r, &input, 0); err != nil {
		return err
	}
	key := s.privateKey()
	if key == nil {
		return httpx.Errorf(http.StatusServiceUnavailable, "Private Podcast Storage Is Unavailable")
	}
	var encrypted string
	err := s.Database.Reader().QueryRowContext(r.Context(), `SELECT feed_data FROM podcast_private_shows WHERE viewer_did=? AND id=?`, viewer, input.ShowID).Scan(&encrypted)
	if err != nil {
		return podcastMissing(err)
	}
	clear, err := openPrivate(key, encrypted, viewer, "feed", input.ShowID)
	if err != nil {
		return err
	}
	result, err := s.resolvePrivate(r.Context(), viewer, string(clear), true)
	if err != nil {
		return err
	}
	return httpx.OK(w, result)
}

func (s *Service) resolvePrivate(ctx context.Context, viewer, target string, existingOnly bool) (map[string]any, error) {
	key := s.privateKey()
	if key == nil {
		return nil, httpx.Errorf(http.StatusServiceUnavailable, "Private Podcast Storage Is Unavailable")
	}
	if !PrivateURLAllowed(target) {
		return nil, httpx.Errorf(http.StatusBadRequest, "Private Feeds Require HTTPS Without URL Userinfo")
	}
	invalid := httpx.Errorf(http.StatusBadRequest, "Private Podcast Feed Could Not Be Loaded")
	result, err := fetchPodcastRSS(ctx, target, PrivateURLAllowed)
	if err != nil {
		return nil, invalid
	}
	show := &result.Show
	show.ID = "private-podcast:show:" + Identity(viewer+"|"+target)
	show.SourceKind = "private-rss"
	show.Visibility = pointer("private")
	show.SourceURI = nil
	for i := range result.Episodes {
		episode := &result.Episodes[i]
		guid := episode.ID
		if episode.Guid != nil {
			guid = *episode.Guid
		}
		episode.ID = "private-podcast:episode:" + Identity(show.ID+"|"+guid)
		episode.ShowID = show.ID
		episode.Visibility = pointer("private")
		episode.SourceURI = nil
	}
	err = s.savePrivateCatalog(ctx, key, viewer, target, result, existingOnly)
	if err != nil {
		if httpx.StatusOf(err) == http.StatusNotFound || httpx.StatusOf(err) == http.StatusServiceUnavailable {
			return nil, err
		}
		return nil, invalid
	}
	showWire, err := jsonObject(show)
	if err != nil {
		return nil, invalid
	}
	episodes := make([]map[string]any, 0, len(result.Episodes))
	for _, episode := range result.Episodes {
		wire, err := jsonObject(episode)
		if err != nil {
			return nil, invalid
		}
		episodes = append(episodes, visibleEpisode(wire))
	}
	return map[string]any{"show": visibleShow(showWire), "episodes": episodes}, nil
}

func jsonObject(from any) (map[string]any, error) {
	data, err := json.Marshal(from)
	if err != nil {
		return nil, err
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, err
	}
	return wire, nil
}

func (s *Service) savePrivateCatalog(ctx context.Context, key cipher.AEAD, viewer, target string, result resolveResult, existingOnly bool) error {
	id := result.Show.ID
	feed, err := sealPrivate(key, []byte(target), viewer, "feed", id)
	if err != nil {
		return err
	}
	body, err := json.Marshal(result.Show)
	if err != nil {
		return err
	}
	show, err := sealPrivate(key, body, viewer, "show", id)
	if err != nil {
		return err
	}
	return s.Database.Write(ctx, func(tx *sql.Tx) error {
		var previous string
		err := tx.QueryRowContext(ctx, `SELECT show_data FROM podcast_private_shows WHERE viewer_did=? AND id=?`, viewer, id).Scan(&previous)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if existingOnly && errors.Is(err, sql.ErrNoRows) {
			return httpx.Errorf(http.StatusNotFound, "Private Podcast Subscription Not Found")
		}
		if err == nil {
			if _, err := openPrivate(key, previous, viewer, "show", id); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO podcast_private_shows(viewer_did,id,feed_hash,feed_data,show_data,updated_at)
			VALUES(?,?,?,?,?,?) ON CONFLICT(viewer_did,id) DO UPDATE SET feed_data=excluded.feed_data,show_data=excluded.show_data,updated_at=excluded.updated_at`,
			viewer, id, Identity(target), feed, show, podcastNow()); err != nil {
			return err
		}
		for _, episode := range result.Episodes {
			if err := savePrivateEpisode(ctx, tx, key, viewer, episode); err != nil {
				return err
			}
		}
		return nil
	})
}

func savePrivateEpisode(ctx context.Context, tx *sql.Tx, key cipher.AEAD, viewer string, episode Episode) error {
	var old string
	err := tx.QueryRowContext(ctx, `SELECT episode_data FROM podcast_private_episodes WHERE viewer_did=? AND id=?`, viewer, episode.ID).Scan(&old)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		clear, err := openPrivate(key, old, viewer, "episode", episode.ID)
		if err != nil {
			return err
		}
		var prior Episode
		if err := json.Unmarshal(clear, &prior); err != nil {
			return err
		}
		if len(episode.Chapters) == 0 && stringPtr(prior.ChapterSourceURL) == stringPtr(episode.ChapterSourceURL) {
			episode.Chapters = prior.Chapters
		}
	}
	body, err := json.Marshal(episode)
	if err != nil {
		return err
	}
	encrypted, err := sealPrivate(key, body, viewer, "episode", episode.ID)
	if err != nil {
		return err
	}
	published, _ := time.Parse(time.RFC3339Nano, episode.PublishedAt)
	seconds := float64(0)
	if !published.IsZero() {
		seconds = float64(published.UnixMicro()) / 1e6
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO podcast_private_episodes(viewer_did,id,show_id,episode_data,published_at)
		VALUES(?,?,?,?,?) ON CONFLICT(viewer_did,id) DO UPDATE SET episode_data=excluded.episode_data,published_at=excluded.published_at`,
		viewer, episode.ID, episode.ShowID, encrypted, seconds)
	return err
}

func (s *Service) privateDelete(w http.ResponseWriter, r *http.Request) error {
	viewer := reqctx.ViewerDID(r.Context())
	if viewer == "" {
		return httpx.ErrUnauthorized
	}
	var input struct {
		ShowID string `json:"showId"`
	}
	if err := httpx.DecodeJSON(r, &input, 0); err != nil {
		return err
	}
	ctx := r.Context()
	key := s.privateKey()
	if key == nil {
		return httpx.Errorf(http.StatusServiceUnavailable, "Private Podcast Storage Is Unavailable")
	}
	err := s.Database.Write(ctx, func(tx *sql.Tx) error {
		var encrypted string
		err := tx.QueryRowContext(ctx, `SELECT show_data FROM podcast_private_shows WHERE viewer_did=? AND id=?`, viewer, input.ShowID).Scan(&encrypted)
		if err != nil {
			return podcastMissing(err)
		}
		if _, err := openPrivate(key, encrypted, viewer, "show", input.ShowID); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT id FROM podcast_private_episodes WHERE viewer_did=? AND show_id=?`, viewer, input.ShowID)
		if err != nil {
			return err
		}
		ids := map[string]bool{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids[id] = true
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM podcast_private_shows WHERE viewer_did=? AND id=?`, viewer, input.ShowID); err != nil {
			return err
		}
		prior, err := readState(ctx, tx, viewer)
		if err != nil || prior.Revision == 0 {
			return err
		}
		if prior.Revision == math.MaxInt64 {
			return httpx.Status(http.StatusConflict)
		}
		queue := make([]string, 0, len(prior.State.Queue))
		for _, id := range prior.State.Queue {
			if !ids[id] {
				queue = append(queue, id)
			}
		}
		prior.State.Queue = queue
		for id := range ids {
			delete(prior.State.Progress, id)
		}
		subs := make([]string, 0, len(prior.State.Subscriptions))
		for _, id := range prior.State.Subscriptions {
			if id != input.ShowID {
				subs = append(subs, id)
			}
		}
		prior.State.Subscriptions = subs
		raw, err := json.Marshal(prior.State)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE podcast_viewer_state SET revision=revision+1,state_json=?,updated_at=? WHERE viewer_did=?`, string(raw), podcastNow(), viewer)
		return err
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{})
}
