package podcasts

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
	"github.com/google/uuid"
)

func sourceFingerprint(ctx context.Context, episode map[string]any) string {
	target, _ := episode["audioUrl"].(string)
	value, err := atproto.PublicMedia.Fingerprint(ctx, target)
	if err != nil || value == "" {
		metadata, _ := json.Marshal(episode)
		value = string(metadata) + "|" + strconv.FormatInt(time.Now().Unix()/900, 10)
	}
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func (s *Service) requestAnalysis(w http.ResponseWriter, r *http.Request) error {
	viewer := reqctx.ViewerDID(r.Context())
	if viewer == "" {
		return httpx.ErrUnauthorized
	}
	var input struct {
		EpisodeID string `json:"episodeId"`
	}
	if err := httpx.DecodeJSON(r, &input, 0); err != nil {
		return err
	}
	ctx := r.Context()
	episode, err := s.loadEpisode(ctx, viewer, input.EpisodeID)
	if err != nil {
		return err
	}
	if episode["visibility"] == "private" {
		return httpx.Errorf(http.StatusForbidden, "Silence Analysis Is Unavailable For Private Podcasts")
	}
	fingerprint := sourceFingerprint(ctx, episode)
	payload, err := json.Marshal(map[string]any{"episode": episode, "sourceFingerprint": fingerprint})
	if err != nil {
		return err
	}
	episodeID, _ := episode["id"].(string)
	var job string
	err = s.Database.Write(ctx, func(tx *sql.Tx) error {
		now := podcastNow()
		var id string
		err := tx.QueryRowContext(ctx, `INSERT INTO podcast_jobs(id,episode_id,kind,dedupe_key,payload_json,available_at,created_at,updated_at)
			VALUES(?,?,'silence',?,?,?,?,?) ON CONFLICT(dedupe_key) DO UPDATE SET dedupe_key=excluded.dedupe_key RETURNING id`,
			uuid.NewString(), episodeID, "silence:v2:"+episodeID+":"+fingerprint, string(payload), now, now, now).Scan(&id)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE podcast_jobs SET status='queued',available_at=?,error=NULL,updated_at=?
			WHERE id=? AND status='failed' AND attempts<5`, now, now, id); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, selectJob+` AND id=?`, viewer, viewer, id).Scan(&job)
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, json.RawMessage(job))
}

func (s *Service) getAnalysis(w http.ResponseWriter, r *http.Request) error {
	viewer := reqctx.ViewerDID(r.Context())
	if viewer == "" {
		return httpx.ErrUnauthorized
	}
	ctx := r.Context()
	episode, err := s.loadEpisode(ctx, viewer, r.URL.Query().Get("episodeId"))
	if errors.Is(err, httpx.ErrNotFound) {
		return httpx.ErrBadRequest
	}
	if err != nil {
		return err
	}
	result := map[string]any{"analysisVersion": "v2", "status": "unavailable", "intervals": []any{}}
	if episode["visibility"] == "private" {
		result["reason"] = "private-feed"
		return httpx.OK(w, result)
	}
	expected := sourceFingerprint(ctx, episode)
	episodeID, _ := episode["id"].(string)
	var id, status string
	var stored sql.NullString
	err = s.Database.Reader().QueryRowContext(ctx, `SELECT id,status,result_json FROM podcast_jobs WHERE kind='silence' AND dedupe_key=?
		AND (viewer_did IS NULL OR viewer_did=?)`, "silence:v2:"+episodeID+":"+expected, viewer).Scan(&id, &status, &stored)
	if errors.Is(err, sql.ErrNoRows) {
		return httpx.OK(w, result)
	}
	if err != nil {
		return err
	}
	var analysis struct {
		SourceFingerprint string `json:"sourceFingerprint"`
		Intervals         []any  `json:"intervals"`
	}
	if stored.Valid {
		if err := json.Unmarshal([]byte(stored.String), &analysis); err != nil {
			return err
		}
	}
	if status == "complete" && analysis.SourceFingerprint != expected {
		err = s.Database.Write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `UPDATE podcast_jobs SET status='failed',result_json=NULL,error='Media Source Changed During Analysis',
				updated_at=? WHERE id=? AND kind='silence' AND status='complete' AND json_extract(result_json,'$.sourceFingerprint') IS NOT ?`,
				podcastNow(), id, expected)
			return err
		})
		if err != nil {
			return err
		}
		return httpx.OK(w, result)
	}
	result["status"] = status
	if analysis.Intervals != nil {
		result["intervals"] = analysis.Intervals
	}
	return httpx.OK(w, result)
}
