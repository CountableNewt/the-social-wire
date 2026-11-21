package atproto

import "strings"

// OAuth scope helpers (ATProtoAuthKit OAuthScopes).
const (
	ScopeATProto       = "atproto"
	ScopeRepoPrefix    = "repo:"
	defaultRepoActions = "create,update,delete"
)

// RepoCollectionScope builds repo:{nsid}?{actions}; nil actions means create,update,delete.
func RepoCollectionScope(nsid string, actions []string) string {
	joined := defaultRepoActions
	if actions != nil {
		joined = strings.Join(actions, ",")
	}
	return ScopeRepoPrefix + nsid + "?" + joined
}

// ParseRepoCollectionScope returns the collection of a repo: scope.
func ParseRepoCollectionScope(scope string) (string, bool) {
	rest, ok := strings.CutPrefix(scope, ScopeRepoPrefix)
	if !ok {
		return "", false
	}
	collection, _, _ := strings.Cut(rest, "?")
	return collection, true
}
