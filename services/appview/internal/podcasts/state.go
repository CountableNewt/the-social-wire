package podcasts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
)

type listenerProgress struct {
	Position  float64 `json:"positionSeconds"`
	Updated   string  `json:"updatedAt"`
	Completed bool    `json:"completed"`
}
type showLink struct {
	RSSShowID      string `json:"rssShowId"`
	ProtocolShowID string `json:"protocolShowId"`
}
type listenerState struct {
	Subscriptions  []string                    `json:"subscriptions"`
	Queue          []string                    `json:"queue"`
	Progress       map[string]listenerProgress `json:"progress"`
	PlaybackSpeed  float64                     `json:"playbackSpeed"`
	RemoveSilences bool                        `json:"removeSilences"`
	ManualLinks    []showLink                  `json:"manualLinks"`
}
type stateSnapshot struct {
	State    listenerState `json:"state"`
	Revision int64         `json:"revision"`
}

func readState(ctx context.Context, tx *sql.Tx, viewer string) (stateSnapshot, error) {
	result := stateSnapshot{State: listenerState{
		Subscriptions: []string{}, Queue: []string{}, Progress: map[string]listenerProgress{}, PlaybackSpeed: 1, ManualLinks: []showLink{},
	}}
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT revision,state_json FROM podcast_viewer_state WHERE viewer_did=?`, viewer).Scan(&result.Revision, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal([]byte(raw), &result.State); err != nil {
		return result, err
	}
	result.State.PlaybackSpeed = math.Round(min(2, max(0.75, result.State.PlaybackSpeed))*4) / 4
	return result, nil
}

func (s *Service) state(w http.ResponseWriter, r *http.Request) error {
	viewer := reqctx.ViewerDID(r.Context())
	if viewer == "" {
		return httpx.ErrUnauthorized
	}
	setPodcastPrivateCache(w)
	if err := s.hydrateSubscriptions(r.Context(), viewer); err != nil {
		return err
	}

	var snapshot stateSnapshot
	ctx := r.Context()
	err := s.Database.Read(ctx, func(tx *sql.Tx) error {
		var err error
		snapshot, err = readState(ctx, tx, viewer)
		if err != nil {
			return err
		}
		if err = s.setSubscriptions(ctx, tx, viewer, &snapshot.State); err != nil {
			return err
		}
		return canonicalState(ctx, tx, &snapshot.State)
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, snapshot)
}

func (s *Service) setSubscriptions(ctx context.Context, tx *sql.Tx, viewer string, state *listenerState) error {
	shows, err := s.loadShows(ctx, tx, viewer)
	if err != nil {
		return err
	}
	state.Subscriptions = make([]string, 0, len(shows))
	for _, show := range shows {
		if id, ok := show["id"].(string); ok {
			state.Subscriptions = append(state.Subscriptions, id)
		}
	}
	return nil
}

func canonicalState(ctx context.Context, tx *sql.Tx, state *listenerState) error {
	wanted := make([]string, 0, len(state.Queue)+len(state.Progress))
	wanted = append(wanted, state.Queue...)
	for id := range state.Progress {
		wanted = append(wanted, id)
	}
	encoded, err := json.Marshal(wanted)
	if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT alias,canonical_id FROM podcast_aliases
		WHERE entity_kind='episode' AND alias IN(SELECT value FROM json_each(?))`, string(encoded))
	if err != nil {
		return err
	}
	aliases, err := readAliases(rows)
	if err != nil {
		return err
	}
	links, err := json.Marshal(state.ManualLinks)
	if err != nil {
		return err
	}
	rows, err = tx.QueryContext(ctx, `SELECT native.id,rss.id FROM json_each(?) link
		JOIN podcast_episodes native ON native.show_id=json_extract(link.value,'$.protocolShowId')
		JOIN podcast_episodes rss ON rss.show_id=json_extract(link.value,'$.rssShowId') AND rss.guid=native.guid
		WHERE native.id IN(SELECT value FROM json_each(?))`, string(links), string(encoded))
	if err != nil {
		return err
	}
	manual, err := readAliases(rows)
	if err != nil {
		return err
	}
	for source, target := range manual {
		aliases[source] = target
	}
	seen := map[string]bool{}
	queue := make([]string, 0, len(state.Queue))
	for _, id := range state.Queue {
		if canonical := aliases[id]; canonical != "" {
			id = canonical
		}
		if !seen[id] {
			queue = append(queue, id)
			seen[id] = true
		}
	}
	state.Queue = queue
	for source, target := range aliases {
		if source == target {
			continue
		}
		if progress, ok := state.Progress[source]; ok {
			delete(state.Progress, source)
			updated, _ := time.Parse(time.RFC3339Nano, progress.Updated)
			current, _ := time.Parse(time.RFC3339Nano, state.Progress[target].Updated)
			if !current.After(updated) {
				state.Progress[target] = progress
			}
		}
	}
	return nil
}

func readAliases(rows *sql.Rows) (map[string]string, error) {
	defer rows.Close()
	aliases := map[string]string{}
	for rows.Next() {
		var source, target string
		if err := rows.Scan(&source, &target); err != nil {
			return nil, err
		}
		aliases[source] = target
	}
	return aliases, rows.Err()
}

func (s *Service) putState(w http.ResponseWriter, r *http.Request) error {
	viewer := reqctx.ViewerDID(r.Context())
	if viewer == "" {
		return httpx.ErrUnauthorized
	}
	var input struct {
		Expected *int64        `json:"expectedRevision"`
		State    listenerState `json:"state"`
	}
	if err := httpx.DecodeJSON(r, &input, 0); err != nil {
		return err
	}
	invalid := httpx.Errorf(http.StatusBadRequest, "Invalid Podcast State")
	state := input.State
	if input.Expected == nil || *input.Expected < 0 || *input.Expected == math.MaxInt64 || !state.valid() {
		return invalid
	}
	ctx := r.Context()
	err := s.Database.Write(ctx, func(tx *sql.Tx) error {
		prior, err := readState(ctx, tx, viewer)
		if err != nil {
			return err
		}
		if prior.Revision != *input.Expected {
			return httpx.Errorf(http.StatusConflict, "Podcast State Revision Conflict")
		}
		if err = s.setSubscriptions(ctx, tx, viewer, &state); err != nil {
			return err
		}
		for _, link := range state.ManualLinks {
			var ok bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM podcast_shows WHERE id=? AND source_kind='rss')
				AND EXISTS(SELECT 1 FROM podcast_shows WHERE id=? AND source_kind='atproto')`, link.RSSShowID, link.ProtocolShowID).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return httpx.Errorf(http.StatusBadRequest, "Invalid Podcast Link")
			}
		}
		if err := canonicalState(ctx, tx, &state); err != nil {
			return err
		}
		private := map[string]bool{}
		for _, id := range state.Queue {
			if privateID(id) {
				private[id] = true
			}
		}
		for id := range state.Progress {
			if privateID(id) {
				private[id] = true
			}
		}
		for id := range private {
			var raw string
			if err := tx.QueryRowContext(ctx, `SELECT episode_data FROM podcast_private_episodes WHERE viewer_did=? AND id=?`, viewer, id).Scan(&raw); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return invalid
				}
				return err
			}
			if _, err := s.decodePrivate(raw, viewer, "episode", id); err != nil {
				return err
			}
		}
		payload, err := json.Marshal(state)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO podcast_viewer_state(viewer_did,revision,state_json,updated_at) VALUES(?,?,?,?)
			ON CONFLICT(viewer_did) DO UPDATE SET revision=excluded.revision,state_json=excluded.state_json,updated_at=excluded.updated_at`,
			viewer, *input.Expected+1, string(payload), float64(time.Now().UnixMicro())/1e6)
		return err
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, stateSnapshot{State: state, Revision: *input.Expected + 1})
}

func (s listenerState) valid() bool {
	if s.PlaybackSpeed < 0.75 || s.PlaybackSpeed > 2 || math.Round(s.PlaybackSpeed*4) != s.PlaybackSpeed*4 ||
		s.Queue == nil || len(s.Queue) > 1000 || s.Progress == nil || len(s.Progress) > 10000 || s.ManualLinks == nil ||
		len(s.ManualLinks) > 1000 || s.Subscriptions == nil || len(s.Subscriptions) > 10000 {
		return false
	}
	for _, progress := range s.Progress {
		if progress.Position < 0 {
			return false
		}
		if _, err := time.Parse(time.RFC3339Nano, progress.Updated); err != nil {
			return false
		}
	}
	return true
}
