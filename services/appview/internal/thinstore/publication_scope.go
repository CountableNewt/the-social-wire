package thinstore

import (
	"slices"
	"sort"
	"time"
)

// PublicationScope is a persisted viewer publication scope (AppViewPublicationScope).
type PublicationScope struct {
	ViewerDID              string
	PublicationID          string
	AuthorDID              string
	PublicationAtURI       *string
	PublicationScopeAtURIs []string
	PublicationSiteURLs    []string
	ScopeKeys              []string
	SectionKeys            []string
	UpdatedAt              time.Time
}

// PublicationUnreadScope identifies one publication's content for counting and listing.
type PublicationUnreadScope struct {
	PublicationID          string
	AuthorDID              string
	PublicationAtURI       *string
	PublicationScopeAtURIs []string
	PublicationSiteURLs    []string
}

// UnreadScope narrows a persisted scope to its counting identity.
func (s PublicationScope) UnreadScope() PublicationUnreadScope {
	return PublicationUnreadScope{
		PublicationID: s.PublicationID, AuthorDID: s.AuthorDID, PublicationAtURI: s.PublicationAtURI,
		PublicationScopeAtURIs: s.PublicationScopeAtURIs, PublicationSiteURLs: s.PublicationSiteURLs,
	}
}

// PublicationSiteKeys is the sorted set of stored publication_site spellings a
// scope matches (AppViewProjectionCacheScopeKeys.publicationSiteKeys).
func PublicationSiteKeys(publicationAtURI *string, publicationScopeAtURIs, publicationSiteURLs []string) []string {
	keys := map[string]bool{}
	if publicationAtURI != nil {
		for key := range PublicationFilterEquivalenceKeys(*publicationAtURI) {
			keys[key] = true
		}
	}
	for _, uri := range publicationScopeAtURIs {
		if key, ok := CanonicalPublicationAtURIKey(uri); ok {
			keys[key] = true
		}
	}
	for _, raw := range publicationSiteURLs {
		if normalized, ok := NormalizeFeedURL(raw); ok {
			keys[normalized] = true
		}
		if normalized, ok := NormalizePublicationSiteURL(raw); ok {
			keys[normalized] = true
		}
	}
	return sortedKeys(keys)
}

// ScopeKeys is AppViewUnreadCounterSupport.scopeKeys.
func ScopeKeys(publicationAtURI *string, publicationScopeAtURIs, publicationSiteURLs []string) []string {
	return PublicationSiteKeys(publicationAtURI, publicationScopeAtURIs, publicationSiteURLs)
}

// NewPublicationScope builds a persisted scope with derived scope keys and
// sorted, de-duplicated section keys (AppViewUnreadCounterSupport.publicationScope).
func NewPublicationScope(viewerDID, publicationID, authorDID string, publicationAtURI *string,
	publicationScopeAtURIs, publicationSiteURLs, sectionKeys []string, updatedAt time.Time) PublicationScope {
	set := map[string]bool{}
	for _, key := range sectionKeys {
		set[key] = true
	}
	return PublicationScope{
		ViewerDID: viewerDID, PublicationID: publicationID, AuthorDID: authorDID,
		PublicationAtURI: publicationAtURI, PublicationScopeAtURIs: nonNil(publicationScopeAtURIs),
		PublicationSiteURLs: nonNil(publicationSiteURLs),
		ScopeKeys:           ScopeKeys(publicationAtURI, publicationScopeAtURIs, publicationSiteURLs),
		SectionKeys:         sortedKeys(set), UpdatedAt: updatedAt,
	}
}

// ContentMatchesScope reports whether a content row belongs to a persisted scope.
func ContentMatchesScope(authorDID string, publicationSite *string, scope PublicationScope) bool {
	if scope.AuthorDID != authorDID {
		return false
	}
	if len(scope.ScopeKeys) == 0 {
		return true
	}
	return publicationSiteMatches(publicationSite, scope.PublicationAtURI, scope.PublicationScopeAtURIs,
		scope.PublicationSiteURLs)
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func uniqueSorted(values []string) []string {
	set := map[string]bool{}
	for _, value := range values {
		set[value] = true
	}
	return sortedKeys(set)
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func containsString(values []string, value string) bool { return slices.Contains(values, value) }
