package podcasts

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
)

func (s *Service) loadShow(ctx context.Context, viewer, id string) (map[string]any, error) {
	var raw string
	if privateID(id) {
		err := s.Database.Reader().QueryRowContext(ctx, `SELECT show_data FROM podcast_private_shows WHERE viewer_did=? AND id=?`, viewer, id).Scan(&raw)
		if err != nil {
			return nil, podcastMissing(err)
		}
		return s.decodePrivate(raw, viewer, "show", id)
	}
	err := s.Database.Reader().QueryRowContext(ctx, `SELECT show_json FROM podcast_shows WHERE id=? OR feed_url=? OR source_uri=?
		OR id=(SELECT canonical_id FROM podcast_aliases WHERE alias=? AND entity_kind='show') LIMIT 1`, id, id, id, id).Scan(&raw)
	if err != nil {
		return nil, podcastMissing(err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil || result == nil {
		return nil, httpx.Status(http.StatusInternalServerError)
	}
	return result, nil
}

func (s *Service) image(w http.ResponseWriter, r *http.Request) error {
	viewer := reqctx.ViewerDID(r.Context())
	if viewer == "" {
		return httpx.ErrUnauthorized
	}
	query := r.URL.Query()
	kind := query.Get("kind")
	if kind == "" {
		kind = "artwork"
	}
	index := 0
	if raw := query.Get("index"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 || value >= 1000 {
			return httpx.ErrBadRequest
		}
		index = value
	}
	var imageURL string
	ctx := r.Context()
	if id := query.Get("showId"); id != "" {
		show, err := s.loadShow(ctx, viewer, id)
		if err != nil {
			return err
		}
		switch kind {
		case "artwork":
			imageURL, _ = show["artworkUrl"].(string)
		case "host":
			host := objectAt(show["hosts"], index)
			imageURL, _ = host["imageUrl"].(string)
		}
	} else if id := query.Get("episodeId"); id != "" {
		episode, err := s.loadEpisode(ctx, viewer, id)
		if err != nil {
			return err
		}
		switch kind {
		case "artwork":
			imageURL, _ = episode["artworkUrl"].(string)
		case "showArtwork":
			imageURL, _ = episode["showArtworkUrl"].(string)
		case "chapter":
			s.enrichEpisodePage(ctx, viewer, []map[string]any{episode}, false)
			chapter := objectAt(episode["chapters"], index)
			imageURL, _ = chapter["artworkUrl"].(string)
			if imageURL == "" || strings.HasPrefix(imageURL, rpcPrefix+"getImage?") {
				if image, ok := s.chapterArtwork(ctx, viewer, episode, chapter); ok {
					return writePodcastImage(w, r, image.MIMEType, image.Data)
				}
				return httpx.ErrNotFound
			}
		}
	}
	if imageURL == "" {
		return httpx.ErrNotFound
	}
	data, err := atproto.PublicMedia.Fetch(ctx, imageURL, atproto.FetchOptions{MaximumBytes: 5 << 20})
	if err != nil {
		return httpx.Errorf(http.StatusBadGateway, "Podcast Image Could Not Be Loaded")
	}
	media := imageType(data)
	if media == "" {
		return httpx.Status(http.StatusUnsupportedMediaType)
	}
	return writePodcastImage(w, r, media, data)
}

func writePodcastImage(w http.ResponseWriter, r *http.Request, media string, data []byte) error {
	w.Header().Set("Content-Type", media)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, err := w.Write(data)
		return err
	}
	return nil
}

func objectAt(raw any, index int) map[string]any {
	rows, ok := raw.([]any)
	if !ok || index >= len(rows) {
		return nil
	}
	object, _ := rows[index].(map[string]any)
	return object
}

func imageType(data []byte) string {
	if len(data) < 24 {
		return ""
	}
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) && string(data[12:16]) == "IHDR":
		return "image/png"
	case bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}) && bytes.HasSuffix(data, []byte{0xff, 0xd9}):
		return "image/jpeg"
	case string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a":
		return "image/gif"
	case string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	case string(data[4:8]) == "ftyp" && (string(data[8:12]) == "avif" || string(data[8:12]) == "avis"):
		return "image/avif"
	}
	return ""
}
