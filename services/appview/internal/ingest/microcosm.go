package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/repo"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
	"golang.org/x/sync/singleflight"
)

type PublicDiscoveryStore interface {
	InsertContentItemIfAbsent(context.Context, thinstore.IndexedContentItem) (bool, error)
	HasContentItem(context.Context, string) (bool, error)
}

type DiscoveryScope struct {
	Author string
	Keys   []string
}

// Microcosm refreshes known publication scopes with bounded, public, positive-only indexing.
type Microcosm struct {
	client atproto.HTTPDoer
	store  PublicDiscoveryStore
	queue  chan DiscoveryScope
	mu     sync.Mutex
	active map[string]bool
	slots  chan struct{}
	checks singleflight.Group
}

func NewMicrocosm(client atproto.HTTPDoer, store PublicDiscoveryStore) *Microcosm {
	return &Microcosm{client: client, store: store, queue: make(chan DiscoveryScope, 64), active: make(map[string]bool), slots: make(chan struct{}, 4)}
}

func normalizeDiscoveryScope(scope DiscoveryScope) DiscoveryScope {

	did, err := atproto.NormalizeDID(scope.Author)
	if err != nil || len(did) > 2048 || did == atproto.RSSAuthorDID ||
		(!strings.HasPrefix(did, "did:plc:") && !strings.HasPrefix(did, "did:web:")) {
		return DiscoveryScope{}
	}
	keys := make([]string, 0, min(len(scope.Keys), 8))
	for _, raw := range scope.Keys {
		if key := publicKey(did, raw); key != "" {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	keys = slices.Compact(keys)
	if len(keys) == 0 {
		return DiscoveryScope{}
	}
	return DiscoveryScope{Author: did, Keys: keys[:min(len(keys), 8)]}
}

func (m *Microcosm) Schedule(scope DiscoveryScope) {
	scope = normalizeDiscoveryScope(scope)
	if scope.Author == "" {
		return
	}
	id := discoveryID(scope)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active[id] {
		return
	}
	select {
	case m.queue <- scope:
		m.active[id] = true
	default:
	}
}

func discoveryID(scope DiscoveryScope) string {
	return scope.Author + "\x00" + strings.Join(scope.Keys, "\x00")
}

func publicKey(author, raw string) string {
	if len(raw) > 2048 || strings.ContainsAny(raw, "?#\\ \r\n\t") {
		return ""
	}
	if uri, ok := atproto.ParseRecordURI(raw); ok {
		if uri.Repo == author && uri.Collection == "site.standard.publication" && repo.ValidRecordPath(uri.Collection+"/"+uri.RKey) && !strings.Contains(raw, "%") {
			return raw
		}
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return ""
	}
	normalized, ok := thinstore.NormalizePublicationSiteURL(raw)
	if ok {
		return normalized
	}
	return ""
}

func (m *Microcosm) Run(ctx context.Context, onError func(error)) {
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return
		case scope := <-m.queue:
			_, err := m.Check(ctx, scope)
			m.mu.Lock()
			delete(m.active, discoveryID(scope))
			m.mu.Unlock()
			if err != nil && ctx.Err() == nil {
				onError(err)
			}
		}
	}
}

type backlink struct {
	DID        string `json:"did"`
	Collection string `json:"collection"`
	RKey       string `json:"rkey"`
}

// Check waits for an on-demand refresh of a known publication.
func (m *Microcosm) Check(ctx context.Context, scope DiscoveryScope) (int, error) {
	scope = normalizeDiscoveryScope(scope)
	if scope.Author == "" {
		return 0, nil
	}
	flight := m.checks.DoChan(discoveryID(scope), func() (any, error) {
		select {
		case m.slots <- struct{}{}:
			defer func() { <-m.slots }()
		default:
			return 0, errDiscoveryUnavailable
		}
		ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()
		return m.discover(ctx, scope)
	})
	select {
	case result := <-flight:
		count, _ := result.Val.(int)
		return count, result.Err
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func (m *Microcosm) discover(ctx context.Context, scope DiscoveryScope) (int, error) {
	indexed := 0
	const pageSize, maxPages, maxRecords, maxRequests = 25, 2, 50, 8
	seen := make(map[string]bool)
	requests := 0
	for _, target := range scope.Keys {
		for _, collection := range documentCollections {
			cursors := map[string]bool{}
			query := url.Values{
				"subject": {target}, "source": {collection + ":site"}, "did": {scope.Author},
				"limit": {"25"}, "reverse": {"false"},
			}
			for range maxPages {
				if requests >= maxRequests || len(seen) >= maxRecords {
					return indexed, nil
				}
				requests++
				var page struct {
					Records []backlink `json:"records"`
					Cursor  string     `json:"cursor"`
				}
				err := m.fetch(ctx, "https://constellation.microcosm.blue/xrpc/blue.microcosm.links.getBacklinks", query, &page)
				if err != nil {
					return indexed, err
				}
				if page.Records == nil || len(page.Records) > pageSize {
					return indexed, errDiscoveryInvalid
				}
				for _, link := range page.Records {
					if len(seen) >= maxRecords {
						return indexed, nil
					}
					if link.DID != scope.Author || link.Collection != collection ||
						!repo.ValidRecordPath(collection+"/"+link.RKey) {
						return indexed, errDiscoveryInvalid
					}
					uri := atproto.BuildRecordURI(link.DID, collection, link.RKey)
					if seen[uri] {
						continue
					}
					seen[uri] = true
					inserted, err := m.hydrate(ctx, scope, link, uri)
					if err != nil {
						return indexed, err
					}
					if inserted {
						indexed++
					}
				}
				if page.Cursor == "" {
					break
				}
				if len(page.Cursor) > 2048 || cursors[page.Cursor] {
					return indexed, errDiscoveryInvalid
				}
				cursors[page.Cursor] = true
				query.Set("cursor", page.Cursor)
			}
		}
	}
	return indexed, nil
}

func (m *Microcosm) hydrate(ctx context.Context, scope DiscoveryScope, link backlink, uri string) (bool, error) {
	known, err := m.store.HasContentItem(ctx, uri)
	if err != nil || known {
		return false, err
	}
	query := url.Values{"repo": {link.DID}, "collection": {link.Collection}, "rkey": {link.RKey}}
	var record struct {
		URI   string          `json:"uri"`
		CID   string          `json:"cid"`
		Value json.RawMessage `json:"value"`
	}
	err = m.fetch(ctx, "https://slingshot.microcosm.blue/xrpc/com.atproto.repo.getRecord", query, &record)
	if errors.Is(err, errDiscoveryUnavailable) && ctx.Err() == nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var value map[string]any
	if record.URI != uri || repo.VerifyPublicRecordCID(record.Value, record.CID) != nil ||
		json.Unmarshal(record.Value, &value) != nil || value["$type"] != link.Collection {
		return false, errDiscoveryInvalid
	}
	content := documentContent(uri, record.CID, link.DID, link.Collection, value, time.Now().UTC())
	if content.PublicationSite == nil || !slices.Contains(scope.Keys, publicKey(link.DID, *content.PublicationSite)) {
		return false, errDiscoveryInvalid
	}
	return m.store.InsertContentItemIfAbsent(ctx, content)
}

var errDiscoveryInvalid = errors.New("invalid public discovery response")
var errDiscoveryUnavailable = errors.New("public discovery provider unavailable")

func (m *Microcosm) fetch(ctx context.Context, endpoint string, query url.Values, result any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "TheSocialWire/1 (+https://thesocialwire.app)")
	response, err := m.client.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %v", errDiscoveryUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errDiscoveryUnavailable
	}
	const limit = 2 << 20
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || len(body) > limit || json.Unmarshal(body, result) != nil {
		return errDiscoveryInvalid
	}
	return nil
}
