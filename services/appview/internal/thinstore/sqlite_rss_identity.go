package thinstore

import "context"

// ExistingRSSIdentities keeps a publication's persisted identifiers stable
// even when Swift and Go encode the same RSS key differently.
func (s SQLiteIngestStore) ExistingRSSIdentities(ctx context.Context, feed string) (map[string]IndexedContentIdentity, error) {
	rows, err := s.DB.Reader().QueryContext(ctx, `SELECT uri,cid FROM content_items
		WHERE author_did=? AND publication_site=? ORDER BY indexed_at_us DESC,uri LIMIT 5000`, RssAuthorDID, feed)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]IndexedContentIdentity)
	for rows.Next() {
		var id IndexedContentIdentity
		if err := rows.Scan(&id.URI, &id.CID); err != nil {
			return nil, err
		}
		if priorFeed, stable, ok := decodeRssEntryID(id.URI); ok && priorFeed == feed {
			if _, exists := result[stable]; !exists {
				result[stable] = id
			}
		}
	}
	return result, rows.Err()
}
