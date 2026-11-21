package main

import (
	"context"
	"database/sql"
	"log/slog"
	"strconv"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/ingest"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/isotime"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

func cleanupExpired(ctx context.Context, db *sqlitedb.DB, environment string, env config.Env, logger *slog.Logger) {
	readRetention := configuredSeconds(env, "THIN_APPVIEW_READ_MARK_TTL_SECONDS", 180*24*time.Hour)
	for ctx.Err() == nil {
		sweep, cancel := context.WithTimeout(ctx, 30*time.Second)
		backlog, err := cleanupBatch(sweep, db, environment, time.Now(), readRetention)
		for err == nil && backlog && sweep.Err() == nil {
			backlog, err = cleanupBatch(sweep, db, environment, time.Now(), readRetention)
		}
		cancel()
		if err != nil && ctx.Err() == nil {
			logger.Warn("AppView TTL cleanup failed", "error", err)
		}
		delay := time.Hour
		if backlog || err != nil {
			delay = 10 * time.Second
		}
		if !waitFor(ctx, delay) {
			return
		}
	}
}

func cleanupBatch(ctx context.Context, db *sqlitedb.DB, environment string, now time.Time, readRetention time.Duration) (bool, error) {
	const limit = 1000
	before := isotime.Format(now)
	statements := []struct {
		query string
		args  []any
	}{
		{`DELETE FROM content_items WHERE rowid IN (
			SELECT rowid FROM content_items WHERE expires_at<=? ORDER BY expires_at,uri LIMIT ?
		)`, []any{before, limit}},
		{`DELETE FROM read_marks WHERE rowid IN (
			SELECT rowid FROM read_marks WHERE created_at<=? AND NOT EXISTS (
				SELECT 1 FROM appview_pds_read_state_authority authority
				WHERE authority.viewer_did=read_marks.viewer_did AND authority.manifest_cid IS NOT NULL
			) ORDER BY created_at,viewer_did,subject_uri LIMIT ?
		)`, []any{isotime.Format(now.Add(-readRetention)), limit}},
		{`DELETE FROM appview_ingestion_inbox WHERE rowid IN (
			SELECT rowid FROM appview_ingestion_inbox WHERE environment=? AND expires_at<=?
			AND (status IN ('applied','filtered_scope') OR (status='dead_letter' AND reconciled_at IS NOT NULL))
			ORDER BY expires_at,seq LIMIT ?
		)`, []any{environment, before, limit}},
	}
	for _, table := range []string{"sidebar_projection_cache", "unread_counts_cache", "first_page_cache", "pds_repo_record_cache"} {
		statements = append(statements, struct {
			query string
			args  []any
		}{
			"DELETE FROM " + table + " WHERE rowid IN (SELECT rowid FROM " + table + " WHERE expires_at<=? ORDER BY expires_at LIMIT ?)",
			[]any{before, limit},
		})
	}
	var backlog bool
	err := db.Write(ctx, func(tx *sql.Tx) error {
		for _, statement := range statements {
			result, err := tx.ExecContext(ctx, statement.query, statement.args...)
			if err != nil {
				return err
			}
			count, err := result.RowsAffected()
			if err != nil {
				return err
			}
			backlog = backlog || count >= limit
		}
		return nil
	})
	return backlog, err
}

func configuredSeconds(env config.Env, key string, fallback time.Duration) time.Duration {
	seconds, err := strconv.ParseFloat(env.String(key), 64)
	if err != nil || !(seconds > 0 && seconds < float64(1<<63-1)/float64(time.Second)) {
		return fallback
	}
	return time.Duration(seconds * float64(time.Second))
}

func waitFor(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func pollRSS(ctx context.Context, db *sqlitedb.DB, service *ingest.Service, env config.Env, logger *slog.Logger) {
	interval := configuredSeconds(env, "THIN_APPVIEW_RSS_FEED_POLL_INTERVAL_SECONDS", 30*time.Minute)
	limit, err := strconv.Atoi(env.StringOr("THIN_APPVIEW_RSS_FEED_POLL_FEED_LIMIT", "20"))
	if err != nil {
		limit = 20
	}
	limit = max(1, min(limit, 200))
	for ctx.Err() == nil {
		feeds, err := indexedFeeds(ctx, db, limit)
		if err != nil && ctx.Err() == nil {
			logger.Warn("RSS feed poll could not load indexed feeds", "error", err)
		}
		for _, feed := range feeds {
			if ctx.Err() != nil {
				return
			}
			if _, err := service.RefreshFeed(ctx, feed); err != nil && ctx.Err() == nil {
				logger.Debug("RSS feed poll did not finish", "error", err)
			}
		}
		if !waitFor(ctx, interval) {
			return
		}
	}
}

func indexedFeeds(ctx context.Context, db *sqlitedb.DB, limit int) ([]string, error) {
	rows, err := db.Reader().QueryContext(ctx, `
		SELECT content.publication_site
		FROM content_items content LEFT JOIN rss_feed_fetch_metadata meta ON meta.feed_url=content.publication_site
		WHERE content.author_did=? AND content.publication_site IS NOT NULL AND content.expires_at>?
			AND (meta.backoff_until IS NULL OR meta.backoff_until<=?)
		GROUP BY content.publication_site ORDER BY COALESCE(MAX(meta.last_poll_at),MIN(content.indexed_at)),content.publication_site
		LIMIT ?
	`, atproto.RSSAuthorDID, isotime.Format(time.Now()), isotime.Format(time.Now()), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	feeds := make([]string, 0, limit)
	for rows.Next() {
		var feed string
		if err := rows.Scan(&feed); err != nil {
			return nil, err
		}
		feeds = append(feeds, feed)
	}
	return feeds, rows.Err()
}
