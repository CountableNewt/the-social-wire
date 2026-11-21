package thinstore

import (
	"net/url"
	"regexp"
	"strings"
)

// This file ports the RenderFieldExtractor helpers the store's publication
// scope matching depends on (canonical AT-URI keys, site URL normalization and
// matchesPublication).

var publicationRecordCollections = map[string]bool{
	"site.standard.publication": true,
	"com.standard.publication":  true,
}

var atURIPattern = regexp.MustCompile(`^at://([^/]+)/([^/]+)/([^/]+)$`)

// parseAtURI splits `at://did/collection/rkey`.
func parseAtURI(uri string) (did, collection, rkey string, ok bool) {
	match := atURIPattern.FindStringSubmatch(uri)
	if match == nil {
		return "", "", "", false
	}
	return match[1], match[2], match[3], true
}

func normalizedDID(did string) string {
	if strings.HasPrefix(strings.ToLower(did), "did:plc:") {
		return strings.ToLower(did)
	}
	return did
}

// CanonicalPublicationAtURIKey lowercases did:plc identifiers so AT-URI site fields compare stably.
func CanonicalPublicationAtURIKey(uri string) (string, bool) {
	did, collection, rkey, ok := parseAtURI(uri)
	if !ok {
		return "", false
	}
	return "at://" + normalizedDID(did) + "/" + collection + "/" + rkey, true
}

// PublicationFilterEquivalenceKeys returns the canonical key plus the legacy
// com.standard / site.standard publication alias.
func PublicationFilterEquivalenceKeys(publicationAtURI string) map[string]bool {
	keys := map[string]bool{}
	if primary, ok := CanonicalPublicationAtURIKey(publicationAtURI); ok {
		keys[primary] = true
	}
	did, collection, rkey, ok := parseAtURI(publicationAtURI)
	if !ok || !publicationRecordCollections[collection] {
		return keys
	}
	didNorm := normalizedDID(did)
	switch collection {
	case "site.standard.publication":
		keys["at://"+didNorm+"/com.standard.publication/"+rkey] = true
	case "com.standard.publication":
		keys["at://"+didNorm+"/site.standard.publication/"+rkey] = true
	}
	return keys
}

func hasHTTPPrefix(value string) bool {
	lower := strings.ToLower(value)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// NormalizePublicationSiteURL drops query and fragment and a trailing slash from an HTTP(S) URL.
func NormalizePublicationSiteURL(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if !hasHTTPPrefix(trimmed) {
		return "", false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", false
	}
	parsed.Fragment = ""
	parsed.RawFragment = ""
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	href := parsed.String()
	href = strings.TrimSuffix(href, "/")
	return href, true
}

func hasQueryString(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.RawQuery != ""
}

// MatchesPublication reports whether a content row's site field belongs to the
// publication scope (RenderFieldExtractor.matchesPublication).
func MatchesPublication(siteField *string, publicationAtURI *string, publicationScopeAtURIs, publicationSiteURLs []string) bool {
	scoped := publicationAtURI != nil || len(publicationScopeAtURIs) > 0 || len(publicationSiteURLs) > 0
	if !scoped {
		return true
	}
	if siteField == nil {
		return false
	}
	site := *siteField

	wantAtKeys := map[string]bool{}
	if publicationAtURI != nil {
		for key := range PublicationFilterEquivalenceKeys(*publicationAtURI) {
			wantAtKeys[key] = true
		}
	}
	for _, uri := range publicationScopeAtURIs {
		if key, ok := CanonicalPublicationAtURIKey(uri); ok {
			wantAtKeys[key] = true
		}
	}
	if got, ok := CanonicalPublicationAtURIKey(site); ok && wantAtKeys[got] {
		return true
	}

	wantFeedURLs := map[string]bool{}
	for _, raw := range publicationSiteURLs {
		if normalized, ok := NormalizeFeedURL(raw); ok {
			wantFeedURLs[normalized] = true
		}
	}
	if len(wantFeedURLs) > 0 {
		if gotFeed, ok := NormalizeFeedURL(site); ok {
			if wantFeedURLs[gotFeed] {
				return true
			}
			for want := range wantFeedURLs {
				if hasQueryString(want) {
					return false
				}
			}
		}
	}

	wantSiteURLs := map[string]bool{}
	for _, raw := range publicationSiteURLs {
		if normalized, ok := NormalizePublicationSiteURL(raw); ok {
			wantSiteURLs[normalized] = true
		}
	}
	if gotSite, ok := NormalizePublicationSiteURL(site); ok && wantSiteURLs[gotSite] {
		return true
	}
	return false
}

func publicationSiteMatches(siteField *string, publicationAtURI *string, publicationScopeAtURIs, publicationSiteURLs []string) bool {
	return MatchesPublication(siteField, publicationAtURI, publicationScopeAtURIs, publicationSiteURLs)
}

// requiresPublicationSiteFilter reports whether a scope narrows an author's content by site.
func requiresPublicationSiteFilter(publicationAtURI *string, publicationScopeAtURIs, publicationSiteURLs []string) bool {
	return publicationAtURI != nil || len(publicationScopeAtURIs) > 0 || len(publicationSiteURLs) > 0
}
