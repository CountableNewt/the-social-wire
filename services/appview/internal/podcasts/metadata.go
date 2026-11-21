package podcasts

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"sync"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
)

var embeddedArtwork EmbeddedArtwork

func (s *Service) enrichEpisodePage(ctx context.Context, viewer string, items []map[string]any, embedded bool) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for offset := 0; offset < min(100, len(items)) && ctx.Err() == nil; offset += 8 {
		var active sync.WaitGroup
		for _, episode := range items[offset:min(offset+8, 100, len(items))] {
			active.Go(func() {
				s.enrichEpisode(ctx, viewer, episode, embedded)
			})
		}
		active.Wait()
	}
}

func (s *Service) enrichEpisode(ctx context.Context, viewer string, item map[string]any, embedded bool) {
	var episode Episode
	raw, err := json.Marshal(item)
	if err != nil || json.Unmarshal(raw, &episode) != nil {
		return
	}
	changed := false
	if len(episode.Chapters) == 0 && episode.ChapterSourceURL != nil {
		body, err := atproto.PublicMedia.Fetch(ctx, *episode.ChapterSourceURL, atproto.FetchOptions{MaximumBytes: 2 << 20, Timeout: 3 * time.Second})
		if err == nil {
			episode.Chapters = ParseChapters(body, episode.DurationSeconds)
			changed = len(episode.Chapters) > 0
		}
	}
	if episode.ShowArtworkURL == nil {
		if show, err := s.loadShow(ctx, viewer, episode.ShowID); err == nil {
			if artwork := rowString(show, "artworkUrl"); artwork != nil {
				episode.ShowArtworkURL = artwork
				changed = true
			}
		}
	}
	if changed {
		s.persistMetadata(ctx, viewer, episode)
	}
	if embedded && needsChapterArtwork(episode.Chapters) {
		images, err := embeddedArtwork.Artwork(ctx, episode, viewer, atproto.PublicMedia)
		if err == nil {
			episode = EnrichEmbeddedArtwork(episode, images)
		}
	}
	if episode.ShowArtworkURL != nil {
		item["showArtworkUrl"] = *episode.ShowArtworkURL
	}
	if len(episode.Chapters) > 0 {
		wire, err := json.Marshal(episode.Chapters)
		if err == nil {
			var chapters []any
			if json.Unmarshal(wire, &chapters) == nil {
				item["chapters"] = chapters
			}
		}
	}
}

func needsChapterArtwork(chapters []Chapter) bool {
	for _, chapter := range chapters {
		if chapter.ArtworkURL == nil {
			return true
		}
	}
	return false
}

func (s *Service) persistMetadata(ctx context.Context, viewer string, rendered Episode) {
	_ = s.Database.Write(ctx, func(tx *sql.Tx) error {
		var raw string
		private := privateID(rendered.ID)
		if private {
			if err := tx.QueryRowContext(ctx, `SELECT episode_data FROM podcast_private_episodes WHERE viewer_did=? AND id=?`, viewer, rendered.ID).Scan(&raw); err != nil {
				return err
			}
		} else if err := tx.QueryRowContext(ctx, `SELECT episode_json FROM podcast_episodes WHERE id=?`, rendered.ID).Scan(&raw); err != nil {
			return err
		}
		data := []byte(raw)
		key := s.privateKey()
		if private {
			clear, err := openPrivate(key, raw, viewer, "episode", rendered.ID)
			if err != nil {
				return err
			}
			data = clear
		}
		var prior Episode
		if err := json.Unmarshal(data, &prior); err != nil {
			return err
		}
		if prior.AudioURL != rendered.AudioURL {
			return nil
		}
		if len(prior.Chapters) == 0 && stringPtr(prior.ChapterSourceURL) == stringPtr(rendered.ChapterSourceURL) {
			prior.Chapters = rendered.Chapters
		}
		if prior.ShowArtworkURL == nil {
			prior.ShowArtworkURL = rendered.ShowArtworkURL
		}
		data, err := json.Marshal(prior)
		if err != nil {
			return err
		}
		if private {
			encrypted, err := sealPrivate(key, data, viewer, "episode", prior.ID)
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `UPDATE podcast_private_episodes SET episode_data=? WHERE viewer_did=? AND id=?`, encrypted, viewer, prior.ID)
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE podcast_episodes SET episode_json=? WHERE id=?`, string(data), prior.ID)
		return err
	})
}

func (s *Service) chapterArtwork(ctx context.Context, viewer string, item, chapter map[string]any) (ID3ChapterArtwork, bool) {
	start, ok := chapter["startSeconds"].(float64)
	if !ok {
		return ID3ChapterArtwork{}, false
	}
	raw, err := json.Marshal(item)
	var episode Episode
	if err != nil || json.Unmarshal(raw, &episode) != nil {
		return ID3ChapterArtwork{}, false
	}
	images, err := embeddedArtwork.Artwork(ctx, episode, viewer, atproto.PublicMedia)
	if err == nil {
		for _, image := range images {
			if math.Abs(image.StartSeconds-start) < 0.5 {
				return image, true
			}
		}
	}
	return ID3ChapterArtwork{}, false
}
