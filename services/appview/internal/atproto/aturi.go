package atproto

import (
	"regexp"
	"strings"
)

// ATURI is a parsed record URI (at://{repo}/{collection}/{rkey}).
type ATURI struct {
	Repo       string
	Collection string
	RKey       string
}

// String renders the canonical at:// form.
func (u ATURI) String() string { return BuildRecordURI(u.Repo, u.Collection, u.RKey) }

// ParseATURI parses leniently like ATProtoPrimitiveKit's ATURI: empty path
// segments are skipped and segments beyond the third are ignored.
func ParseATURI(raw string) (ATURI, bool) {
	rest, ok := strings.CutPrefix(raw, "at://")
	if !ok {
		return ATURI{}, false
	}
	parts := make([]string, 0, 3)
	for _, part := range strings.Split(rest, "/") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) < 3 {
		return ATURI{}, false
	}
	return ATURI{Repo: parts[0], Collection: parts[1], RKey: parts[2]}, true
}

var strictRecordURI = regexp.MustCompile(`^at://([^/]+)/([^/]+)/([^/]+)$`)

// ParseRecordURI parses exactly at://{did}/{collection}/{rkey} with no extra
// segments (RenderFieldExtractor.parseAtUri).
func ParseRecordURI(raw string) (ATURI, bool) {
	match := strictRecordURI.FindStringSubmatch(raw)
	if match == nil {
		return ATURI{}, false
	}
	return ATURI{Repo: match[1], Collection: match[2], RKey: match[3]}, true
}

// BuildRecordURI renders at://{repo}/{collection}/{rkey}.
func BuildRecordURI(repo, collection, rkey string) string {
	return "at://" + repo + "/" + collection + "/" + rkey
}

// NormalizeAtRepoParam trims whitespace from an AT repo/URI query parameter
// (AtUriNormalization.normalizeAtRepoParam).
func NormalizeAtRepoParam(raw string) string { return strings.TrimSpace(raw) }

// RKeyFromURI returns the last non-empty path segment of uri, or uri itself.
func RKeyFromURI(uri string) string {
	parts := strings.Split(uri, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" {
			return parts[i]
		}
	}
	return uri
}

// RepoAndPublicationFilter splits a publication ID into its repo DID and,
// when the ID is an AT-URI, the publication AT-URI filter ("" otherwise).
func RepoAndPublicationFilter(publicationID string) (repoDID, publicationATURI string) {
	if at, ok := ParseATURI(publicationID); ok {
		return at.Repo, publicationID
	}
	return publicationID, ""
}
