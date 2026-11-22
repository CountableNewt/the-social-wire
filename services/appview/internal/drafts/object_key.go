package drafts

import "regexp"

// Retain the exact identities of archived clip objects during storage migration.
// This does not restore their retired publishing feature or accept general paths.
var legacyClipObjectKey = regexp.MustCompile(`^clips/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/(?:audio\.m4a|audiogram\.mp4)$`)

func validObjectKey(key string) bool {
	return assetDigest.MatchString(key) || legacyClipObjectKey.MatchString(key)
}
