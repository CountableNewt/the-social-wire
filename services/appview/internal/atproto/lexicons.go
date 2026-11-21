package atproto

// Collection NSIDs shared by repo writes and projection reads (PublicationLexicons).
const (
	CollectionFolder                 = "app.thesocialwire.folder"
	CollectionPublicationPrefs       = "app.thesocialwire.publicationPrefs"
	CollectionGraphSubscription      = "site.standard.graph.subscription"
	CollectionSkyreaderSubscription  = "app.skyreader.feed.subscription"
	CollectionGraphFollow            = "app.bsky.graph.follow"
	CollectionPreferences            = "app.thesocialwire.preferences"
	LegacyCollectionFolder           = "com.thesocialwire.folder"
	LegacyCollectionPublicationPrefs = "com.thesocialwire.publicationPrefs"
	LegacyCollectionPreferences      = "com.thesocialwire.preferences"

	// RSSAuthorDID is the synthetic author of Skyreader RSS entries.
	RSSAuthorDID = "did:web:skyreader.rss"
	// RSSPublicationPrefix prefixes RSS publication IDs.
	RSSPublicationPrefix = "rss:"
)

// LexiconPair maps a legacy collection to its current replacement.
type LexiconPair struct {
	Legacy  string
	Current string
}

// LegacyCollections lists the com.thesocialwire.* → app.thesocialwire.* migrations.
func LegacyCollections() []LexiconPair {
	return []LexiconPair{
		{LegacyCollectionFolder, CollectionFolder},
		{LegacyCollectionPublicationPrefs, CollectionPublicationPrefs},
		{LegacyCollectionPreferences, CollectionPreferences},
	}
}

// IsPublicationRecordCollection reports membership in publicationRecordCollections.
func IsPublicationRecordCollection(collection string) bool {
	return collection == "site.standard.publication" || collection == "com.standard.publication"
}

// DiscoveryPublicationCollections are probed for publication records, in order.
func DiscoveryPublicationCollections() []string {
	return []string{"site.standard.publication", "com.standard.publication", "app.offprint.publication"}
}

// DiscoveryContentCollections are probed for content records, in order.
func DiscoveryContentCollections() []string {
	return []string{"site.standard.document", "com.standard.document", "site.standard.entry", "com.standard.entry"}
}
