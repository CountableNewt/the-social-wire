package podcasts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

const rpcPrefix = "/xrpc/app.thesocialwire.podcast."

type PublicRecords interface {
	GetRecordByATURI(context.Context, *reqctx.Auth, string) (map[string]any, error)
}

type Service struct {
	Database              *sqlitedb.DB
	Repo                  PublicRecords
	WorkerURL             string
	WorkerSecret          string
	PrivateStorageKey     string
	BridgeEnabled         bool
	Background            context.Context
	catalogQueue          catalogQueue
	subscriptionRefreshes sync.Map
}

type clipMetadata struct {
	ID           string  `json:"id"`
	EpisodeID    string  `json:"episodeId"`
	Status       string  `json:"status"`
	PublishedURI string  `json:"publishedUri"`
	AudioURL     *string `json:"publicAudioUrl"`
	VideoURL     *string `json:"publicVideoUrl"`
	StartSeconds float64 `json:"startSeconds"`
	EndSeconds   float64 `json:"endSeconds"`
}

var clipIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Register mounts clip browsing and verified clip asset delivery.
func (s *Service) Register(protected *httpx.Group, router *httpx.Router) {
	protected.Post(rpcPrefix+"resolveShow", s.resolveRoute)
	protected.Post(rpcPrefix+"findPodcasts", s.findPodcasts)
	protected.Post(rpcPrefix+"resolvePrivateShow", s.privateSubscribe)
	protected.Post(rpcPrefix+"refreshPrivateShow", s.privateRefresh)
	protected.Post(rpcPrefix+"deletePrivateSubscription", s.privateDelete)

	protected.Get(rpcPrefix+"getShows", s.shows)
	protected.Get(rpcPrefix+"getState", s.state)
	protected.Post(rpcPrefix+"putState", s.putState)
	protected.Get(rpcPrefix+"getEpisodes", s.episodes)
	protected.Get(rpcPrefix+"getMedia", s.media)
	protected.Get(rpcPrefix+"getImage", s.image)
	protected.Get(rpcPrefix+"getAnalysis", s.getAnalysis)
	protected.Post(rpcPrefix+"requestAnalysis", s.requestAnalysis)
	protected.Get(rpcPrefix+"listClips", s.listClips)
	protected.Get(rpcPrefix+"getTranscript", s.transcript)
	protected.Post(rpcPrefix+"createClip", s.createClip)
	protected.Post(rpcPrefix+"publishClip", s.publishClip)
	protected.Post(rpcPrefix+"deleteClip", s.deleteClip)
	protected.Get(rpcPrefix+"getJob", s.getJob)
	protected.Post(rpcPrefix+"retryJob", s.retryJob)
	protected.Get(rpcPrefix+"getClipAsset", s.clipAsset(false))
	router.Get(rpcPrefix+"getPublicClip", s.publicClip)
	router.Get(rpcPrefix+"getPublicClipAsset", s.clipAsset(true))
}

func (s *Service) listClips(w http.ResponseWriter, r *http.Request) error {
	viewer := reqctx.ViewerDID(r.Context())
	if viewer == "" {
		return httpx.ErrUnauthorized
	}
	rows, err := s.Database.Reader().QueryContext(r.Context(), `SELECT json_set(clip_json,'$.jobId',(
		SELECT id FROM podcast_jobs WHERE json_extract(payload_json,'$.clipId')=podcast_clips.id
		AND kind='clip' ORDER BY created_at DESC LIMIT 1
	)) FROM podcast_clips WHERE viewer_did=? ORDER BY updated_at DESC LIMIT 100`, viewer)
	if err != nil {
		return err
	}
	defer rows.Close()
	clips := []json.RawMessage{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		clips = append(clips, json.RawMessage(raw))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"clips": clips})
}

func (s *Service) loadClip(ctx context.Context, id, viewer string, public bool) (clipMetadata, json.RawMessage, error) {
	var clip clipMetadata
	var stored string
	var err error
	if public {
		err = s.Database.Reader().QueryRowContext(ctx, `SELECT clip_json FROM podcast_clips
			WHERE (id=? OR published_uri=?) AND published_uri IS NOT NULL LIMIT 1`, id, id).Scan(&stored)
	} else {
		err = s.Database.Reader().QueryRowContext(ctx, `SELECT clip_json FROM podcast_clips
			WHERE (id=? OR published_uri=?) AND viewer_did=? LIMIT 1`, id, id, viewer).Scan(&stored)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return clip, nil, httpx.ErrNotFound
	}
	if err != nil {
		return clip, nil, err
	}
	if err := json.Unmarshal([]byte(stored), &clip); err != nil {
		return clip, nil, err
	}
	if public {
		if clip.PublishedURI == "" || s.Repo == nil {
			return clip, nil, httpx.ErrNotFound
		}
		record, err := s.Repo.GetRecordByATURI(ctx, nil, clip.PublishedURI)
		if err != nil || !matchesClipRecord(record, clip) {
			return clip, nil, httpx.ErrNotFound
		}
	}
	return clip, json.RawMessage(stored), nil
}

func matchesClipRecord(record map[string]any, c clipMetadata) bool {
	mediaEqual := func(key string, wanted *string) bool {
		got, ok := record[key].(string)
		if wanted == nil {
			return !ok
		}
		return ok && got == *wanted
	}
	return record["$type"] == "app.thesocialwire.podcast.clip" && record["episodeId"] == c.EpisodeID &&
		record["startMillis"] == math.Round(c.StartSeconds*1000) && record["endMillis"] == math.Round(c.EndSeconds*1000) &&
		mediaEqual("audioUrl", c.AudioURL) && mediaEqual("videoUrl", c.VideoURL)
}

func (s *Service) publicClip(w http.ResponseWriter, r *http.Request) error {
	id := r.URL.Query().Get("clipId")
	if id == "" {
		id = r.URL.Query().Get("uri")
	}
	if id == "" {
		return httpx.ErrNotFound
	}
	_, raw, err := s.loadClip(r.Context(), id, "", true)
	if err != nil {
		return err
	}
	return httpx.OK(w, raw)
}

func (s *Service) clipAsset(public bool) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		viewer := reqctx.ViewerDID(r.Context())
		if !public && viewer == "" {
			return httpx.ErrUnauthorized
		}
		id := r.URL.Query().Get("clipId")
		if id == "" {
			return httpx.ErrNotFound
		}
		clip, _, err := s.loadClip(r.Context(), id, viewer, public)
		if err != nil {
			return err
		}
		if !public && clip.Status != "complete" {
			return httpx.ErrNotFound
		}
		filename := ""
		switch r.URL.Query().Get("format") {
		case "audio":
			filename = "audio.m4a"
		case "video":
			filename = "audiogram.mp4"
		}
		if filename == "" || !clipIDPattern.MatchString(clip.ID) {
			return httpx.ErrBadRequest
		}
		origin, ok := loopbackOrigin(s.WorkerURL)
		if !ok {
			return httpx.Status(http.StatusServiceUnavailable)
		}
		path := "/assets/"
		if !public {
			path = "/internal/assets/"
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, r.Method, strings.TrimSuffix(origin, "/")+path+clip.ID+"/"+filename, nil)
		if err != nil {
			return err
		}
		req.Header.Set("X-Podcast-Media-Secret", s.WorkerSecret)
		req.Header.Set("Range", r.Header.Get("Range"))
		response, err := readinessClient.Do(req)
		if err != nil {
			return httpx.Status(http.StatusBadGateway)
		}
		defer response.Body.Close()
		w.Header().Set("Cache-Control", "private, no-store")
		for _, name := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges"} {
			if value := response.Header.Get(name); value != "" {
				w.Header().Set(name, value)
			}
		}
		w.WriteHeader(response.StatusCode)
		_, err = io.Copy(w, response.Body)
		return err
	}
}
