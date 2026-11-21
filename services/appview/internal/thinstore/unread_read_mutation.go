package thinstore

import (
	"encoding/json"
	"sort"
	"time"
)

// UnreadReadMutationEntry is the minimal projection for age counts and
// snapshot-based read mutations; publication identity is the first matching scope.
type UnreadReadMutationEntry struct {
	EntryID        string
	PublishedAt    time.Time
	FeedPositionAt time.Time
	PublicationID  string
}

// UnreadReadMutationPage uses the same created-at/URI keyset ordering as the unread query.
type UnreadReadMutationPage struct {
	Entries []UnreadReadMutationEntry
	Cursor  *string
}

func unreadReadMutationPage(matches []UnreadReadMutationEntry, limit int) UnreadReadMutationPage {
	entries := matches
	if len(entries) > limit {
		entries = entries[:limit]
	}
	page := UnreadReadMutationPage{Entries: append([]UnreadReadMutationEntry{}, entries...)}
	if len(matches) > limit && len(entries) > 0 {
		last := entries[len(entries)-1]
		cursor := EncodeCursor(last.FeedPositionAt, last.EntryID)
		page.Cursor = &cursor
	}
	return page
}

// unreadReadMutationScope keeps read snapshots independent of persisted sidebar projections.
type unreadReadMutationScope struct {
	PublicationID string   `json:"publicationId"`
	AuthorDID     string   `json:"authorDid"`
	ScopeKeys     []string `json:"scopeKeys"`
	Position      int      `json:"position"`
	Unscoped      bool     `json:"unscoped"`
}

// overlappingAuthors returns authors with both a broad and a site-specific scope.
func overlappingAuthors(scopes []PublicationScope) []string {
	broad := map[string]bool{}
	specific := map[string]bool{}
	for _, scope := range scopes {
		if len(scope.ScopeKeys) == 0 {
			broad[scope.AuthorDID] = true
		} else {
			specific[scope.AuthorDID] = true
		}
	}
	both := map[string]bool{}
	for author := range broad {
		if specific[author] {
			both[author] = true
		}
	}
	return sortedKeys(both)
}

// siteIdentityKeys normalizes a site spelling into its comparable identities.
func siteIdentityKeys(site string) []string {
	var keys []string
	if value, ok := CanonicalPublicationAtURIKey(site); ok {
		keys = append(keys, "at:"+value)
	}
	if value, ok := NormalizeFeedURL(site); ok {
		keys = append(keys, "feed:"+value)
	}
	if value, ok := NormalizePublicationSiteURL(site); ok {
		keys = append(keys, "site:"+value)
	}
	return keys
}

// unreadReadMutationScopeJSON resolves each scope's stored site spellings once.
func unreadReadMutationScopeJSON(scopes []PublicationScope, additionalSites []string) (string, error) {
	ordered := append([]PublicationScope{}, scopes...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].PublicationID < ordered[j].PublicationID })
	candidates := map[string]bool{}
	for _, scope := range scopes {
		for _, key := range scope.ScopeKeys {
			candidates[key] = true
		}
	}
	for _, site := range additionalSites {
		candidates[site] = true
	}
	byIdentity := map[string]map[string]bool{}
	for candidate := range candidates {
		for _, identity := range siteIdentityKeys(candidate) {
			if byIdentity[identity] == nil {
				byIdentity[identity] = map[string]bool{}
			}
			byIdentity[identity][candidate] = true
		}
	}
	values := make([]unreadReadMutationScope, 0, len(ordered))
	for position, scope := range ordered {
		related := map[string]bool{}
		for _, key := range scope.ScopeKeys {
			for _, identity := range siteIdentityKeys(key) {
				for candidate := range byIdentity[identity] {
					related[candidate] = true
				}
			}
		}
		matching := []string{}
		for _, candidate := range sortedKeys(related) {
			candidate := candidate
			if ContentMatchesScope(scope.AuthorDID, &candidate, scope) {
				matching = append(matching, candidate)
			}
		}
		values = append(values, unreadReadMutationScope{
			PublicationID: scope.PublicationID, AuthorDID: scope.AuthorDID, ScopeKeys: matching,
			Position: position, Unscoped: len(scope.ScopeKeys) == 0,
		})
	}
	data, err := json.Marshal(values)
	return string(data), err
}
