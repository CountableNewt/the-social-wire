package thinstore

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

// This file ports the RssFeedIdentity helpers the store needs for feed-URL
// scope keys and article-duplicate suppression.

// RssAuthorDID is the synthetic author of Skyreader RSS rows.
const RssAuthorDID = "did:web:skyreader.rss"

const rssEntryPrefix = "rssentry:"

// NormalizeFeedURL upgrades to HTTPS, drops fragment and credentials, and trims a trailing slash.
func NormalizeFeedURL(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}
	href := trimmed
	if !hasHTTPPrefix(href) {
		href = "https://" + href
	}
	parsed, err := url.Parse(href)
	if err != nil {
		return "", false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	if strings.TrimSpace(parsed.Hostname()) == "" {
		return "", false
	}
	if scheme == "http" {
		parsed.Scheme = "https"
	}
	parsed.Fragment = ""
	parsed.RawFragment = ""
	parsed.User = nil
	out := parsed.String()
	out = strings.TrimSuffix(out, "/")
	return out, true
}

// CanonicalArticleURL normalizes article URLs for dedupe (no query/fragment, no trailing slash).
func CanonicalArticleURL(raw string) (string, bool) {
	if value, ok := NormalizePublicationSiteURL(raw); ok {
		return value, true
	}
	return NormalizeFeedURL(raw)
}

func base64URLToUTF8(payload string) (string, bool) {
	s := strings.NewReplacer("-", "+", "_", "/").Replace(payload)
	if pad := len(s) % 4; pad != 0 {
		s += strings.Repeat("=", 4-pad)
	}
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil || !utf8.Valid(data) {
		return "", false
	}
	return string(data), true
}

// decodeRssEntryID returns the feed URL and stable item key packed into an `rssentry:` id.
func decodeRssEntryID(entryID string) (feedURL, stableItemKey string, ok bool) {
	if !strings.HasPrefix(entryID, rssEntryPrefix) {
		return "", "", false
	}
	inner, decoded := base64URLToUTF8(strings.TrimPrefix(entryID, rssEntryPrefix))
	if !decoded {
		return "", "", false
	}
	var object map[string]string
	if err := json.Unmarshal([]byte(inner), &object); err == nil {
		f, hasF := object["f"]
		k, hasK := object["k"]
		if hasF && hasK {
			return f, k, true
		}
	}
	pipe := strings.IndexByte(inner, '|')
	if pipe < 0 {
		return "", "", false
	}
	feedURL, stableItemKey = inner[:pipe], inner[pipe+1:]
	if feedURL == "" || stableItemKey == "" {
		return "", "", false
	}
	return feedURL, stableItemKey, true
}

func canonicalLinkFromStableItemKey(stableItemKey string) (string, bool) {
	switch {
	case strings.HasPrefix(stableItemKey, "post:"):
		return "", false
	case strings.HasPrefix(stableItemKey, "link:"):
		return CanonicalArticleURL(strings.TrimPrefix(stableItemKey, "link:"))
	case strings.HasPrefix(stableItemKey, "guid:"):
		raw := strings.TrimPrefix(stableItemKey, "guid:")
		if !strings.HasPrefix(strings.ToLower(raw), "http") {
			return "", false
		}
		return CanonicalArticleURL(raw)
	}
	return "", false
}

func rawURLFromStableItemKey(stableItemKey string) (string, bool) {
	if strings.HasPrefix(stableItemKey, "link:") {
		return strings.TrimPrefix(stableItemKey, "link:"), true
	}
	if strings.HasPrefix(stableItemKey, "guid:") {
		return strings.TrimPrefix(stableItemKey, "guid:"), true
	}
	return "", false
}

func allNumeric(value string) bool {
	for _, r := range value {
		if !unicode.IsNumber(r) {
			return false
		}
	}
	return true
}

// postIdentityStableKey maps WordPress / Atom ids like `https://host/?p=936829`
// and path ids like `/936829/` to one `post:host:id` key.
func postIdentityStableKey(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		return "", false
	}
	if postID, ok := firstQueryValue(parsed.RawQuery, "p"); ok && postID != "" && allNumeric(postID) {
		return "post:" + host + ":" + postID, true
	}
	for _, part := range strings.Split(parsed.Path, "/") {
		if part == "" {
			continue
		}
		if utf8.RuneCountInString(part) >= 5 && allNumeric(part) {
			return "post:" + host + ":" + part, true
		}
	}
	return "", false
}

// firstQueryValue reads the first percent-decoded value named name (URLComponents.queryItems).
func firstQueryValue(rawQuery, name string) (string, bool) {
	if rawQuery == "" {
		return "", false
	}
	for _, pair := range strings.Split(rawQuery, "&") {
		key, value, hasValue := strings.Cut(pair, "=")
		decodedKey, err := url.PathUnescape(key)
		if err != nil || decodedKey != name {
			continue
		}
		if !hasValue {
			return "", false
		}
		decodedValue, err := url.PathUnescape(value)
		if err != nil {
			return "", false
		}
		return decodedValue, true
	}
	return "", false
}

func trimmedNonEmpty(value *string) (string, bool) {
	if value == nil {
		return "", false
	}
	trimmed := strings.TrimSpace(*value)
	return trimmed, trimmed != ""
}

// canonicalLink resolves an entry's canonical article link from its render
// articleUrl, its RSS stable item key, or an http summary.
func canonicalLink(entryID string, articleURL *string, summary *string) (string, bool) {
	if article, ok := trimmedNonEmpty(articleURL); ok {
		if canonical, ok := CanonicalArticleURL(article); ok {
			return canonical, true
		}
	}
	if strings.HasPrefix(entryID, rssEntryPrefix) {
		if _, key, ok := decodeRssEntryID(entryID); ok {
			if link, ok := canonicalLinkFromStableItemKey(key); ok {
				return link, true
			}
		}
	}
	if text, ok := trimmedNonEmpty(summary); ok && strings.HasPrefix(strings.ToLower(text), "http") {
		if normalized, ok := CanonicalArticleURL(text); ok {
			return normalized, true
		}
	}
	return "", false
}

func renderArticleURL(renderJSON *string) *string {
	if renderJSON == nil {
		return nil
	}
	render, ok := DecodeRenderFields(*renderJSON)
	if !ok {
		return nil
	}
	return render.ArticleURL
}

// dedupeIdentityKeys collapses legacy Atom `?p=` rows with path-based link rows.
func dedupeIdentityKeys(entryID string, renderJSON *string, summary *string) map[string]bool {
	keys := map[string]bool{}
	articleURL := renderArticleURL(renderJSON)
	if canonical, ok := canonicalLink(entryID, articleURL, summary); ok {
		keys["url:"+canonical] = true
	}
	if strings.HasPrefix(entryID, rssEntryPrefix) {
		if _, key, ok := decodeRssEntryID(entryID); ok {
			if strings.HasPrefix(key, "post:") {
				keys[key] = true
			}
			if raw, ok := rawURLFromStableItemKey(key); ok {
				if postKey, ok := postIdentityStableKey(raw); ok {
					keys[postKey] = true
				}
			}
		}
	}
	if article, ok := trimmedNonEmpty(articleURL); ok {
		if canonical, ok := CanonicalArticleURL(article); ok {
			keys["url:"+canonical] = true
		}
		if postKey, ok := postIdentityStableKey(article); ok {
			keys[postKey] = true
		}
	}
	if text, ok := trimmedNonEmpty(summary); ok && strings.HasPrefix(strings.ToLower(text), "http") {
		if postKey, ok := postIdentityStableKey(text); ok {
			keys[postKey] = true
		}
	}
	return keys
}

func registersAsDuplicateIdentity(keys map[string]bool, seen map[string]bool) bool {
	if len(keys) == 0 {
		return false
	}
	for key := range keys {
		if seen[key] {
			return true
		}
	}
	for key := range keys {
		seen[key] = true
	}
	return false
}

func keySetsIntersect(left, right map[string]bool) bool {
	for key := range left {
		if right[key] {
			return true
		}
	}
	return false
}

// originalArticleURL is the browser-openable permalink for an indexed entry.
func originalArticleURL(entryID string, render *ContentRenderFields, summary *string) *string {
	var articleURL *string
	if render != nil {
		articleURL = render.ArticleURL
	}
	if link, ok := canonicalLink(entryID, articleURL, summary); ok {
		return &link
	}
	return nil
}

// DedupeEntryListItems suppresses article duplicates, preferring a duplicate with a thumbnail.
func DedupeEntryListItems(items []EntryListItem) []EntryListItem {
	seenEntryIDs := map[string]bool{}
	seenIdentityKeys := map[string]bool{}
	seenTitlePublished := map[string]bool{}
	deduped := make([]EntryListItem, 0, len(items))
	for _, item := range items {
		if seenEntryIDs[item.EntryID] {
			continue
		}
		seenEntryIDs[item.EntryID] = true
		identityKeys := dedupeIdentityKeys(item.EntryID, nil, item.Summary)
		if registersAsDuplicateIdentity(identityKeys, seenIdentityKeys) {
			for index, existing := range deduped {
				if keySetsIntersect(dedupeIdentityKeys(existing.EntryID, nil, existing.Summary), identityKeys) {
					if stringValue(existing.ThumbnailURL) == "" && stringValue(item.ThumbnailURL) != "" {
						deduped[index] = item
					}
					break
				}
			}
			continue
		}
		if len(identityKeys) == 0 {
			titleKey := strings.ToLower(strings.TrimSpace(item.Title)) + "|" + itoa64(item.PublishedAt.Unix())
			if seenTitlePublished[titleKey] {
				continue
			}
			seenTitlePublished[titleKey] = true
		}
		deduped = append(deduped, item)
	}
	return deduped
}
