package publications

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
	"golang.org/x/net/html"
)

func Register(group *httpx.Group, service *Service) {
	for _, path := range []string{"/v1/publications/sidebar", "/xrpc/app.thesocialwire.publication.getSidebar"} {
		group.Get(path, func(w http.ResponseWriter, r *http.Request) error {
			auth := reqctx.AuthFrom(r.Context())
			if auth == nil {
				return httpx.ErrUnauthorized
			}
			sidebar, err := service.Load(r.Context(), auth, false)
			if err != nil {
				return err
			}
			if httpx.Query(r, "phase") == "priority" {
				sidebar = sidebar.Priority()
			}
			return httpx.OK(w, sidebar)
		})
	}
	for _, path := range []string{"/v1/publications/refresh", "/xrpc/app.thesocialwire.publication.refreshSidebar"} {
		group.Post(path, func(w http.ResponseWriter, r *http.Request) error {
			auth := reqctx.AuthFrom(r.Context())
			if auth == nil {
				return httpx.ErrUnauthorized
			}
			sidebar, err := service.Load(r.Context(), auth, true)
			if err != nil {
				return err
			}
			return httpx.OK(w, sidebar)
		})
	}
	for _, path := range []string{"/v1/publications/resolve", "/xrpc/app.thesocialwire.publication.resolvePublication"} {
		group.Post(path, func(w http.ResponseWriter, r *http.Request) error {
			if reqctx.AuthFrom(r.Context()) == nil {
				return httpx.ErrUnauthorized
			}
			var input struct {
				Input *string `json:"input"`
			}
			if err := httpx.DecodeJSON(r, &input, 0); err != nil {
				return err
			}
			if input.Input == nil {
				return httpx.ErrBadRequest
			}
			return httpx.OK(w, service.resolve(r.Context(), strings.TrimSpace(*input.Input)))
		})
	}
}

func (s *Sidebar) Priority() *Sidebar {
	result := *s
	result.AllRows = append(append(append([]Row{}, s.OwnRows...), s.UnfolderedRows...), s.FollowingRows...)
	seen := map[string]bool{}
	result.AllRows = slices.DeleteFunc(result.AllRows, func(row Row) bool {
		duplicate := seen[row.PublicationID]
		seen[row.PublicationID] = true
		return duplicate
	})
	result.FolderSections = make([]FolderSection, 0, len(s.FolderSections))
	for _, section := range s.FolderSections {
		section.Publications = []Row{}
		result.FolderSections = append(result.FolderSections, section)
	}
	return &result
}

type resolveResult struct {
	Kind             string `json:"kind"`
	PublicationAtURI string `json:"publicationAtUri,omitempty"`
	FeedURL          string `json:"feedUrl,omitempty"`
	SiteURL          string `json:"siteUrl,omitempty"`
}

type resolveResponse struct {
	Result *resolveResult `json:"result,omitempty"`
	Error  string         `json:"error,omitempty"`
}

func standardResult(uri string) resolveResponse {
	return resolveResponse{Result: &resolveResult{Kind: "standard-site", PublicationAtURI: uri}}
}

func (s *Service) resolve(ctx context.Context, input string) resolveResponse {
	if input == "" {
		return resolveResponse{Error: "Enter a link or publication reference."}
	}
	if decoded, err := url.PathUnescape(input); err == nil && strings.HasPrefix(decoded, "at://") {
		return s.resolveATURI(ctx, decoded)
	}
	if !strings.Contains(input, "://") {
		if did, err := s.repo.Resolver().ResolveRepoDID(ctx, input); err == nil && did != "" {
			for _, collection := range atproto.DiscoveryPublicationCollections() {
				rows, _, err := s.repo.ListRecords(ctx, nil, atproto.ListRecordsParams{Repo: did, Collection: collection, Limit: 1})
				if err == nil && len(rows) > 0 {
					return s.resolveATURI(ctx, rows[0].URI)
				}
			}
		}
		if strings.HasPrefix(input, "did:") {
			return resolveResponse{Error: "No site.standard.publication (or com.standard.publication) records found for that DID."}
		}
		input = "https://" + input
	}
	page, ok := thinstore.NormalizeFeedURL(input)
	if !ok {
		return resolveResponse{Error: "invalid url"}
	}
	pageURL, err := url.Parse(page)
	if err != nil {
		return resolveResponse{Error: "invalid url"}
	}
	origin := (&url.URL{Scheme: pageURL.Scheme, Host: pageURL.Host}).String()
	ctx, cancel := context.WithTimeout(ctx, 14*time.Second)
	defer cancel()
	marker, _ := readPublicURL(ctx, origin+"/.well-known/site.standard.publication", 4096)
	if at, ok := atproto.ParseRecordURI(strings.TrimSpace(string(marker))); ok && atproto.IsPublicationRecordCollection(at.Collection) {
		return standardResult(at.String())
	}
	body, finalURL := readPublicURL(ctx, page, 1<<20)
	if finalURL != "" {
		pageURL, _ = url.Parse(finalURL)
	}
	if isFeed(body) {
		return resolveResponse{Result: &resolveResult{Kind: "rss", FeedURL: finalURL, SiteURL: origin}}
	}
	candidates := alternateFeeds(body, pageURL)
	for _, path := range []string{"/feed", "/feed.xml", "/rss", "/rss.xml", "/atom.xml", "/feeds/rss"} {
		candidates = append(candidates, pageURL.ResolveReference(&url.URL{Path: path}).String())
	}
	for _, candidate := range candidates {
		body, finalURL := readPublicURL(ctx, candidate, 1<<20)
		if isFeed(body) {
			return resolveResponse{Result: &resolveResult{Kind: "rss", FeedURL: finalURL, SiteURL: origin}}
		}
	}
	return resolveResponse{Error: "Could not find a standard.site publication marker for this domain or a reachable RSS/Atom feed. Try a publication AT-URI or a direct feed URL."}
}

func (s *Service) resolveATURI(ctx context.Context, raw string) resolveResponse {
	at, ok := atproto.ParseRecordURI(raw)
	if !ok {
		return resolveResponse{Error: "Invalid AT-URI."}
	}
	if at.Collection == "app.offprint.publication" {
		record, _ := s.repo.GetRecordByATURI(ctx, nil, raw)
		if inner, ok := atproto.ParseRecordURI(str(record, "publication")); ok && atproto.IsPublicationRecordCollection(inner.Collection) {
			return standardResult(inner.String())
		}
		return resolveResponse{Error: "Offprint publication record is missing its site.standard.publication reference."}
	}
	if atproto.IsPublicationRecordCollection(at.Collection) {
		return standardResult(raw)
	}
	return resolveResponse{Error: "Unsupported AT-URI — use a publication record (site.standard.publication or com.standard.publication)."}
}

func readPublicURL(ctx context.Context, raw string, maxBytes int64) ([]byte, string) {
	for range 4 {
		normalized, ok := thinstore.NormalizeFeedURL(raw)
		if !ok || ctx.Err() != nil {
			return nil, ""
		}
		parsed, err := url.Parse(normalized)
		if err != nil || parsed.User != nil {
			return nil, ""
		}
		address, err := atproto.RepositoryDNS.ValidatedAddress(ctx, normalized, time.Now().Add(3*time.Second))
		if err != nil {
			return nil, ""
		}
		client := atproto.NewPinnedHTTPClient(address, 3*time.Second, 5*time.Second)
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, normalized, nil)
		if err != nil {
			return nil, ""
		}
		request.Header.Set("User-Agent", "the-social-wire/resolve-publication")
		response, err := client.Do(request)
		if err != nil {
			client.CloseIdleConnections()
			return nil, ""
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
		response.Body.Close()
		client.CloseIdleConnections()
		if response.StatusCode >= 300 && response.StatusCode < 400 {
			location, err := response.Location()
			if err != nil {
				return nil, ""
			}
			raw = location.String()
			continue
		}
		if err != nil || int64(len(body)) > maxBytes || response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, ""
		}
		return body, normalized
	}
	return nil, ""
}

func isFeed(body []byte) bool {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	for {
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		if element, ok := token.(xml.StartElement); ok {
			switch strings.ToLower(element.Name.Local) {
			case "rss", "feed", "rdf":
				return true
			default:
				return false
			}
		}
	}
}

func alternateFeeds(body []byte, page *url.URL) []string {
	tokenizer := html.NewTokenizer(bytes.NewReader(body))
	var feeds []string
	for len(feeds) < 8 {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return feeds
		case html.StartTagToken, html.SelfClosingTagToken:
			element := tokenizer.Token()
			if element.Data != "link" {
				continue
			}
			var href, rel, mime string
			for _, attr := range element.Attr {
				switch attr.Key {
				case "href":
					href = attr.Val
				case "rel":
					rel = attr.Val
				case "type":
					mime = attr.Val
				}
			}
			if !slices.Contains(strings.Fields(strings.ToLower(rel)), "alternate") ||
				(mime != "application/rss+xml" && mime != "application/atom+xml") {
				continue
			}
			if ref, err := url.Parse(href); err == nil && href != "" {
				feeds = append(feeds, page.ResolveReference(ref).String())
			}
		}
	}
	return feeds
}
