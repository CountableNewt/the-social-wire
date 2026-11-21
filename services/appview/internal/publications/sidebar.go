package publications

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
	"golang.org/x/sync/errgroup"
)

// Service loads and persists the viewer's publication projection.
type Service struct {
	db   *sqlitedb.DB
	repo *atproto.RepoClient
}

func New(db *sqlitedb.DB, repo *atproto.RepoClient) *Service {
	return &Service{db: db, repo: repo}
}

type Scope struct {
	AuthorDID              string   `json:"authorDid"`
	PublicationAtURI       *string  `json:"publicationAtUri,omitempty"`
	PublicationScopeAtURIs []string `json:"publicationScopeAtUris"`
	PublicationSiteURLs    []string `json:"publicationSiteUrls"`
}

type Row struct {
	PublicationID             string `json:"publicationId"`
	SubscriptionPublicationID string `json:"subscriptionPublicationId,omitempty"`
	AuthorDID                 string `json:"authorDid"`
	AuthorHandle              string `json:"authorHandle,omitempty"`
	Title                     string `json:"title"`
	IconURL                   string `json:"iconUrl,omitempty"`
	AvatarURL                 string `json:"avatarUrl,omitempty"`
	DiscoveredAt              string `json:"discoveredAt"`
	Scope                     Scope  `json:"appViewScope"`
	UnreadCount               *int   `json:"unreadCount,omitempty"`
}

type Folder struct {
	URI   string         `json:"uri"`
	RKey  string         `json:"rkey"`
	Value map[string]any `json:"value"`
}

type Pref struct {
	URI           string         `json:"uri"`
	PublicationID string         `json:"publicationId"`
	Value         map[string]any `json:"value"`
}

type FolderSection struct {
	FolderRKey   string `json:"folderRkey"`
	FolderURI    string `json:"folderUri"`
	Publications []Row  `json:"publications"`
}

type Sidebar struct {
	ViewerDID        string          `json:"viewerDid"`
	Folders          []Folder        `json:"folders"`
	Prefs            []Pref          `json:"publicationPrefs"`
	FolderSections   []FolderSection `json:"folderSections"`
	AllRows          []Row           `json:"allPublicationRows"`
	OwnRows          []Row           `json:"myPublications"`
	UnfolderedRows   []Row           `json:"subscribedUnfoldered"`
	FollowingRows    []Row           `json:"followingTabPublications"`
	EnrollAuthorDIDs []string        `json:"enrollAuthorDids"`
	RefreshedAt      string          `json:"refreshedAt"`
	UnreadCounts     map[string]int  `json:"unreadCountsByPublicationId,omitempty"`
}

func newSidebar(viewer string) *Sidebar {
	return &Sidebar{
		ViewerDID: viewer, Folders: []Folder{}, Prefs: []Pref{}, FolderSections: []FolderSection{},
		AllRows: []Row{}, OwnRows: []Row{}, UnfolderedRows: []Row{}, FollowingRows: []Row{},
		EnrollAuthorDIDs: []string{}, RefreshedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

// Cached accepts both a plain Swift sidebar projection and the bootstrap snapshot envelope.
func (s *Service) Cached(ctx context.Context, viewer string) (*Sidebar, error) {
	var data []byte
	err := s.db.Reader().QueryRowContext(ctx,
		"SELECT json_body FROM sidebar_projection_cache WHERE viewer_did = ?", viewer).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sidebar := newSidebar(viewer)
	var envelope struct {
		Priority *Sidebar `json:"priority"`
		Folders  *struct {
			Sections []FolderSection `json:"folderSections"`
			AllRows  []Row           `json:"allPublicationRows"`
		} `json:"folderPayload"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, nil
	}
	if envelope.Priority != nil && envelope.Priority.ViewerDID == viewer {
		sidebar = envelope.Priority
		if envelope.Folders != nil {
			sidebar.FolderSections = envelope.Folders.Sections
			sidebar.AllRows = envelope.Folders.AllRows
		}
	} else if err := json.Unmarshal(data, sidebar); err != nil || sidebar.ViewerDID != viewer || sidebar.RefreshedAt == "" {
		return nil, nil
	}
	return sidebar, nil
}

func (s *Service) Load(ctx context.Context, auth *reqctx.Auth, refresh bool) (*Sidebar, error) {
	cached, err := s.Cached(ctx, auth.DID)
	if err != nil {
		return nil, err
	}
	if cached != nil && !refresh {
		refreshedAt, _ := time.Parse(time.RFC3339Nano, cached.RefreshedAt)
		if time.Since(refreshedAt) < 5*time.Minute {
			return cached, nil
		}
	}
	live, err := s.discover(ctx, auth)
	if err != nil {
		if cached != nil && !refresh && ctx.Err() == nil {
			return cached, nil
		}
		return nil, err
	}
	if err := s.persist(ctx, live); err != nil {
		return nil, err
	}
	return live, nil
}

func (s *Service) discover(ctx context.Context, auth *reqctx.Auth) (*Sidebar, error) {
	sidebar := newSidebar(auth.DID)
	var folders, prefs, subscriptions, rss, follows []atproto.RepoRecord
	group, groupCtx := errgroup.WithContext(ctx)
	for collection, out := range map[string]*[]atproto.RepoRecord{
		atproto.CollectionFolder: &folders, atproto.CollectionPublicationPrefs: &prefs,
		atproto.CollectionGraphSubscription: &subscriptions, atproto.CollectionSkyreaderSubscription: &rss,
		atproto.CollectionGraphFollow: &follows,
	} {
		group.Go(func() error {
			var err error
			*out, err = s.viewerRecords(groupCtx, auth.DID, collection)
			return err
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}

	subscriptionIDs := map[string]bool{}
	rows := map[string]Row{}
	for _, record := range rss {
		row, ok := rssRow(record, sidebar.RefreshedAt)
		if ok {
			rows[row.PublicationID] = row
			subscriptionIDs[row.PublicationID] = true
		}
	}

	authors := map[string]bool{auth.DID: true}
	for _, follow := range follows[:min(500, len(follows))] {
		if did := str(follow.Value, "subject"); atproto.IsValidDID(did) {
			authors[did] = true
		}
	}
	for _, subscription := range subscriptions {
		id := str(subscription.Value, "publication")
		if at, ok := atproto.ParseRecordURI(id); ok && atproto.IsPublicationRecordCollection(at.Collection) {
			subscriptionIDs[id] = true
			authors[at.Repo] = true
			rows[id] = standardRow(atproto.RepoRecord{URI: id}, sidebar.RefreshedAt)
		}
	}

	group, groupCtx = errgroup.WithContext(ctx)
	group.SetLimit(25)
	type found struct {
		author  string
		records []atproto.RepoRecord
	}
	foundRows := make([]found, len(authors))
	index := 0
	for did := range authors {
		slot := index
		index++
		group.Go(func() error {
			foundRows[slot].author = did
			for _, collection := range []string{"site.standard.publication", "com.standard.publication"} {
				records, err := s.repo.ListAllRecords(groupCtx, nil, did, collection, 100, 3)
				if err == nil {
					foundRows[slot].records = append(foundRows[slot].records, records...)
				}
			}
			return groupCtx.Err()
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	for _, result := range foundRows {
		if len(result.records) == 0 && result.author != auth.DID {
			var indexed bool
			err := s.db.Reader().QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM content_items WHERE author_did=? AND expires_at > ?)",
				result.author, sidebar.RefreshedAt).Scan(&indexed)
			if err != nil {
				return nil, err
			}
			if indexed {
				rows[result.author] = Row{PublicationID: result.author, AuthorDID: result.author,
					AuthorHandle: result.author, Title: result.author, DiscoveredAt: sidebar.RefreshedAt,
					Scope: Scope{result.author, nil, []string{}, []string{}}}
			}
		}
		for _, record := range result.records {
			if _, ok := atproto.ParseRecordURI(record.URI); ok {
				rows[record.URI] = standardRow(record, sidebar.RefreshedAt)
			}
		}
	}
	assembleSidebar(sidebar, rows, subscriptionIDs, folders, prefs)
	return sidebar, nil
}

// The viewer record collections are public. A single gateway-bound DPoP proof
// cannot be replayed for concurrent PDS collection requests.
func (s *Service) viewerRecords(ctx context.Context, viewer, collection string) ([]atproto.RepoRecord, error) {
	var records []atproto.RepoRecord
	var cursor string
	for range 20 {
		page, next, err := s.repo.ListRecords(ctx, nil, atproto.ListRecordsParams{
			Repo: viewer, Collection: collection, Cursor: cursor, Limit: 100, RequireResolvedRepository: true,
		})
		if err != nil {
			return nil, err
		}
		records = append(records, page...)
		if next == "" {
			break
		}
		if next == cursor {
			return nil, atproto.ErrRepoMalformedResponse
		}
		cursor = next
	}
	return records, nil
}

func assembleSidebar(sidebar *Sidebar, rows map[string]Row, subscriptions map[string]bool, folders, prefs []atproto.RepoRecord) {
	sections := map[string]int{}
	for _, folder := range folders {
		key := atproto.RKeyFromURI(folder.URI)
		sidebar.Folders = append(sidebar.Folders, Folder{folder.URI, key, folder.Value})
		sections[key] = len(sidebar.FolderSections)
		sidebar.FolderSections = append(sidebar.FolderSections, FolderSection{key, folder.URI, []Row{}})
	}
	preferences := map[string]map[string]any{}
	for _, pref := range prefs {
		if id := str(pref.Value, "publicationId"); id != "" && preferences[id] == nil {
			preferences[id] = pref.Value
			sidebar.Prefs = append(sidebar.Prefs, Pref{pref.URI, id, pref.Value})
		}
	}
	ordered := make([]Row, 0, len(rows))
	for _, row := range rows {
		ordered = append(ordered, row)
	}
	sort.Slice(ordered, func(i, j int) bool {
		a, b := strings.ToLower(ordered[i].Title), strings.ToLower(ordered[j].Title)
		if a != b {
			return a < b
		}
		return ordered[i].PublicationID < ordered[j].PublicationID
	})
	enrolled := map[string]bool{}
	for _, row := range ordered {
		pref := preferences[row.PublicationID]
		if hidden, _ := pref["hidden"].(bool); hidden {
			continue
		}
		sidebar.AllRows = append(sidebar.AllRows, row)
		if row.AuthorDID != atproto.RSSAuthorDID && !enrolled[row.AuthorDID] {
			sidebar.EnrollAuthorDIDs = append(sidebar.EnrollAuthorDIDs, row.AuthorDID)
			enrolled[row.AuthorDID] = true
		}
		own := row.AuthorDID == sidebar.ViewerDID
		if own {
			sidebar.OwnRows = append(sidebar.OwnRows, row)
		}
		if !own && !subscriptions[row.PublicationID] {
			sidebar.FollowingRows = append(sidebar.FollowingRows, row)
			continue
		}
		key := atproto.RKeyFromURI(str(pref, "folderId"))
		if key == "" {
			key = atproto.RKeyFromURI(str(pref, "folderUri"))
		}
		if index, ok := sections[key]; ok {
			sidebar.FolderSections[index].Publications = append(sidebar.FolderSections[index].Publications, row)
		} else {
			sidebar.UnfolderedRows = append(sidebar.UnfolderedRows, row)
		}
	}
}

func str(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func standardRow(record atproto.RepoRecord, now string) Row {
	at, _ := atproto.ParseRecordURI(record.URI)
	title := str(record.Value, "title")
	if title == "" {
		title = str(record.Value, "name")
	}
	if title == "" {
		title = at.Repo
	}
	sites := []string{}
	for _, field := range []string{"url", "siteUrl", "site", "homepage"} {
		if value, ok := thinstore.NormalizePublicationSiteURL(str(record.Value, field)); ok {
			sites = append(sites, value)
		}
	}
	atURIs := []string{}
	for key := range thinstore.PublicationFilterEquivalenceKeys(record.URI) {
		atURIs = append(atURIs, key)
	}
	slices.Sort(atURIs)
	icon := ""
	for _, field := range []string{"icon", "iconUrl", "iconImage", "iconImageUrl", "avatar", "avatarUrl", "logo", "logoUrl", "favicon"} {
		if value, err := url.Parse(str(record.Value, field)); err == nil && value.Scheme == "https" && value.Hostname() != "" {
			icon = value.String()
			break
		}
	}
	return Row{
		PublicationID: record.URI, SubscriptionPublicationID: record.URI, AuthorDID: at.Repo, AuthorHandle: at.Repo,
		Title: title, DiscoveredAt: now, IconURL: icon,
		Scope: Scope{at.Repo, &record.URI, atURIs, sites},
	}
}

func rssRow(record atproto.RepoRecord, now string) (Row, bool) {
	category := strings.ToLower(str(record.Value, "category"))
	sourceType := strings.ToLower(str(record.Value, "sourceType"))
	if category == "podcast" || (sourceType != "" && sourceType != "rss") {
		return Row{}, false
	}
	feed, ok := thinstore.NormalizeFeedURL(str(record.Value, "feedUrl"))
	if !ok {
		return Row{}, false
	}
	page, _ := url.Parse(feed)
	title := str(record.Value, "customTitle")
	if title == "" {
		title = str(record.Value, "title")
	}
	if title == "" {
		title = page.Hostname()
	}
	icon := str(record.Value, "customIconUrl")
	if icon == "" {
		if site, err := url.Parse(str(record.Value, "siteUrl")); err == nil && site.Hostname() != "" {
			page = site
		}
		icon = (&url.URL{Scheme: page.Scheme, Host: page.Host, Path: "/favicon.ico"}).String()
	}
	return Row{
		PublicationID: atproto.RSSPublicationPrefix + atproto.EncodeBase64URL([]byte(feed)), SubscriptionPublicationID: record.URI,
		AuthorDID: atproto.RSSAuthorDID, AuthorHandle: "RSS", Title: title, IconURL: icon, DiscoveredAt: now,
		Scope: Scope{atproto.RSSAuthorDID, nil, []string{}, []string{feed}},
	}, true
}

// Repository returns the public AT Protocol repository client used by the projection.
func (s *Service) Repository() *atproto.RepoClient { return s.repo }
