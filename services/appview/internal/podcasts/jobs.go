package podcasts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
)

const viewerJobAccess = `(viewer_did=? OR (viewer_did IS NULL AND kind='silence') OR
	(viewer_did IS NULL AND kind='bridge' AND EXISTS(
		SELECT 1 FROM podcast_subscriptions s WHERE s.viewer_did=?
		AND s.show_id=json_extract(podcast_jobs.payload_json,'$.showId')
	)))`

const selectJob = `SELECT json_object('id',id,'kind',kind,'status',status,'result',json(result_json),'error',error)
	FROM podcast_jobs WHERE ` + viewerJobAccess

func (s *Service) getJob(w http.ResponseWriter, r *http.Request) error {
	viewer := reqctx.ViewerDID(r.Context())
	if viewer == "" {
		return httpx.ErrUnauthorized
	}
	query := r.URL.Query()
	id, showID, clipID := query.Get("jobId"), query.Get("showId"), query.Get("clipId")
	if id == "" && showID == "" && clipID == "" {
		return httpx.ErrNotFound
	}
	kind := ""
	if showID != "" {
		kind = "bridge"
	} else if clipID != "" {
		kind = "clip"
	}
	var raw string
	err := s.Database.Reader().QueryRowContext(r.Context(), selectJob+`
		AND (?='' OR id=?) AND (?='' OR kind=?) AND (?='' OR json_extract(payload_json,'$.showId')=?)
		AND (?='' OR json_extract(payload_json,'$.clipId')=?) ORDER BY created_at DESC LIMIT 1`,
		viewer, viewer, id, id, kind, kind, showID, showID, clipID, clipID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	return httpx.OK(w, json.RawMessage(raw))
}

func (s *Service) retryJob(w http.ResponseWriter, r *http.Request) error {
	viewer := reqctx.ViewerDID(r.Context())
	if viewer == "" {
		return httpx.ErrUnauthorized
	}
	var input struct {
		ID string `json:"jobId"`
	}
	if err := httpx.DecodeJSON(r, &input, 0); err != nil {
		return err
	}
	if input.ID == "" {
		return httpx.ErrBadRequest
	}
	raw, err := s.retry(r.Context(), viewer, input.ID)
	if err != nil {
		return err
	}
	return httpx.OK(w, raw)
}

func (s *Service) retry(ctx context.Context, viewer, id string) (json.RawMessage, error) {
	var raw string
	err := s.Database.Write(ctx, func(tx *sql.Tx) error {
		now := float64(time.Now().UnixMicro()) / 1e6
		var kind, payload string
		err := tx.QueryRowContext(ctx, `UPDATE podcast_jobs SET status='queued',available_at=?,error=NULL,updated_at=?
			WHERE id=? AND status='failed' AND attempts<5 AND kind IN ('bridge','silence','clip') AND `+
			viewerJobAccess+` RETURNING kind,payload_json`, now, now, id, viewer, viewer).Scan(&kind, &payload)
		if errors.Is(err, sql.ErrNoRows) {
			return httpx.Errorf(http.StatusConflict, "Job Cannot Be Retried")
		}
		if err != nil {
			return err
		}
		if kind == "clip" {
			if _, err := tx.ExecContext(ctx, `UPDATE podcast_clips SET clip_json=json_set(json_remove(clip_json,'$.error'),'$.status','queued'),updated_at=?
				WHERE id=json_extract(?,'$.clipId') AND viewer_did=?`, now, payload, viewer); err != nil {
				return err
			}
		}
		return tx.QueryRowContext(ctx, selectJob+` AND id=?`, viewer, viewer, id).Scan(&raw)
	})
	return json.RawMessage(raw), err
}
