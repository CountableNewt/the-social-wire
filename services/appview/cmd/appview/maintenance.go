package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/analytics"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/drafts"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/ingest"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/isotime"
	jobrunner "github.com/CountableNewt/the-social-wire/services/appview/internal/jobs"
	scheduler "github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling/backend"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
	"golang.org/x/sync/errgroup"
)

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

type maintenanceOptions struct {
	recovery    func(context.Context) error
	ingestion   *ingest.Service
	analytics   *analytics.Store
	environment string
	env         config.Env
}

// Every interval and Cron invocation uses the same bounded leased action.
func newMaintenanceRunner(db *sqlitedb.DB, secret string, schedulingService *scheduler.Service, draftService *drafts.Service, discovery *ingest.Microcosm, options ...maintenanceOptions) (*jobrunner.Runner, error) {
	actions := map[string]jobrunner.Action{}
	if schedulingService != nil {
		actions["scheduling"] = jobrunner.Action{Run: schedulingService.RunWorkerOnce, Timeout: 150 * time.Second}
	}
	if draftService != nil {
		actions["drafts-cleanup"] = jobrunner.Action{Run: draftService.CleanupOnce, Timeout: 45 * time.Second}
	}
	if discovery != nil {
		actions["discovery"] = jobrunner.Action{Run: func(ctx context.Context) error {
			// Keep the previous four-worker concurrency, while each claim and its
			// acknowledgement remain owned by the durable discovery queue.
			group, work := errgroup.WithContext(ctx)
			for range 4 {
				group.Go(func() error { _, err := discovery.RunOnce(work); return err })
			}
			return group.Wait()
		}, Timeout: 30 * time.Second}
	}
	if len(options) > 0 {
		option := options[0]
		actions["ttl-cleanup"] = jobrunner.Action{Run: func(ctx context.Context) error {
			retention := configuredSeconds(option.env, "THIN_APPVIEW_READ_MARK_TTL_SECONDS", 180*24*time.Hour)
			for range 10 {
				backlog, err := cleanupBatch(ctx, db, option.environment, time.Now(), retention)
				if err != nil {
					return err
				}
				if !backlog {
					return nil
				}
			}
			return nil
		}, Timeout: 45 * time.Second}
		if option.recovery != nil {
			actions["read-state-recovery"] = jobrunner.Action{Run: func(ctx context.Context) error {
				group, work := errgroup.WithContext(ctx)
				for range 2 {
					group.Go(func() error { return option.recovery(work) })
				}
				return group.Wait()
			}, Timeout: 60 * time.Second}
		}
		if option.analytics != nil {
			actions["analytics-cleanup"] = jobrunner.Action{Run: func(ctx context.Context) error { return option.analytics.Cleanup(ctx, time.Now()) }, Timeout: 45 * time.Second}
		}
		if option.ingestion != nil {
			actions["rss-poll"] = jobrunner.Action{Run: func(ctx context.Context) error { return pollRSSOnce(ctx, db, option.ingestion, option.env) }, Timeout: 90 * time.Second}
			actions["author-history"] = jobrunner.Action{Run: func(ctx context.Context) error {
				if option.ingestion.AuthorPDS == nil {
					return nil
				}
				group, work := errgroup.WithContext(ctx)
				for range 2 {
					group.Go(func() error { _, err := option.ingestion.RunAuthorHistoryOnce(work); return err })
				}
				return group.Wait()
			}, Timeout: 90 * time.Second}
		}
	}
	return jobrunner.New(db, secret, actions)
}

func runMaintenance(ctx context.Context, runner *jobrunner.Runner, name string, interval time.Duration, immediately bool, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	if !immediately {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
	for ctx.Err() == nil {
		if _, err := runner.Run(ctx, name); err != nil && ctx.Err() == nil {
			logger.Warn("Maintenance job deferred", "job", name)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func pollRSSOnce(ctx context.Context, db *sqlitedb.DB, service *ingest.Service, env config.Env) error {
	if _, supplied := env.Optional("THIN_APPVIEW_RSS_FEED_POLL_ENABLED"); supplied && !env.Truthy("THIN_APPVIEW_RSS_FEED_POLL_ENABLED") {
		return nil
	}
	limit, err := strconv.Atoi(env.StringOr("THIN_APPVIEW_RSS_FEED_POLL_FEED_LIMIT", "20"))
	if err != nil {
		limit = 20
	}
	feeds, err := indexedFeeds(ctx, db, max(1, min(limit, 200)))
	if err != nil {
		return err
	}
	var failure error
	for _, feed := range feeds {
		if err := ctx.Err(); err != nil {
			return errors.Join(failure, err)
		}
		_, err := service.RefreshFeed(ctx, feed)
		failure = errors.Join(failure, err)
	}
	return failure
}
