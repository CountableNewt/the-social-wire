package podcasts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
)

type SearchResponse struct {
	Shows          []map[string]any     `json:"shows"`
	Episodes       []map[string]any     `json:"episodes"`
	Cursor         *string              `json:"cursor,omitempty"`
	Candidates     []DirectoryCandidate `json:"candidates"`
	DirectoryLimit *int                 `json:"directoryLimit,omitempty"`
	HasMore        bool                 `json:"hasMore"`
}

var podcastDirectory = NewDirectory(atproto.PublicMedia)

func (s *Service) findPodcasts(w http.ResponseWriter, r *http.Request) error {
	viewer := reqctx.ViewerDID(r.Context())
	if viewer == "" {
		return httpx.ErrUnauthorized
	}
	var input SearchRequest
	if err := httpx.DecodeJSON(r, &input, 0); err != nil {
		return err
	}
	var result SearchResponse
	var err error
	if stringPtr(input.Scope) == "directory" {
		result, err = podcastDirectory.Search(r.Context(), input)
	} else {
		result, err = s.searchLibrary(r.Context(), viewer, input)
	}
	switch {
	case errors.Is(err, ErrDirectoryInvalidQuery):
		return httpx.Errorf(http.StatusBadRequest, "Discover Accepts Podcast Names Or Topics. Add Feed URLs Using Add Podcast.")
	case errors.Is(err, ErrDirectoryInvalidRequest):
		return httpx.Errorf(http.StatusBadRequest, "Invalid Podcast Directory Search")
	case errors.Is(err, ErrDirectoryBusy):
		return httpx.Errorf(http.StatusServiceUnavailable, "Podcast Directory Is Busy. Try Again Shortly.")
	case errors.Is(err, ErrDirectoryUnavailable):
		return httpx.Errorf(http.StatusBadGateway, "Podcast Index Is Unavailable. Try Again Shortly.")
	case errors.Is(err, ErrInvalidRequest):
		return httpx.Errorf(http.StatusBadRequest, "Invalid Podcast Search")
	case err != nil:
		return err
	}
	w.Header().Set("Cache-Control", "private, no-store")
	return httpx.OK(w, result)
}

func (s *Service) searchLibrary(ctx context.Context, viewer string, input SearchRequest) (SearchResponse, error) {
	result := SearchResponse{Shows: []map[string]any{}, Episodes: []map[string]any{}, Candidates: []DirectoryCandidate{}}
	if err := input.Validate(); err != nil {
		return result, err
	}
	binding := SearchBinding(viewer, input)
	cursor, err := DecodeSearchCursor(input.Cursor, binding)
	if err != nil {
		return result, err
	}
	if cursor == nil {
		cursor = &SearchCursor{Binding: binding, Entity: -1}
	}
	limit, kind := 20, "all"
	if input.Limit != nil {
		limit = *input.Limit
	}
	if input.Kind != nil {
		kind = *input.Kind
	}
	err = s.Database.Read(ctx, func(tx *sql.Tx) error {
		state, err := readState(ctx, tx, viewer)
		if err != nil {
			return err
		}
		links, err := json.Marshal(state.State.ManualLinks)
		if err != nil {
			return err
		}
		hidden := map[string]bool{}
		for _, link := range state.State.ManualLinks {
			hidden[link.ProtocolShowID] = true
		}
		hasKey := s.privateKey() != nil
		rows, err := tx.QueryContext(ctx, `WITH mapping AS (
			SELECT json_extract(value,'$.protocolShowId') AS native_show_id,json_extract(value,'$.rssShowId') AS rss_show_id,
			cast(key AS INTEGER) AS ordinal FROM json_each(?)
		), ranked_links AS (
			SELECT native_show_id,rss_show_id,row_number() OVER (PARTITION BY native_show_id ORDER BY ordinal) AS position
			FROM mapping WHERE EXISTS(SELECT 1 FROM podcast_subscriptions WHERE viewer_did=? AND show_id=mapping.rss_show_id)
		), links AS (
			SELECT native_show_id,rss_show_id FROM ranked_links WHERE position=1
		), catalog AS (
			SELECT 0 AS entity,s.id,s.id AS show_id,s.show_json AS payload,0 AS is_private
			FROM podcast_shows s JOIN podcast_subscriptions v ON v.show_id=s.id WHERE v.viewer_did=?
			UNION ALL SELECT 0,id,id,show_data,1 FROM podcast_private_shows WHERE viewer_did=? AND ?
			UNION ALL SELECT 1,e.id,coalesce(l.rss_show_id,e.show_id),e.episode_json,0
			FROM podcast_episodes e JOIN podcast_subscriptions v ON v.show_id=e.show_id
			LEFT JOIN links l ON l.native_show_id=e.show_id WHERE v.viewer_did=? AND NOT EXISTS(
				SELECT 1 FROM podcast_episodes rss WHERE rss.show_id=l.rss_show_id AND rss.guid=e.guid AND e.guid IS NOT NULL)
			UNION ALL SELECT 1,id,show_id,episode_data,1 FROM podcast_private_episodes WHERE viewer_did=? AND ?
		) SELECT entity,id,payload,is_private,show_id FROM catalog
		WHERE (?='all' OR (?='shows' AND entity=0) OR (?='episodes' AND entity=1))
		AND (? IS NULL OR show_id=?) AND (entity,id)>(?,?)
		ORDER BY entity,id LIMIT 501`, string(links), viewer, viewer, viewer, hasKey, viewer, viewer, hasKey,
			kind, kind, kind, input.ShowID, input.ShowID, cursor.Entity, cursor.ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		scanned := 0
		for rows.Next() {
			if scanned >= 500 || len(result.Shows)+len(result.Episodes) >= limit {
				encoded, err := cursor.Encode()
				if err != nil {
					return err
				}
				result.HasMore = true
				result.Cursor = &encoded
				break
			}
			var id, raw, showID string
			var entity int
			var private bool
			if err := rows.Scan(&entity, &id, &raw, &private, &showID); err != nil {
				return err
			}
			cursor.Entity, cursor.ID = entity, id
			scanned++
			var value map[string]any
			if private {
				kind := "show"
				if entity == 1 {
					kind = "episode"
				}
				value, err = s.decodePrivate(raw, viewer, kind, id)
			} else {
				err = json.Unmarshal([]byte(raw), &value)
			}
			if err != nil {
				return err
			}
			if value == nil {
				return httpx.Status(http.StatusInternalServerError)
			}
			if entity == 0 {
				if hidden[id] {
					continue
				}
				visibleShow(value)
				var hosts []Person
				if rawHosts, ok := value["hosts"].([]any); ok {
					for _, raw := range rawHosts {
						if row, ok := raw.(map[string]any); ok {
							if name, ok := row["name"].(string); ok {
								hosts = append(hosts, Person{Name: name})
							}
						}
					}
				}
				title, _ := value["title"].(string)
				if SearchMatches(input.Query, title, rowString(value, "description"), hosts) {
					result.Shows = append(result.Shows, value)
				}
			} else {
				visibleEpisode(value)
				value["showId"] = showID
				title, _ := value["title"].(string)
				if SearchMatches(input.Query, title, rowString(value, "description"), nil) {
					result.Episodes = append(result.Episodes, value)
				}
			}
		}
		return rows.Err()
	})
	return result, err
}
