package podcasts

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
	"github.com/google/uuid"
)

func (s *Service) createClip(w http.ResponseWriter, r *http.Request) error {
	viewer := reqctx.ViewerDID(r.Context())
	if viewer == "" {
		return httpx.ErrUnauthorized
	}
	var input struct {
		Episode  string   `json:"episodeId"`
		Start    *float64 `json:"startSeconds"`
		End      *float64 `json:"endSeconds"`
		Title    *string  `json:"title"`
		Captions *bool    `json:"includeCaptions"`
	}
	if err := httpx.DecodeJSON(r, &input, 0); err != nil {
		return err
	}
	ctx := r.Context()
	episode, err := s.loadEpisode(ctx, viewer, input.Episode)
	invalid := httpx.Errorf(http.StatusBadRequest, "Invalid Clip Range")
	if err != nil {
		if !privateID(input.Episode) && errors.Is(err, httpx.ErrNotFound) {
			return invalid
		}
		return err
	}
	if privateID(input.Episode) || episode["visibility"] == "private" {
		return httpx.Errorf(http.StatusForbidden, "Clipping Private Podcasts Is Not Supported")
	}
	if input.Start == nil || input.End == nil || *input.Start < 0 || *input.End <= *input.Start ||
		*input.End-*input.Start > 600 || *input.End*1000 >= float64(math.MaxInt64) {
		return invalid
	}
	if duration, ok := episode["durationSeconds"].(float64); ok && *input.End > duration {
		return invalid
	}
	showID, _ := episode["showId"].(string)
	var show string
	if err := s.Database.Reader().QueryRowContext(ctx, `SELECT show_json FROM podcast_shows WHERE id=?`, showID).Scan(&show); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return invalid
		}
		return err
	}
	id, jobID := uuid.NewString(), uuid.NewString()
	episodeID, _ := episode["id"].(string)
	title, _ := episode["title"].(string)
	if input.Title != nil {
		title = *input.Title
	}
	now := time.Now()
	clip := map[string]any{"id": id, "episodeId": episodeID, "title": title, "status": "queued", "createdAt": now.UTC().Format(time.RFC3339),
		"startSeconds": *input.Start, "endSeconds": *input.End}
	if source, ok := episode["sourceUri"].(string); ok {
		clip["sourceUri"] = source
	}
	if input.Captions != nil && !*input.Captions {
		episode["transcripts"] = []any{}
	} else {
		s.hydrateTranscripts(ctx, episode)
	}
	payload, err := json.Marshal(map[string]any{"episode": episode, "show": json.RawMessage(show), "clipId": id, "startSeconds": *input.Start, "endSeconds": *input.End, "title": title})
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(clip)
	if err != nil {
		return err
	}
	seconds := float64(now.UnixMicro()) / 1e6
	err = s.Database.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO podcast_clips(id,viewer_did,episode_id,clip_json,created_at,updated_at)
			VALUES(?,?,?,?,?,?)`, id, viewer, episodeID, string(metadata), seconds, seconds); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO podcast_jobs(id,viewer_did,episode_id,kind,dedupe_key,payload_json,available_at,created_at,updated_at)
			VALUES(?,?,?,'clip',?,?,?,?,?)`, jobID, viewer, episodeID, "clip:"+id, string(payload), seconds, seconds, seconds)
		return err
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]string{"clipId": id, "jobId": jobID, "status": "queued"})
}

func (s *Service) publishClip(w http.ResponseWriter, r *http.Request) error {
	viewer := reqctx.ViewerDID(r.Context())
	if viewer == "" {
		return httpx.ErrUnauthorized
	}
	var input struct {
		ID  string `json:"clipId"`
		URI string `json:"uri"`
	}
	if err := httpx.DecodeJSON(r, &input, 0); err != nil {
		return err
	}
	ctx := r.Context()
	clip, _, err := s.loadClip(ctx, input.ID, viewer, false)
	parsed, ok := atproto.ParseRecordURI(input.URI)
	if err != nil || clip.Status != "complete" || !ok || parsed.Repo != viewer || parsed.Collection != "app.thesocialwire.podcast.clip" || s.Repo == nil {
		return httpx.Errorf(http.StatusBadRequest, "Clip Record Unavailable")
	}
	record, err := s.Repo.GetRecordByATURI(ctx, nil, input.URI)
	if err != nil || record == nil {
		return httpx.Errorf(http.StatusBadRequest, "Clip Record Unavailable")
	}
	if !matchesClipRecord(record, clip) {
		return httpx.Errorf(http.StatusBadRequest, "Clip Record Does Not Match Rendered Clip")
	}
	err = s.Database.Write(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE podcast_clips SET published_uri=?,clip_json=json_set(clip_json,'$.publishedUri',?),updated_at=?
			WHERE id=? AND viewer_did=? AND json_extract(clip_json,'$.status')='complete'`, input.URI, input.URI, podcastNow(), clip.ID, viewer)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err == nil && count == 0 {
			return httpx.ErrNotFound
		}
		return err
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]string{"clipId": clip.ID, "uri": input.URI})
}

func podcastNow() float64 { return float64(time.Now().UnixMicro()) / 1e6 }

func (s *Service) deleteClip(w http.ResponseWriter, r *http.Request) error {
	viewer := reqctx.ViewerDID(r.Context())
	if viewer == "" {
		return httpx.ErrUnauthorized
	}
	var input struct {
		ID string `json:"clipId"`
	}
	if err := httpx.DecodeJSON(r, &input, 0); err != nil {
		return err
	}
	ctx := r.Context()
	err := s.Database.Write(ctx, func(tx *sql.Tx) error {
		var clip clipMetadata
		var raw, episode string
		err := tx.QueryRowContext(ctx, `DELETE FROM podcast_clips WHERE (id=? OR published_uri=?) AND viewer_did=? RETURNING clip_json,episode_id`,
			input.ID, input.ID, viewer).Scan(&raw, &episode)
		if err != nil {
			return podcastMissing(err)
		}
		var keys struct {
			Audio string `json:"audioKey"`
			Video string `json:"videoKey"`
		}
		if err := json.Unmarshal([]byte(raw), &clip); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(raw), &keys); err != nil {
			return err
		}
		if keys.Audio == "" {
			keys.Audio = "clips/" + clip.ID + "/audio.m4a"
		}
		if keys.Video == "" {
			keys.Video = "clips/" + clip.ID + "/audiogram.mp4"
		}
		payload, err := json.Marshal(map[string]string{"clipId": clip.ID, "audioKey": keys.Audio, "videoKey": keys.Video})
		if err != nil {
			return err
		}
		now := podcastNow()
		_, err = tx.ExecContext(ctx, `INSERT INTO podcast_jobs(id,viewer_did,episode_id,kind,dedupe_key,payload_json,available_at,created_at,updated_at)
			VALUES(?,?,?,'cleanup',?,?,?,?,?) ON CONFLICT(dedupe_key) DO NOTHING`, uuid.NewString(), viewer, episode, "cleanup:"+clip.ID, string(payload), now, now, now)
		return err
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{})
}
