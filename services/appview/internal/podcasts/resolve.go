package podcasts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

type repoCatalog interface {
	PublicRecords
	ListRecords(context.Context, *reqctx.Auth, atproto.ListRecordsParams) ([]atproto.RepoRecord, string, error)
	Resolver() *atproto.Resolver
}

type resolveResult struct {
	Show     Show      `json:"show"`
	Episodes []Episode `json:"episodes"`
	Choices  []Show    `json:"shows,omitempty"`
}

func publicRSSAllowed(raw string) bool {
	raw = strings.TrimSpace(raw)
	if !PrivateURLAllowed(raw) {
		return false
	}
	parsed, _ := url.Parse(raw)
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return false
	}
	for key, values := range query {
		switch strings.ToLower(key) {
		case "feed", "format":
		default:
			return false
		}
		for _, value := range values {
			switch strings.ToLower(value) {
			case "rss", "rss2", "atom", "rdf", "xml":
			default:
				return false
			}
		}
	}
	return true
}

func (s *Service) resolveRoute(w http.ResponseWriter, r *http.Request) error {
	if reqctx.ViewerDID(r.Context()) == "" {
		return httpx.ErrUnauthorized
	}
	var input struct {
		URL string `json:"url"`
	}
	if err := httpx.DecodeJSON(r, &input, 0); err != nil {
		return err
	}
	resolved, err := s.resolve(r.Context(), strings.TrimSpace(input.URL))
	if err != nil {
		return err
	}
	return httpx.OK(w, resolved)
}

func (s *Service) resolve(ctx context.Context, raw string) (resolveResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var result resolveResult
	var err error
	switch {
	case strings.HasPrefix(raw, "at://"):
		result, err = s.resolveProtocol(ctx, raw)
	case strings.HasPrefix(raw, "did:") || (!strings.Contains(raw, "/") && !strings.HasPrefix(raw, "https:") && !strings.HasPrefix(raw, "http:")):
		result, err = s.resolveProtocolOwner(ctx, raw)
	default:
		if !publicRSSAllowed(raw) {
			return result, httpx.Errorf(http.StatusBadRequest, "Only Public HTTPS Podcast Feeds Are Supported")
		}
		target, ok := thinstore.NormalizeFeedURL(raw)
		if !ok {
			return result, httpx.Errorf(http.StatusBadRequest, "Only Public HTTPS Podcast Feeds Are Supported")
		}
		result, err = fetchPodcastRSS(ctx, target, publicRSSAllowed)
	}
	if err != nil {
		return result, err
	}
	if err := s.savePublicCatalog(ctx, &result); err != nil {
		return result, err
	}
	for index := range result.Episodes {
		result.Episodes[index].ChapterSourceURL = nil
	}
	return result, nil
}

func fetchPodcastRSS(ctx context.Context, raw string, allowed func(string) bool) (resolveResult, error) {
	var result resolveResult
	body, err := atproto.PublicMedia.Fetch(ctx, raw, atproto.FetchOptions{MaximumBytes: 32 << 20, ValidateURL: allowed})
	if err != nil {
		return result, httpx.Errorf(http.StatusBadGateway, "Podcast Feed Could Not Be Loaded")
	}
	result.Show, result.Episodes, err = ParseRSS(body, raw)
	if err != nil || len(result.Episodes) == 0 {
		return result, httpx.Errorf(http.StatusBadRequest, "No Audio Episodes Found")
	}
	return result, nil
}

func (s *Service) resolveProtocol(ctx context.Context, raw string) (resolveResult, error) {
	var result resolveResult
	client, ok := s.Repo.(repoCatalog)
	uri, valid := atproto.ParseRecordURI(raw)
	collection := ProtocolCollections[uri.Collection]
	if !valid || collection == "" || !ok {
		return result, httpx.Errorf(http.StatusBadRequest, "Unsupported Podcast Source")
	}
	record, err := client.GetRecordByATURI(ctx, nil, raw)
	if err != nil || record == nil {
		return result, httpx.Errorf(http.StatusBadRequest, "Unsupported Podcast Source")
	}
	pds, err := client.Resolver().ResolvePDSBaseStrict(ctx, uri.Repo, atproto.EndpointPublicHTTPS)
	if err != nil {
		return result, httpx.Errorf(http.StatusBadGateway, "Podcast Repository Could Not Be Loaded")
	}
	show, err := ProtocolShow(raw, record, pds)
	if err != nil {
		return result, httpx.Errorf(http.StatusBadRequest, "Unsupported Podcast Source")
	}
	episodes := []Episode{}
	err = walkPodcastRecords(ctx, client, uri.Repo, collection, func(row atproto.RepoRecord) {
		provenance, ok := atproto.ParseRecordURI(row.URI)
		if !ok || provenance.Repo != uri.Repo || provenance.Collection != collection {
			return
		}
		if item := ProtocolEpisode(row.URI, row.Value, show, pds); item != nil {
			episodes = append(episodes, *item)
		}
	})
	if err != nil {
		return result, httpx.Errorf(http.StatusBadGateway, "Podcast Repository Could Not Be Loaded")
	}
	return resolveResult{Show: show, Episodes: episodes}, nil
}

func (s *Service) resolveProtocolOwner(ctx context.Context, owner string) (resolveResult, error) {
	var result resolveResult
	client, ok := s.Repo.(repoCatalog)
	if !ok || owner == "" {
		return result, httpx.ErrNotFound
	}
	keys := make([]string, 0, len(ProtocolCollections))
	for collection := range ProtocolCollections {
		keys = append(keys, collection)
	}
	sort.Strings(keys)
	choices := []Show{}
	for _, collection := range keys {
		err := walkPodcastRecords(ctx, client, owner, collection, func(row atproto.RepoRecord) {
			if show, err := ProtocolShow(row.URI, row.Value, ""); err == nil {
				choices = append(choices, show)
			}
		})
		if err != nil && ctx.Err() != nil {
			return result, ctx.Err()
		}
	}
	if len(choices) == 0 {
		return result, httpx.Errorf(http.StatusNotFound, "No Podcast Shows Found")
	}
	result, err := s.resolveProtocol(ctx, choices[0].ID)
	result.Choices = choices
	return result, err
}

func walkPodcastRecords(ctx context.Context, client repoCatalog, owner, collection string, visit func(atproto.RepoRecord)) error {
	cursor := ""
	seen := map[string]bool{}
	for range 20 {
		rows, next, err := client.ListRecords(ctx, nil, atproto.ListRecordsParams{Repo: owner, Collection: collection, Limit: 100, Cursor: cursor, RequireResolvedRepository: true})
		if err != nil {
			return err
		}
		for _, row := range rows {
			visit(row)
		}
		if next == "" {
			return nil
		}
		if seen[next] {
			return publicCatalogUnavailable()
		}
		seen[next] = true
		cursor = next
	}
	return publicCatalogUnavailable()
}

func (s *Service) savePublicCatalog(ctx context.Context, result *resolveResult) error {
	show := &result.Show
	return s.Database.Write(ctx, func(tx *sql.Tx) error {
		now := podcastNow()
		var raw string
		feed, source, guid := stringPtr(show.FeedURL), stringPtr(show.SourceURI), stringPtr(show.Guid)
		err := tx.QueryRowContext(ctx, `SELECT show_json FROM podcast_shows WHERE id=?
			OR (?<>'' AND feed_url=?) OR (?<>'' AND source_uri=?)
			OR id=(SELECT canonical_id FROM podcast_aliases WHERE alias=? AND entity_kind='show')
			ORDER BY CASE WHEN id=? THEN 0 ELSE 1 END,id LIMIT 1`,
			show.ID, feed, feed, source, source, "guid:"+guid, show.ID).Scan(&raw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			var prior Show
			if err = json.Unmarshal([]byte(raw), &prior); err != nil {
				return err
			}
			if show.SourceKind == "atproto" && prior.ID != show.ID {
				if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO podcast_aliases VALUES(?,?,'show')`, show.ID, prior.ID); err != nil {
					return err
				}
				prior.SourceURI, prior.EpisodeCollection = show.SourceURI, show.EpisodeCollection
				*show = prior
			} else if show.SourceKind == "rss" {
				show.ID, show.SourceURI, show.SourceKind, show.EpisodeCollection = prior.ID, prior.SourceURI, prior.SourceKind, prior.EpisodeCollection
				if show.Guid == nil {
					show.Guid = prior.Guid
				}
				if len(show.Hosts) == 0 {
					show.Hosts = prior.Hosts
				}
			}
		}
		body, err := json.Marshal(show)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO podcast_shows(id,feed_url,source_kind,source_uri,show_json,updated_at) VALUES(?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET show_json=excluded.show_json,source_uri=coalesce(excluded.source_uri,podcast_shows.source_uri),
			updated_at=excluded.updated_at`, show.ID, show.FeedURL, show.SourceKind, show.SourceURI, string(body), now); err != nil {
			return err
		}
		for index := range result.Episodes {
			item := &result.Episodes[index]
			item.ShowID = show.ID
			if err := savePublicEpisode(ctx, tx, item, now); err != nil {
				return err
			}
		}
		aliases := []string{stringPtr(show.FeedURL), stringPtr(show.SourceURI)}
		if show.Guid != nil {
			aliases = append(aliases, "guid:"+*show.Guid)
		}
		for _, alias := range aliases {
			if alias != "" {
				if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO podcast_aliases VALUES(?,?,'show')`, alias, show.ID); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func savePublicEpisode(ctx context.Context, tx *sql.Tx, item *Episode, now float64) error {
	if item.Guid != nil {
		var priorID, raw string
		err := tx.QueryRowContext(ctx, `SELECT id,episode_json FROM podcast_episodes WHERE show_id=? AND guid=?`, item.ShowID, item.Guid).Scan(&priorID, &raw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			var prior Episode
			if err := json.Unmarshal([]byte(raw), &prior); err != nil {
				return err
			}
			if item.SourceURI == nil {
				item.SourceURI = prior.SourceURI
			}
			if len(item.Chapters) == 0 && stringPtr(item.ChapterSourceURL) == stringPtr(prior.ChapterSourceURL) {
				item.Chapters = prior.Chapters
			}
			if priorID != item.ID {
				if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO podcast_aliases VALUES(?,?,'episode')`, item.ID, priorID); err != nil {
					return err
				}
				item.ID = priorID
			}
		}
	}
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	published, _ := time.Parse(time.RFC3339Nano, item.PublishedAt)
	seconds := float64(0)
	if !published.IsZero() {
		seconds = float64(published.UnixMicro()) / 1e6
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO podcast_episodes(id,show_id,guid,episode_json,published_at,updated_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET episode_json=excluded.episode_json,updated_at=excluded.updated_at`,
		item.ID, item.ShowID, item.Guid, string(data), seconds, now)
	return err
}

func stringPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
