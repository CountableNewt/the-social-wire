package reader

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/publications"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
)

const (
	listCollection     = "app.standard-reader.list"
	listSaveCollection = "app.standard-reader.listSave"
)

var listRKey = regexp.MustCompile(`^[A-Za-z0-9._~:-]{1,512}$`)

func parseListURI(input string, allowShareURL bool) (atproto.ATURI, bool) {
	input = strings.TrimSpace(input)
	if allowShareURL && !strings.HasPrefix(input, "at://") {
		link, err := url.Parse(input)
		if err != nil || link.Scheme != "https" || link.Host != "standard-reader.app" || link.User != nil {
			return atproto.ATURI{}, false
		}
		parts := strings.Split(link.Path, "/")
		if len(parts) != 4 || parts[1] != "l" {
			return atproto.ATURI{}, false
		}
		input = atproto.BuildRecordURI(parts[2], listCollection, parts[3])
	}
	id, ok := atproto.ParseRecordURI(input)
	if !ok || id.Collection != listCollection || !knownDID(id.Repo) ||
		!listRKey.MatchString(id.RKey) || id.RKey == "." || id.RKey == ".." {
		return atproto.ATURI{}, false
	}
	return id, true
}

func knownDID(id string) bool {
	return len(id) <= 2048 && atproto.IsValidDID(id) && !strings.ContainsFunc(id,
		func(r rune) bool {
			return r > unicode.MaxASCII || unicode.IsSpace(r) || r == '/' || r == '?' || r == '#'
		})
}

type listPublication struct {
	PublicationID string `json:"publicationId"`
	Title         string `json:"title"`
	AuthorDID     string `json:"authorDid"`
	AuthorHandle  string `json:"authorHandle,omitempty"`
	IconURL       string `json:"iconUrl,omitempty"`
	AvatarURL     string `json:"avatarUrl,omitempty"`
}

type standardList struct {
	URI                string            `json:"uri"`
	Name               string            `json:"name"`
	Description        *string           `json:"description,omitempty"`
	CreatorDID         string            `json:"creatorDid"`
	Publications       []string          `json:"publications"`
	Users              []string          `json:"users"`
	Owned              bool              `json:"owned"`
	Saved              bool              `json:"saved"`
	PublicationDetails []listPublication `json:"publicationDetails"`
}

type standardListsResponse struct {
	Lists       []standardList `json:"lists"`
	RefreshedAt string         `json:"refreshedAt"`
	Complete    bool           `json:"complete"`
	CreatorDID  string         `json:"creatorDid,omitempty"`
}

type listCacheEntry struct {
	response standardListsResponse
	expires  time.Time
}

type standardLists struct {
	repo         *atproto.RepoClient
	publications *publications.Service
	mu           sync.Mutex
	cache        map[string]listCacheEntry
	inflight     singleflight.Group
}

func newStandardLists(p *publications.Service) *standardLists {
	return &standardLists{repo: p.Repository(), publications: p, cache: make(map[string]listCacheEntry)}
}

func registerLists(group *httpx.Group, service *standardLists) {
	group.Get("/v1/lists", authenticated(service.listRoute(false)))
	group.Get("/v1/lists/search", authenticated(service.searchRoute))
	group.Post("/v1/lists/resolve", authenticated(service.resolveRoute))
	group.Post("/v1/lists/refresh", authenticated(service.listRoute(true)))
}

func (s *standardLists) listRoute(refresh bool) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		ctx := r.Context()
		response, err := s.list(ctx, reqctx.ViewerDID(ctx), refresh)
		if err != nil {
			return err
		}
		return httpx.OK(w, response)
	}
}

func (s *standardLists) searchRoute(w http.ResponseWriter, r *http.Request) error {
	creator := strings.TrimSpace(httpx.Query(r, "creator"))
	if creator == "" || len(creator) > 253 || strings.ContainsAny(creator, "/?") {
		return httpx.Errorf(http.StatusBadRequest, "Enter a creator handle or DID.")
	}
	ctx := r.Context()
	did, err := s.repo.Resolver().ResolveRepoDID(ctx, creator)
	if err != nil {
		return err
	}
	if did == "" {
		return httpx.Errorf(http.StatusNotFound, "Creator could not be resolved.")
	}
	viewer := reqctx.ViewerDID(ctx)
	found, complete, err := s.records(ctx, did, listCollection)
	if err != nil {
		return err
	}
	viewerLists, err := s.list(ctx, viewer, false)
	if err != nil {
		return err
	}
	saved := map[string]bool{}
	for _, list := range viewerLists.Lists {
		saved[list.URI] = list.Saved
	}
	summary := []standardList{}
	for _, record := range found {
		if id, ok := parseListURI(record.URI, false); ok && id.Repo == did {
			if row, valid := decodeList(id, record.Value, viewer, saved[record.URI]); valid {
				summary = append(summary, row)
			}
		}
	}
	s.enrich(ctx, viewer, summary)
	response := sortedLists(summary, complete)
	response.CreatorDID = did
	return httpx.OK(w, response)
}

func (s *standardLists) resolveRoute(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Input string `json:"input"`
	}
	if err := httpx.DecodeJSON(r, &body, 0); err != nil {
		return err
	}
	id, ok := parseListURI(body.Input, true)
	if !ok {
		return httpx.Errorf(http.StatusBadRequest, "Enter a Standard Reader list URL or an app.standard-reader.list AT URI.")
	}
	viewer := reqctx.ViewerDID(r.Context())
	row, err := s.resolve(r.Context(), viewer, id, false)
	if err != nil {
		return err
	}
	result := []standardList{row}
	s.enrich(r.Context(), viewer, result)
	return httpx.OK(w, map[string]any{"list": result[0]})
}

func (s *standardLists) resolve(ctx context.Context, viewer string, id atproto.ATURI, saved bool) (standardList, error) {
	value, err := s.repo.GetRecord(ctx, nil, id.Repo, id.Collection, id.RKey)
	if err != nil {
		return standardList{}, err
	}
	if list, valid := decodeList(id, value, viewer, saved); valid {
		return list, nil
	}
	return standardList{}, httpx.Errorf(http.StatusNotFound, "List was not found or its record is invalid.")
}

func (s *standardLists) records(ctx context.Context, did, collection string) ([]atproto.RepoRecord, bool, error) {
	var records []atproto.RepoRecord
	var cursor string
	for range 10 {
		page, next, err := s.repo.ListRecords(ctx, nil, atproto.ListRecordsParams{Repo: did, Collection: collection, Limit: 100, Cursor: cursor, RequireResolvedRepository: true})
		if err != nil {
			if ctx.Err() != nil {
				return nil, false, ctx.Err()
			}
			return records, false, nil
		}
		records = append(records, page...)
		if next == "" {
			return records, true, nil
		}
		if next == cursor {
			break
		}
		cursor = next
	}
	return records, false, nil
}

func (s *standardLists) list(ctx context.Context, viewer string, refresh bool) (standardListsResponse, error) {
	if !refresh {
		s.mu.Lock()
		cached, ok := s.cache[viewer]
		s.mu.Unlock()
		if ok && cached.expires.After(time.Now()) {
			return cached.response, nil
		}
	}
	result, err, _ := s.inflight.Do(viewer, func() (any, error) {
		response, err := s.load(ctx, viewer)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.cache) >= 200 {
			oldestKey, oldestAt := "", time.Now().Add(time.Hour)
			for key, entry := range s.cache {
				if entry.expires.Before(time.Now()) {
					delete(s.cache, key)
				} else if entry.expires.Before(oldestAt) {
					oldestKey, oldestAt = key, entry.expires
				}
			}
			if len(s.cache) >= 200 {
				delete(s.cache, oldestKey)
			}
		}
		s.cache[viewer] = listCacheEntry{response, time.Now().Add(time.Minute)}
		return response, nil
	})
	if err != nil {
		return standardListsResponse{}, err
	}
	return result.(standardListsResponse), nil
}

func (s *standardLists) load(ctx context.Context, viewer string) (standardListsResponse, error) {
	group, groupCtx := errgroup.WithContext(ctx)
	var own, saves []atproto.RepoRecord
	var ownComplete, savesComplete bool
	group.Go(func() error {
		var err error
		own, ownComplete, err = s.records(groupCtx, viewer, listCollection)
		return err
	})
	group.Go(func() error {
		var err error
		saves, savesComplete, err = s.records(groupCtx, viewer, listSaveCollection)
		return err
	})
	if err := group.Wait(); err != nil {
		return standardListsResponse{}, err
	}
	saved := map[string]bool{}
	for _, record := range saves {
		if raw, ok := record.Value["list"].(string); ok {
			if id, valid := parseListURI(raw, false); valid {
				saved[id.String()] = true
			}
		}
	}
	lists := []standardList{}
	var missing []string
	for _, record := range own {
		id, ok := parseListURI(record.URI, false)
		if !ok || id.Repo != viewer {
			continue
		}
		if row, valid := decodeList(id, record.Value, viewer, saved[record.URI]); valid {
			lists = append(lists, row)
			delete(saved, record.URI)
		}
	}
	for uri := range saved {
		missing = append(missing, uri)
	}
	slices.Sort(missing)
	complete := ownComplete && savesComplete && len(missing) <= 200
	missing = missing[:min(200, len(missing))]
	results := make([]standardList, len(missing))
	var resolves errgroup.Group
	resolves.SetLimit(5)
	for i, uri := range missing {
		resolves.Go(func() error {
			id, _ := parseListURI(uri, false)
			results[i], _ = s.resolve(ctx, viewer, id, true)
			return nil
		})
	}
	_ = resolves.Wait()
	for _, row := range results {
		if row.URI == "" {
			complete = false
		} else {
			lists = append(lists, row)
		}
	}
	s.enrich(ctx, viewer, lists)
	return sortedLists(lists, complete), nil
}

func decodeList(id atproto.ATURI, value map[string]any, viewer string, saved bool) (standardList, bool) {
	name, _ := value["name"].(string)
	created, _ := value["createdAt"].(string)
	if strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > 64 || len(name) > 640 {
		return standardList{}, false
	}
	if _, err := time.Parse(time.RFC3339Nano, created); err != nil {
		return standardList{}, false
	}
	pubs, ok := listMembers(value["publications"], func(uri string) bool {
		at, ok := atproto.ParseRecordURI(uri)
		return ok && at.Collection == "site.standard.publication" && knownDID(at.Repo) && listRKey.MatchString(at.RKey) && at.RKey != "." && at.RKey != ".."
	})
	if !ok {
		return standardList{}, false
	}
	users := []string{}
	if raw, present := value["users"]; present {
		users, ok = listMembers(raw, knownDID)
		if !ok {
			return standardList{}, false
		}
	}
	var description *string
	if text, ok := value["description"].(string); ok {
		if utf8.RuneCountInString(text) > 300 || len(text) > 3000 {
			return standardList{}, false
		}
		description = &text
	}
	return standardList{
		URI: id.String(), Name: name, Description: description, CreatorDID: id.Repo, Publications: pubs, Users: users,
		Owned: id.Repo == viewer, Saved: saved, PublicationDetails: []listPublication{},
	}, true
}

func listMembers(raw any, valid func(string) bool) ([]string, bool) {
	values, ok := raw.([]any)
	if !ok || len(values) > 500 {
		return nil, false
	}
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, item := range values {
		value, ok := item.(string)
		if !ok || !valid(value) {
			return nil, false
		}
		if !seen[value] {
			result = append(result, value)
			seen[value] = true
		}
	}
	return result, true
}

func sortedLists(lists []standardList, complete bool) standardListsResponse {
	slices.SortFunc(lists, func(a, b standardList) int {
		if a.Owned != b.Owned {
			if a.Owned {
				return -1
			}
			return 1
		}
		if value := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); value != 0 {
			return value
		}
		return strings.Compare(a.URI, b.URI)
	})
	return standardListsResponse{Lists: lists, RefreshedAt: time.Now().UTC().Format(time.RFC3339), Complete: complete}
}

func (s *standardLists) enrich(ctx context.Context, viewer string, lists []standardList) {
	sidebar, err := s.publications.Cached(ctx, viewer)
	if err != nil || sidebar == nil {
		return
	}
	byID := map[string]publications.Row{}
	for _, row := range sidebar.AllRows {
		byID[row.PublicationID] = row
	}
	for i, list := range lists {
		for _, uri := range list.Publications {
			if row, ok := byID[uri]; ok {
				lists[i].PublicationDetails = append(lists[i].PublicationDetails, listPublication{uri, row.Title, row.AuthorDID, row.AuthorHandle, row.IconURL, row.AvatarURL})
			}
		}
	}
}

func (s *standardLists) feedScopes(ctx context.Context, viewer string, list standardList) []thinstore.PublicationScope {
	now := time.Now()
	scopes := make([]thinstore.PublicationScope, 0, len(list.Users)+len(list.Publications))
	authors := map[string]bool{}
	for _, did := range list.Users {
		authors[did] = true
		scopes = append(scopes, thinstore.NewPublicationScope(viewer, did, did, nil, nil, nil, nil, now))
	}
	sites := map[string][]string{}
	if sidebar, err := s.publications.Cached(ctx, viewer); err == nil && sidebar != nil {
		for _, row := range sidebar.AllRows {
			sites[row.PublicationID] = row.Scope.PublicationSiteURLs
		}
	}
	for _, uri := range list.Publications {
		id, _ := atproto.ParseRecordURI(uri)
		if !authors[id.Repo] {
			scopes = append(scopes, thinstore.NewPublicationScope(viewer, uri, id.Repo, &uri, []string{uri}, sites[uri], nil, now))
		}
	}
	return scopes
}

type listCursor struct {
	Fingerprint  string `json:"fingerprint"`
	Continuation string `json:"continuation"`
}

func listFingerprint(viewer string, list standardList, filter thinstore.EntryListFilter) string {
	material := slices.Concat([]string{viewer, list.URI, string(filter)}, list.Publications, []string{"authors"}, list.Users)
	digest := sha256.Sum256([]byte(strings.Join(material, "\n")))
	return hex.EncodeToString(digest[:])
}

func decodeListCursor(encoded string, fingerprint string) (*string, error) {
	if len(encoded) > 8192 {
		return nil, httpx.ErrBadRequest
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	var cursor listCursor
	if err != nil || json.Unmarshal(data, &cursor) != nil || cursor.Fingerprint != fingerprint {
		return nil, httpx.ErrBadRequest
	}
	if _, _, ok := thinstore.DecodeCursor(cursor.Continuation); !ok {
		return nil, httpx.ErrBadRequest
	}
	return &cursor.Continuation, nil
}

func encodeListCursor(continuation *string, fingerprint string) *string {
	if continuation == nil {
		return nil
	}
	data, _ := json.Marshal(listCursor{fingerprint, *continuation})
	value := base64.StdEncoding.EncodeToString(data)
	return &value
}

func (s *service) listFeed(w http.ResponseWriter, r *http.Request, id string) error {
	identity, ok := parseListURI(id, false)
	if !ok {
		return invalidFeed(w, r, "Enter a canonical Standard Reader list AT URI.")
	}
	query := r.URL.Query()
	rawCursor, hasCursor := query.Get("cursor"), query.Has("cursor")
	query.Del("cursor")
	args, validation := parsePage(query, false)
	if validation != "" {
		return invalidFeed(w, r, validation)
	}
	viewer := reqctx.ViewerDID(r.Context())
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	start := time.Now()
	var list standardList
	s.lists.mu.Lock()
	cached := s.lists.cache[viewer]
	s.lists.mu.Unlock()
	if cached.expires.After(start) {
		for _, entry := range cached.response.Lists {
			if entry.URI == identity.String() {
				list = entry
				break
			}
		}
	}
	if list.URI == "" {
		var err error
		list, err = s.lists.resolve(ctx, viewer, identity, false)
		if err != nil {
			if ctx.Err() != nil {
				return feedError(w, r, http.StatusServiceUnavailable, "feed_projection_warming",
					"The list projection is warming. Refresh to retry.", true)
			}
			return err
		}
	}
	fingerprint := listFingerprint(viewer, list, args.filter)
	if hasCursor {
		var err error
		args.cursor, err = decodeListCursor(rawCursor, fingerprint)
		if err != nil {
			return invalidFeed(w, r, "List cursor does not match this viewer, list, or filter.")
		}
	}
	requested := s.lists.feedScopes(ctx, viewer, list)
	var page thinstore.EntryListResponse
	err := s.db.Read(ctx, func(tx *sql.Tx) error {
		var err error
		page, err = listEntries(ctx, tx, viewer, requested, args.filter, args.cursor, args.limit, true)
		return err
	})
	if err != nil {
		return feedFailure(w, r, err)
	}
	page.Cursor = encodeListCursor(page.Cursor, fingerprint)
	w.Header().Set("X-AppView-Feed-Source", "materialized_projection")
	w.Header().Set("X-AppView-Membership-Updated-At", start.UTC().Format(time.RFC3339))
	return httpx.OK(w, page)
}
