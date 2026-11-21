package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/repo"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

var documentCollections = []string{"site.standard.document", "site.standard.entry"}

type AuthorResolver interface {
	ResolvePDSBaseStrict(context.Context, string, atproto.EndpointPolicy) (string, error)
}

// RefreshAuthors reads one recent page per collection from each author's
// authoritative PDS. It is for the explicitly configured provider-off mode.
func (s *Service) RefreshAuthors(ctx context.Context, authors []string) (int, error) {
	if s.AuthorPDS == nil {
		return 0, nil
	}
	seen := make(map[string]bool)
	total := 0
	for _, raw := range authors {
		did, err := atproto.NormalizeDID(raw)
		if err != nil || (!strings.HasPrefix(did, "did:plc:") && !strings.HasPrefix(did, "did:web:")) || did == thinstore.RssAuthorDID || seen[did] {
			continue
		}
		if len(seen) == 8 {
			break
		}
		seen[did] = true
		s.scheduleAuthorHistory(did)
		if err := ctx.Err(); err != nil {
			return total, err
		}
		base, err := s.AuthorPDS.ResolvePDSBaseStrict(ctx, did, atproto.EndpointPublicHTTPS)
		if err != nil || base == "" {
			continue
		}
		for _, collection := range documentCollections {
			count, err := s.refreshAuthorCollection(ctx, base, did, collection)
			total += count
			if err != nil {
				return total, err
			}
		}
	}
	return total, nil
}

type authorRecord struct {
	URI   string          `json:"uri"`
	CID   string          `json:"cid"`
	Value json.RawMessage `json:"value"`
}

func (s *Service) refreshAuthorCollection(ctx context.Context, base, did, collection string) (int, error) {
	page, _, err := s.loadAuthorPage(ctx, base, did, collection, "")
	if err != nil {
		// A temporarily unavailable public PDS does not discard stored data.
		return 0, ctx.Err()
	}
	return s.indexAuthorPage(ctx, did, collection, page)
}

func (s *Service) loadAuthorPage(ctx context.Context, base, did, collection, cursor string) ([]authorRecord, string, error) {
	limit := 50
	if collection == "site.standard.document" {
		limit = 10
	}
	query := url.Values{
		"repo":       {did},
		"collection": {collection},
		"limit":      {strconv.Itoa(limit)},
		"reverse":    {strconv.FormatBool(!atproto.RelayHostOmitsListRecordsReverse(base))},
	}
	if cursor != "" {
		query.Set("cursor", cursor)
	}
	media := s.Media
	if media == nil {
		media = atproto.PublicMedia
	}
	requestCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	response, err := media.Open(requestCtx, strings.TrimRight(base, "/")+"/xrpc/com.atproto.repo.listRecords?"+query.Encode(),
		12*time.Second, map[string]string{"Accept": "application/json"})
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("public repository returned HTTP %d", response.StatusCode)
	}
	const maximumBody = 8 << 20
	body, err := io.ReadAll(io.LimitReader(response.Body, maximumBody+1))
	if err != nil {
		return nil, "", err
	}
	if len(body) > maximumBody {
		return nil, "", fmt.Errorf("public repository page exceeds response limit")
	}
	var page struct {
		Records []authorRecord `json:"records"`
		Cursor  string         `json:"cursor"`
	}
	if json.Unmarshal(body, &page) != nil || page.Records == nil || len(page.Records) > limit || len(page.Cursor) > 2048 {
		return nil, "", fmt.Errorf("invalid public repository page")
	}
	return page.Records, page.Cursor, nil
}

func (s *Service) indexAuthorPage(ctx context.Context, did, collection string, records []authorRecord) (int, error) {
	now := time.Now().UTC()
	count := 0
	for _, record := range records {
		uri, ok := atproto.ParseRecordURI(record.URI)
		if !ok || uri.Repo != did || uri.Collection != collection || !repo.ValidRecordPath(collection+"/"+uri.RKey) {
			continue
		}
		var value map[string]any
		if json.Unmarshal(record.Value, &value) != nil || value["$type"] != collection || repo.VerifyPublicRecordCID(record.Value, record.CID) != nil {
			continue
		}
		content := documentContent(record.URI, record.CID, did, collection, value, now)
		content.ExpiresAt = now.Add(s.retention())
		if err := s.Store.UpsertContentItem(ctx, content); err != nil {
			return count, fmt.Errorf("index author document: %w", err)
		}
		count++
	}
	return count, ctx.Err()
}

func documentContent(uri, cid, did, collection string, value map[string]any, now time.Time) thinstore.IndexedContentItem {
	text := func(keys ...string) string {
		for _, key := range keys {
			if field, ok := value[key].(string); ok && strings.TrimSpace(field) != "" {
				return strings.TrimSpace(field)
			}
		}
		return ""
	}
	title := text("title", "name")
	if title == "" {
		title = path.Base(strings.Trim(text("path"), "/"))
		if title == "." {
			title = "Untitled"
		}
	}
	published := text("publishedAt", "createdAt", "indexedAt")
	created, err := time.Parse(time.RFC3339Nano, published)
	if err != nil {
		created = now
		if published == "" {
			published = now.Format(time.RFC3339)
		}
	}
	var site string
	for _, key := range []string{"site", "publication", "publicationUri", "publicationId"} {
		if site = text(key); site != "" {
			break
		}
		if ref, ok := value[key].(map[string]any); ok {
			if site, _ = ref["uri"].(string); site != "" {
				break
			}
		}
	}
	article := text("url", "externalUrl", "canonicalUrl", "href", "permalink")
	if article == "" && text("path") != "" {
		if siteURL, ok := thinstore.NormalizePublicationSiteURL(site); ok {
			article = siteURL + "/" + strings.Trim(text("path"), "/")
		}
	}
	article, _ = thinstore.NormalizePublicationSiteURL(article)
	thumbnail := text("thumbnailUrl", "imageUrl", "coverImageUrl")
	thumb, err := url.Parse(thumbnail)
	if err != nil || thumb.Scheme != "https" || thumb.Hostname() == "" || thumb.User != nil {
		thumbnail = ""
	}
	return thinstore.IndexedContentItem{
		URI: uri, CID: cid, AuthorDID: did, Collection: collection,
		CreatedAt: created, IndexedAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour), PublicationSite: optional(site),
		Render: thinstore.ContentRenderFields{
			Title: html.UnescapeString(title), PublishedAt: published, Summary: optional(html.UnescapeString(text("summary", "description"))),
			ThumbnailURL: optional(thumbnail), ArticleURL: optional(article), ContentLabels: thinstore.ContentLabelsFromRecord(value),
		},
	}
}
