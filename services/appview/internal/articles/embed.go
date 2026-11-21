// Package articles resolves verified standard.site article embeds.
package articles

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
	"golang.org/x/net/html"
)

var invalidURL = errors.New("invalid article URL")
var missingArticle = errors.New("verified article not found")
var unavailable = errors.New("article verification unavailable")

type embed struct {
	DocumentURI     string   `json:"documentUri"`
	PublicationURI  string   `json:"publicationUri"`
	PublicationName string   `json:"publicationName"`
	PublicationURL  string   `json:"publicationUrl"`
	ArticleURL      string   `json:"articleUrl"`
	Title           string   `json:"title"`
	Description     string   `json:"description,omitempty"`
	AuthorDID       string   `json:"authorDid"`
	ContentLabels   []string `json:"contentLabels,omitempty"`
}

type cachedEmbed struct {
	value   embed
	expires time.Time
}

type resolver struct {
	page    func(context.Context, string) ([]byte, error)
	record  func(context.Context, atproto.ATURI) (*atproto.RepoRecord, error)
	permits chan struct{}
	mu      sync.Mutex
	cache   map[string]cachedEmbed
}

func Register(group *httpx.Group, repo *atproto.RepoClient) {
	service := &resolver{
		page: loadHTML, permits: make(chan struct{}, 16), cache: make(map[string]cachedEmbed),
		record: func(ctx context.Context, uri atproto.ATURI) (*atproto.RepoRecord, error) {
			return repo.GetRecordWithMetadata(ctx, nil, uri.Repo, uri.Collection, uri.RKey, "")
		},
	}
	group.Get("/v1/publications/article-embed", service.handle)
	group.Get("/xrpc/app.thesocialwire.publication.getArticleEmbed", service.handle)
}

func (s *resolver) handle(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	if reqctx.ViewerDID(ctx) == "" {
		return responseError(w, http.StatusUnauthorized, "Authentication is required")
	}
	value, err := s.resolve(ctx, r.URL.Query().Get("url"))
	switch {
	case errors.Is(err, invalidURL):
		return responseError(w, http.StatusBadRequest, "Verified article unavailable")
	case errors.Is(err, missingArticle):
		return responseError(w, http.StatusNotFound, "Verified article unavailable")
	case err != nil:
		return responseError(w, http.StatusBadGateway, "Verified article unavailable")
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	return httpx.OK(w, value)
}

func responseError(w http.ResponseWriter, status int, message string) error {
	w.Header().Set("Cache-Control", "no-store")
	return httpx.JSON(w, status, map[string]string{"error": http.StatusText(status), "message": message})
}

func (s *resolver) resolve(ctx context.Context, raw string) (embed, error) {
	key, err := articleURL(raw)
	if err != nil {
		return embed{}, err
	}
	s.mu.Lock()
	found, cached := s.cache[key]
	s.mu.Unlock()
	if cached && time.Now().Before(found.expires) {
		return found.value, nil
	}
	select {
	case s.permits <- struct{}{}:
		defer func() { <-s.permits }()
	case <-ctx.Done():
		return embed{}, ctx.Err()
	}
	body, err := s.page(ctx, key)
	if err != nil {
		return embed{}, err
	}
	document, ok := headDocument(body)
	if !ok {
		return embed{}, missingArticle
	}
	record, err := s.record(ctx, document)
	if err != nil {
		return embed{}, err
	}
	if !validRecord(record, document) {
		return embed{}, missingArticle
	}
	publicationURI, _ := record.Value["site"].(string)
	publication, ok := subject(publicationURI)
	if !ok || publication.Collection != "site.standard.publication" {
		return embed{}, missingArticle
	}
	pub, err := s.record(ctx, publication)
	if err != nil {
		return embed{}, err
	}
	if !validRecord(pub, publication) {
		return embed{}, missingArticle
	}
	rawBase, _ := pub.Value["url"].(string)
	base, err := articleURL(rawBase)
	path, _ := record.Value["path"].(string)
	if err != nil || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return embed{}, missingArticle
	}
	canonical, err := articleURL(strings.TrimRight(base, "/") + path)
	baseURL, _ := url.Parse(base)
	canonicalURL, _ := url.Parse(canonical)
	if err != nil || canonical != key || baseURL.Hostname() != canonicalURL.Hostname() {
		return embed{}, missingArticle
	}
	title, _ := record.Value["title"].(string)
	name, _ := pub.Value["name"].(string)
	if title == "" || name == "" {
		return embed{}, missingArticle
	}
	description, _ := record.Value["description"].(string)
	value := embed{
		DocumentURI: document.String(), PublicationURI: publicationURI, PublicationName: name,
		PublicationURL: base, ArticleURL: key, Title: title, Description: description,
		AuthorDID: document.Repo, ContentLabels: thinstore.ContentLabelsFromRecord(record.Value),
	}
	if err := ctx.Err(); err != nil {
		return embed{}, err
	}
	s.mu.Lock()
	if len(s.cache) >= 256 {
		clear(s.cache)
	}
	s.cache[key] = cachedEmbed{value, time.Now().Add(5 * time.Minute)}
	s.mu.Unlock()
	return value, nil
}

func validRecord(record *atproto.RepoRecord, uri atproto.ATURI) bool {
	return record != nil && record.URI == uri.String() && record.Value["$type"] == uri.Collection
}

func articleURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || len(raw) > 4096 || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return "", invalidURL
	}
	parsed.Fragment = ""
	parsed.RawFragment = ""
	return parsed.String(), nil
}

func subject(raw string) (atproto.ATURI, bool) {
	uri, ok := atproto.ParseRecordURI(raw)
	_, didErr := atproto.NormalizeDID(uri.Repo)
	if !ok || didErr != nil || strings.ContainsAny(raw, "?#\\ \t\r\n") ||
		!slices.Contains([]string{"site.standard.document", "site.standard.entry", "site.standard.publication"}, uri.Collection) {
		return atproto.ATURI{}, false
	}
	return uri, true
}

func headDocument(body []byte) (atproto.ATURI, bool) {
	tokenizer := html.NewTokenizer(bytes.NewReader(body))
	inHead := false
	for tokenType := tokenizer.Next(); tokenType != html.ErrorToken; tokenType = tokenizer.Next() {
		switch tokenType {
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			switch token.Data {
			case "body":
				return atproto.ATURI{}, false
			case "head":
				if inHead {
					return atproto.ATURI{}, false
				}
				inHead = true
			case "meta", "link":
				if !inHead {
					continue
				}
				attrs := make(map[string]string, len(token.Attr))
				for _, attr := range token.Attr {
					attrs[attr.Key] = attr.Val
				}
				candidate := ""
				if token.Data == "meta" && (strings.EqualFold(attrs["name"], "at:canonical") || strings.EqualFold(attrs["property"], "at:canonical")) {
					candidate = attrs["content"]
				}
				rels := strings.Fields(attrs["rel"])
				if token.Data == "link" && (slices.Contains(rels, "site.standard.document") || slices.Contains(rels, "site.standard.entry")) {
					candidate = attrs["href"]
				}
				if uri, ok := subject(candidate); ok && uri.Collection != "site.standard.publication" {
					return uri, true
				}
			}
		case html.EndTagToken:
			name, _ := tokenizer.TagName()
			if string(name) == "head" {
				return atproto.ATURI{}, false
			}
		}
	}
	return atproto.ATURI{}, false
}

func loadHTML(ctx context.Context, target string) ([]byte, error) {
	for hop := range 4 {
		response, err := atproto.PublicMedia.Open(ctx, target, 12*time.Second, map[string]string{"Accept": "text/html"})
		if err != nil {
			return nil, err
		}
		switch response.StatusCode {
		case 301, 302, 303, 307, 308:
			next, err := response.Location()
			response.Body.Close()
			if err != nil || hop == 3 {
				return nil, unavailable
			}
			target, err = articleURL(next.String())
			if err != nil {
				return nil, unavailable
			}
		case http.StatusNotFound:
			response.Body.Close()
			return nil, missingArticle
		case http.StatusOK:
			const limit = 1 << 20
			body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
			response.Body.Close()
			if err != nil || len(body) > limit {
				return nil, unavailable
			}
			return body, nil
		default:
			response.Body.Close()
			return nil, unavailable
		}
	}
	return nil, unavailable
}
