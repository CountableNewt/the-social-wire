package ingest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
	"github.com/mmcdole/gofeed"
)

type feedRefresh struct {
	done    chan struct{}
	indexed int
	err     error
}

// RefreshFeed fetches and indexes an already authorized canonical feed URL.
// Concurrent requests share the same refresh so their writes cannot commit
// different versions of the ETag and error backoff state out of order.
func (s *Service) RefreshFeed(ctx context.Context, feed string) (int, error) {
	refresh := &feedRefresh{done: make(chan struct{})}
	if existing, loaded := s.feedRefreshes.LoadOrStore(feed, refresh); loaded {
		prior := existing.(*feedRefresh)
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-prior.done:
			return prior.indexed, prior.err
		}
	}
	defer s.feedRefreshes.Delete(feed)
	defer close(refresh.done)
	refresh.indexed, refresh.err = s.refreshFeed(ctx, feed)
	return refresh.indexed, refresh.err
}

func (s *Service) refreshFeed(ctx context.Context, feed string) (int, error) {
	now := time.Now().UTC()
	previous, err := s.Store.FetchRssFeedMetadata(ctx, feed)
	if err != nil {
		return 0, err
	}
	meta := thinstore.RssFeedFetchMetadata{NormalizedFeedURL: feed, LastPollAt: &now}
	headers := map[string]string{
		"Accept":     "application/rss+xml, application/atom+xml, application/feed+json, application/xml, text/xml, */*",
		"User-Agent": "the-social-wire/thin-appview",
	}
	if previous != nil {
		if previous.BackoffUntil != nil && previous.BackoffUntil.After(now) {
			return 0, rssSourceFailure(feed, "rss_backoff")
		}
		meta.ETag, meta.LastModified = previous.ETag, previous.LastModified
		if meta.ETag != nil {
			headers["If-None-Match"] = *meta.ETag
		}
		if meta.LastModified != nil {
			headers["If-Modified-Since"] = *meta.LastModified
		}
	}
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	status, validators, body, err := s.fetchFeed(requestCtx, feed, headers)
	var parsed *gofeed.Feed
	if err == nil && status != http.StatusNotModified {
		parsed, err = gofeed.NewParser().Parse(bytes.NewReader(body))
	}
	if err != nil {
		meta.ConsecutiveErrorCount = 1
		if previous != nil {
			meta.ConsecutiveErrorCount = min(previous.ConsecutiveErrorCount+1, 8)
		}
		until := now.Add(min(6*time.Hour, 5*time.Minute*time.Duration(1<<max(0, meta.ConsecutiveErrorCount-1))))
		meta.BackoffUntil = &until
		if storeErr := s.Store.StoreRssFeedMetadata(ctx, meta); storeErr != nil {
			return 0, storeErr
		}
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, rssSourceFailure(feed, "rss_fetch")
	}
	if tag := validators.Get("ETag"); tag != "" {
		meta.ETag = &tag
	}
	if modified := validators.Get("Last-Modified"); modified != "" {
		meta.LastModified = &modified
	}
	var contents []thinstore.IndexedContentItem
	if parsed != nil {
		sort.SliceStable(parsed.Items, func(i, j int) bool {
			return itemPublished(parsed.Items[i], now).After(itemPublished(parsed.Items[j], now))
		})
		limit := s.MaximumRSSItems
		if limit <= 0 {
			limit = 200
		}
		existing := map[string]thinstore.IndexedContentIdentity{}
		if store, ok := s.Store.(interface {
			ExistingRSSIdentities(context.Context, string) (map[string]thinstore.IndexedContentIdentity, error)
		}); ok {
			existing, err = store.ExistingRSSIdentities(ctx, feed)
			if err != nil {
				return 0, err
			}
		}
		seen := make(map[string]bool)
		for _, item := range parsed.Items[:min(limit, len(parsed.Items))] {
			content, key := feedContent(feed, item, now)
			content.ExpiresAt = now.Add(s.retention())
			if old, ok := existing[key]; ok {
				content.URI, content.CID = old.URI, old.CID
			}
			if seen[content.URI] {
				continue
			}
			seen[content.URI] = true
			contents = append(contents, content)
		}
	}
	return s.storeFeed(ctx, contents, meta)
}

// Keep the narrow Store interface usable by alternate ingestion stores. The
// production store atomically batches content and its conditional-GET checkpoint.
func (s *Service) storeFeed(ctx context.Context, contents []thinstore.IndexedContentItem, meta thinstore.RssFeedFetchMetadata) (int, error) {
	if batch, ok := s.Store.(interface {
		StoreRSSFeed(context.Context, []thinstore.IndexedContentItem, thinstore.RssFeedFetchMetadata) error
	}); ok {
		if err := batch.StoreRSSFeed(ctx, contents, meta); err != nil {
			return 0, err
		}
		return len(contents), nil
	}
	for index, content := range contents {
		if err := s.Store.UpsertContentItem(ctx, content); err != nil {
			return index, err
		}
	}
	return len(contents), s.Store.StoreRssFeedMetadata(ctx, meta)
}

func (s *Service) fetchFeed(ctx context.Context, raw string, headers map[string]string) (int, http.Header, []byte, error) {
	media := s.Media
	if media == nil {
		media = atproto.PublicMedia
	}
	for range 6 {
		response, err := media.Open(ctx, raw, 20*time.Second, headers)
		if err != nil {
			return 0, nil, nil, err
		}
		status := response.StatusCode
		switch status {
		case 301, 302, 303, 307, 308:
			location, err := response.Location()
			response.Body.Close()
			if err != nil {
				return status, nil, nil, err
			}
			raw = location.String()
		case http.StatusNotModified:
			response.Body.Close()
			return status, response.Header, nil, nil
		case http.StatusOK:
			const maxFeedBytes = 2 << 20
			body, err := io.ReadAll(io.LimitReader(response.Body, maxFeedBytes+1))
			response.Body.Close()
			if len(body) > maxFeedBytes {
				err = fmt.Errorf("RSS response exceeds size limit")
			}
			return status, response.Header, body, err
		default:
			response.Body.Close()
			return status, nil, nil, fmt.Errorf("RSS provider returned HTTP %d", status)
		}
	}
	return 0, nil, nil, fmt.Errorf("RSS redirect limit exceeded")
}

func feedContent(feed string, item *gofeed.Item, now time.Time) (thinstore.IndexedContentItem, string) {
	published := itemPublished(item, now)
	title := html.UnescapeString(strings.TrimSpace(item.Title))
	if title == "" {
		title = strings.TrimSpace(item.Link)
	}
	if title == "" {
		title = "Untitled"
	}
	publishedISO := published.UTC().Format(time.RFC3339Nano)
	key := stableItemKey(item.Link, item.GUID, title, publishedISO)
	var identity bytes.Buffer
	encoder := json.NewEncoder(&identity)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(struct {
		Feed string `json:"f"`
		Key  string `json:"k"`
	}{feed, key})
	uri := "rssentry:" + base64.RawURLEncoding.EncodeToString(bytes.TrimSuffix(identity.Bytes(), []byte("\n")))
	digest := sha256.Sum256([]byte(uri))
	article, _ := thinstore.CanonicalArticleURL(item.Link)
	thumbnail := feedThumbnail(feed, item)

	body := RSSHTMLBody(optional(item.Content), optional(item.Description))
	summary := strings.TrimSpace(html.UnescapeString(item.Description))
	if summary == "" && item.Link != title {
		summary = item.Link
	}
	return thinstore.IndexedContentItem{
		URI: uri, CID: "rss:" + hex.EncodeToString(digest[:16]), AuthorDID: thinstore.RssAuthorDID,
		Collection: "app.skyreader.feed.entry", PublicationSite: &feed,
		CreatedAt: published, IndexedAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour),
		Render: thinstore.ContentRenderFields{
			Title: title, PublishedAt: publishedISO, ArticleURL: optional(article),
			Summary: optional(summary), ContentHTML: &body, ThumbnailURL: thumbnail,
		},
	}, key
}

func feedThumbnail(feed string, item *gofeed.Item) *string {
	explicit := ""
	for _, node := range item.Extensions["media"]["thumbnail"] {
		if explicit = strings.TrimSpace(node.Attrs["url"]); explicit != "" {
			break
		}
	}
	if explicit == "" && item.Image != nil {
		explicit = item.Image.URL
	}
	if explicit == "" {
		for _, enclosure := range item.Enclosures {
			if AcceptsMediaURL(enclosure.URL, optional(enclosure.Type), nil) {
				explicit = enclosure.URL
				break
			}
		}
	}
	return ResolveThumbnail(explicit, item.Content, item.Description, optional(item.Link), &feed)
}

func itemPublished(item *gofeed.Item, fallback time.Time) time.Time {
	if item.PublishedParsed != nil {
		return *item.PublishedParsed
	}
	if item.UpdatedParsed != nil {
		return *item.UpdatedParsed
	}
	return fallback
}

func (s *Service) retention() time.Duration {
	if s.ContentRetention > 0 {
		return s.ContentRetention
	}
	return 30 * 24 * time.Hour
}

func optional(text string) *string {
	if text == "" {
		return nil
	}
	return &text
}

func stableItemKey(link, guid, title, date string) string {
	for _, raw := range []string{link, guid} {
		parsed, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || parsed.Hostname() == "" {
			continue
		}
		prefix := "post:" + strings.ToLower(parsed.Hostname()) + ":"
		if post := parsed.Query().Get("p"); numeric(post) {
			return prefix + post
		}
		for _, segment := range strings.Split(parsed.Path, "/") {
			if len([]rune(segment)) >= 5 && numeric(segment) {
				return prefix + segment
			}
		}
	}
	for index, raw := range []string{link, guid} {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if normalized, ok := thinstore.NormalizeFeedURL(value); ok {
			value = normalized
		}
		if index == 0 {
			return "link:" + value
		}
		return "guid:" + value
	}
	return "fallback:" + strings.TrimSpace(title) + "\n" + date
}

func numeric(value string) bool {
	return value != "" && strings.IndexFunc(value, func(r rune) bool { return !unicode.IsNumber(r) }) == -1
}

func rssSourceFailure(feed, category string) error {
	host := ""
	if parsed, err := url.Parse(feed); err == nil {
		host = strings.ToLower(parsed.Hostname())
	}
	return &thinstore.RSSSourceFailure{
		PublicationID: atproto.RSSPublicationPrefix + atproto.EncodeBase64URL([]byte(feed)),
		Host:          host, Category: category,
	}
}
