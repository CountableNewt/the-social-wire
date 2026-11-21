package analytics

import (
	"context"

	"errors"
	"fmt"

	"net/url"
	"strings"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
)

const postCollection = "app.bsky.feed.post"
const articleCollection = "site.standard.document"
const publicationCollection = "site.standard.publication"

type Provider struct {
	repo *atproto.RepoClient
}

func (p Provider) records(ctx context.Context, author, collection, cursor string) ([]atproto.RepoRecord, string, error) {
	return p.repo.ListRecords(ctx, nil, atproto.ListRecordsParams{
		Repo: author, Collection: collection, Cursor: cursor, Limit: 100, RequireResolvedRepository: true,
	})
}

func parseSubject(uri string) (string, string, string, error) {
	if !strings.HasPrefix(uri, "at://") || len(uri) > 2048 || strings.ContainsAny(uri, "?#@\\ \r\n\t") {
		return "", "", "", errors.New("invalid subject")
	}
	parts := strings.Split(strings.TrimPrefix(uri, "at://"), "/")
	if len(parts) != 3 || !strings.HasPrefix(parts[0], "did:") || parts[2] == "" || strings.Contains(parts[2], "%") || parts[2] == "." || parts[2] == ".." || len(parts[2]) > 512 || len(strings.SplitN(parts[0], ":", 3)) != 3 || strings.SplitN(parts[0], ":", 3)[2] == "" {
		return "", "", "", errors.New("invalid subject")
	}
	return parts[0], parts[1], parts[2], nil
}
func Subject(uri string) (string, string, string, error) {
	d, c, k, e := parseSubject(uri)
	if e != nil {
		return "", "", "", e
	}
	if c != "app.bsky.feed.post" && c != "site.standard.document" {
		return "", "", "", errors.New("unsupported subject")
	}
	return d, c, k, nil
}
func (p Provider) Validate(ctx context.Context, uri string) error {
	d, c, k, e := Subject(uri)
	if e != nil {
		return e
	}
	r, e := p.repo.GetRecordWithMetadata(ctx, nil, d, c, k, "")
	if e != nil {
		return e
	}
	if r == nil || r.URI != uri || stringValue(r.Value, "$type") != c {
		return errors.New("invalid canonical subject")
	}
	return nil
}
func stringValue(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

// Reads public records using the same policy-bound transport as all public PDS reads.
func (p Provider) Inventory(ctx context.Context, did string, now time.Time) ([]Item, bool, error) {
	items := []Item{}
	complete := true
	publications := map[string]*atproto.RepoRecord{}
	for _, collection := range []string{"app.bsky.feed.post", "site.standard.document"} {
		records := []atproto.RepoRecord{}
		cursor := ""
		seen := map[string]bool{}
		identities := map[string]bool{}
		for pageNumber := 0; pageNumber < 10; pageNumber++ {
			page, next, e := p.records(ctx, did, collection, cursor)
			if e != nil {
				return nil, false, e
			}
			if len(page) > 100 {
				return nil, false, errors.New("inventory page exceeded limit")
			}
			for _, r := range page {
				if identities[r.URI] {
					return nil, false, errors.New("duplicate inventory subject")
				}
				identities[r.URI] = true
				records = append(records, r)
			}
			if next == "" {
				cursor = ""
				break
			}
			if seen[next] {
				return nil, false, errors.New("repeated inventory cursor")
			}
			seen[next] = true
			cursor = next
		}
		if cursor != "" {
			complete = false
		}
		for _, r := range records {
			d, c, rkey, e := Subject(r.URI)
			if e != nil || d != did || c != collection || stringValue(r.Value, "$type") != collection {
				return nil, false, errors.New("invalid owned inventory")
			}
			kind, title, date := "post", stringValue(r.Value, "text"), stringValue(r.Value, "createdAt")
			link := fmt.Sprintf("https://bsky.app/profile/%s/post/%s", did, rkey)
			if collection == "site.standard.document" {
				kind, title, date = "article", stringValue(r.Value, "title"), stringValue(r.Value, "publishedAt")
				link = ""
				site := stringValue(r.Value, "site")
				sd, sc, sk, e := publicationSubject(site)
				if e == nil && sd == did {
					pub, known := publications[site]
					var e error
					if !known && len(publications) < 25 {
						pub, e = p.repo.GetRecordWithMetadata(ctx, nil, sd, sc, sk, "")
						publications[site] = pub
					}
					if e == nil && pub != nil && pub.URI == site && stringValue(pub.Value, "$type") == "site.standard.publication" {
						base := stringValue(pub.Value, "url")
						b, e := url.Parse(base)
						path := stringValue(r.Value, "path")
						if e == nil && b.Scheme == "https" && b.Host != "" && b.User == nil && strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "//") {
							b.Path = strings.TrimRight(b.Path, "/") + path
							b.RawQuery = ""
							b.RawPath = ""
							b.ForceQuery = false
							b.Fragment = ""
							link = b.String()
						}
					}
				}
			}
			items = append(items, Item{URI: r.URI, Kind: kind, Title: title, URL: link, PublishedAt: date, RefreshedAt: timestamp(now)})
		}
	}
	byURI := make(map[string]int, len(items))
	for i, item := range items {
		byURI[item.URI] = i
	}
	for start := 0; start < len(items); start += 25 {
		uris := []string{}
		for _, it := range items[start:min(start+25, len(items))] {
			if it.Kind == "post" {
				uris = append(uris, it.URI)
			}
		}
		if len(uris) == 0 {
			continue
		}
		var data struct {
			Posts []bskyPost `json:"posts"`
		}
		if e := p.publicJSON(ctx, "https://public.api.bsky.app/xrpc/app.bsky.feed.getPosts",
			url.Values{"uris": uris}, 20*time.Second, 2<<20, &data); e != nil {
			return nil, false, e
		}
		for _, post := range data.Posts {
			if post.Author.DID != did {
				return nil, false, errors.New("engagement author mismatch")
			}
			if post.Likes == nil || post.Replies == nil || post.Reposts == nil || post.Quotes == nil {
				continue
			}
			if min(*post.Likes, *post.Replies, *post.Reposts, *post.Quotes) < 0 {
				return nil, false, errors.New("negative engagement")
			}
			if i, found := byURI[post.URI]; found {
				items[i].Engagement = &Engagement{*post.Likes, *post.Replies, *post.Reposts, *post.Quotes}
			}
		}
	}
	return items, complete, nil
}
func publicationSubject(uri string) (string, string, string, error) {
	d, c, k, e := parseSubject(uri)
	if e != nil || c != "site.standard.publication" {
		return "", "", "", errors.New("invalid publication")
	}
	return d, c, k, nil
}
