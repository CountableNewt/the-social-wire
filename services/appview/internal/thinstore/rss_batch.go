package thinstore

import (
	"context"
	"database/sql"
	"strings"
)

// StoreRSSFeed commits validators only with the complete selected feed page.
// Small multi-row statements bound bindings while avoiding one remote round trip
// per article. Existing content triggers and conflict rules still apply per row.
func (s SQLiteIngestStore) StoreRSSFeed(ctx context.Context, items []IndexedContentItem, metadata RssFeedFetchMetadata) error {
	return s.DB.Write(ctx, func(tx *sql.Tx) error {
		return storeRSSFeed(ctx, tx, items, metadata)
	})
}

func storeRSSFeed(ctx context.Context, tx *sql.Tx, items []IndexedContentItem, metadata RssFeedFetchMetadata) error {
	const chunkSize = 64 // 704 bindings, below SQLite's historical 999 limit.
	for start := 0; start < len(items); start += chunkSize {
		chunk := items[start:min(start+chunkSize, len(items))]
		args := make([]any, 0, len(chunk)*11)
		for _, item := range chunk {
			values, err := contentItemValues(item)
			if err != nil {
				return err
			}
			args = append(args, values...)
		}
		query := `INSERT INTO content_items
    (uri,cid,author_did,collection,created_at,indexed_at,created_at_us,indexed_at_us,publication_site,render_json,expires_at)
    VALUES ` + strings.TrimSuffix(strings.Repeat("(?,?,?,?,?,?,?,?,?,?,?),", len(chunk)), ",") + " ON CONFLICT(uri) " + contentUpsertConflict
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}
	return storeRSSMetadata(ctx, tx, metadata)
}
