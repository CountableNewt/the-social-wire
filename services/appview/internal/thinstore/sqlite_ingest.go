package thinstore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

// SQLiteIngestStore writes content and fetch metadata into the reader's database.
type SQLiteIngestStore struct {
	DB *sqlitedb.DB
}

const contentUpsertConflict = `
		DO UPDATE SET
			cid=excluded.cid,author_did=excluded.author_did,collection=excluded.collection,
			created_at=excluded.created_at,indexed_at=excluded.indexed_at,created_at_us=excluded.created_at_us,indexed_at_us=excluded.indexed_at_us,publication_site=excluded.publication_site,
			render_json=excluded.render_json,expires_at=excluded.expires_at
		WHERE (content_items.cid,content_items.author_did,content_items.collection,content_items.created_at,
			content_items.publication_site,content_items.render_json,content_items.expires_at)
		IS NOT (excluded.cid,excluded.author_did,excluded.collection,excluded.created_at,
			excluded.publication_site,excluded.render_json,excluded.expires_at)
	`

func (s SQLiteIngestStore) UpsertContentItem(ctx context.Context, item IndexedContentItem) error {
	_, err := s.writeContentItem(ctx, item, contentUpsertConflict)
	return err
}

func (s SQLiteIngestStore) InsertContentItemIfAbsent(ctx context.Context, item IndexedContentItem) (bool, error) {
	return s.writeContentItem(ctx, item, "DO NOTHING")
}

func (s SQLiteIngestStore) writeContentItem(ctx context.Context, item IndexedContentItem, conflict string) (bool, error) {
	args, err := contentItemValues(item)
	if err != nil {
		return false, err
	}
	result, err := s.DB.Writer().ExecContext(ctx, `
		INSERT INTO content_items
			(uri,cid,author_did,collection,created_at,indexed_at,created_at_us,indexed_at_us,publication_site,render_json,expires_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(uri) `+conflict,
		args...)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count > 0, err
}

func contentItemValues(item IndexedContentItem) ([]any, error) {
	createdAt, indexedAt := pdsTimestamp(item.CreatedAt), isoString(item.IndexedAt)
	createdMicros, err := sqlitedb.TimestampMicroseconds(createdAt)
	if err != nil {
		return nil, err
	}
	indexedMicros, err := sqlitedb.TimestampMicroseconds(indexedAt)
	if err != nil {
		return nil, err
	}
	return []any{item.URI, item.CID, item.AuthorDID, item.Collection, createdAt,
		indexedAt, createdMicros, indexedMicros, item.PublicationSite, item.Render.EncodedJSON(), isoString(item.ExpiresAt)}, nil
}

func (s SQLiteIngestStore) HasContentItem(ctx context.Context, uri string) (bool, error) {
	var exists bool
	err := s.DB.Reader().QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM content_items WHERE uri=?)`, uri).Scan(&exists)
	return exists, err
}

func (s SQLiteIngestStore) FetchRssFeedMetadata(ctx context.Context, feed string) (*RssFeedFetchMetadata, error) {
	result := RssFeedFetchMetadata{NormalizedFeedURL: feed}
	var lastPoll, backoff sql.NullString
	err := s.DB.Reader().QueryRowContext(ctx, `
		SELECT etag,last_modified,last_poll_at,backoff_until,consecutive_error_count
		FROM rss_feed_fetch_metadata WHERE feed_url=?
	`, feed).Scan(&result.ETag, &result.LastModified, &lastPoll, &backoff, &result.ConsecutiveErrorCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if value, ok := dateFromISO(lastPoll.String); ok {
		result.LastPollAt = &value
	}
	if value, ok := dateFromISO(backoff.String); ok {
		result.BackoffUntil = &value
	}
	return &result, nil
}

func (s SQLiteIngestStore) StoreRssFeedMetadata(ctx context.Context, metadata RssFeedFetchMetadata) error {
	return storeRSSMetadata(ctx, s.DB.Writer(), metadata)
}

type ingestExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func storeRSSMetadata(ctx context.Context, executor ingestExecutor, metadata RssFeedFetchMetadata) error {
	_, err := executor.ExecContext(ctx, `
		INSERT INTO rss_feed_fetch_metadata
			(feed_url,etag,last_modified,last_poll_at,backoff_until,consecutive_error_count)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(feed_url) DO UPDATE SET etag=excluded.etag,last_modified=excluded.last_modified,
			last_poll_at=excluded.last_poll_at,backoff_until=excluded.backoff_until,
			consecutive_error_count=excluded.consecutive_error_count
	`, metadata.NormalizedFeedURL, metadata.ETag, metadata.LastModified,
		optionalTimestamp(metadata.LastPollAt, microsTimestamp), optionalTimestamp(metadata.BackoffUntil, isoString),
		metadata.ConsecutiveErrorCount)
	return err
}

func optionalTimestamp(value *time.Time, format func(time.Time) string) *string {
	if value == nil {
		return nil
	}
	rendered := format(*value)
	return &rendered
}

// CompareAndSwapContent updates only the exact version observed before a PDS check.
func (s SQLiteIngestStore) CompareAndSwapContent(ctx context.Context, item IndexedContentItem, previousCID string) (bool, error) {
	createdAt, indexedAt := pdsTimestamp(item.CreatedAt), isoString(item.IndexedAt)
	createdMicros, err := sqlitedb.TimestampMicroseconds(createdAt)
	if err != nil {
		return false, err
	}
	indexedMicros, err := sqlitedb.TimestampMicroseconds(indexedAt)
	if err != nil {
		return false, err
	}
	result, err := s.DB.Writer().ExecContext(ctx, `
		UPDATE content_items SET cid=?,author_did=?,collection=?,created_at=?,indexed_at=?,created_at_us=?,indexed_at_us=?,
			publication_site=?,render_json=?,expires_at=? WHERE uri=? AND cid=?
	`, item.CID, item.AuthorDID, item.Collection, createdAt, indexedAt, createdMicros, indexedMicros,
		item.PublicationSite, item.Render.EncodedJSON(), isoString(item.ExpiresAt), item.URI, previousCID)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}
