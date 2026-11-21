package reader

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/isotime"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

// Migrate initializes read authority and read-side cache metadata in the
// existing Swift database format.
func Migrate(ctx context.Context, db *sqlitedb.DB) error {
	return db.Write(ctx, func(tx *sql.Tx) error {
		if err := sqlitedb.ExecAll(tx, `
			CREATE TABLE IF NOT EXISTS appview_pds_read_state_authority(
				viewer_did TEXT PRIMARY KEY, legacy_revision INTEGER NOT NULL DEFAULT 0,
				manifest TEXT, manifest_cid TEXT, projection_ready INTEGER NOT NULL DEFAULT 1,
				last_accessed_at REAL NOT NULL DEFAULT 0);
			CREATE TABLE IF NOT EXISTS appview_pds_projection_write(
				singleton INTEGER PRIMARY KEY CHECK(singleton=1), active INTEGER NOT NULL);
			INSERT OR IGNORE INTO appview_pds_projection_write VALUES(1,0);
			CREATE TABLE IF NOT EXISTS appview_pds_read_state_exact(
				viewer_did TEXT NOT NULL, subject_uri TEXT NOT NULL, sequence INTEGER NOT NULL,
				is_read INTEGER NOT NULL, acted_at TEXT NOT NULL, PRIMARY KEY(viewer_did,subject_uri));
			CREATE TABLE IF NOT EXISTS appview_pds_read_state_boundaries(
				viewer_did TEXT NOT NULL, sequence INTEGER NOT NULL, rule_key TEXT NOT NULL,
				is_read INTEGER NOT NULL, acted_at TEXT NOT NULL, author_did TEXT NOT NULL,
				scope_keys TEXT NOT NULL, boundary_at TEXT NOT NULL, boundary_uri TEXT,
				PRIMARY KEY(viewer_did,sequence,rule_key));
			CREATE INDEX IF NOT EXISTS appview_pds_boundary_lookup
				ON appview_pds_read_state_boundaries(viewer_did,author_did,boundary_at);
			CREATE TABLE IF NOT EXISTS unread_counts_cache (
				viewer_did TEXT NOT NULL, publication_id TEXT NOT NULL, unread_count INTEGER NOT NULL,
				cached_at TEXT NOT NULL, expires_at TEXT NOT NULL, PRIMARY KEY(viewer_did,publication_id));
			CREATE TABLE IF NOT EXISTS first_page_cache (
				viewer_did TEXT NOT NULL, publication_id TEXT NOT NULL, json_body TEXT NOT NULL,
				cached_at TEXT NOT NULL, expires_at TEXT NOT NULL, PRIMARY KEY(viewer_did,publication_id));
		`); err != nil {
			return err
		}
		for _, table := range []string{"read_marks", "appview_unread_overrides", "appview_publication_read_floors"} {
			for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
				row := "NEW"
				if op == "DELETE" {
					row = "OLD"
				}
				if _, err := tx.ExecContext(ctx, `CREATE TRIGGER IF NOT EXISTS pds_fence_`+table+"_"+op+" BEFORE "+op+" ON "+table+`
					WHEN (SELECT active FROM appview_pds_projection_write WHERE singleton=1)=0
						AND EXISTS(SELECT 1 FROM appview_pds_read_state_authority WHERE viewer_did=`+row+`.viewer_did AND manifest_cid IS NOT NULL)
					BEGIN SELECT RAISE(ABORT,'PDSReadStateAlreadyMigrated'); END;`); err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `CREATE TRIGGER IF NOT EXISTS pds_revision_`+table+"_"+op+" AFTER "+op+" ON "+table+`
					WHEN (SELECT active FROM appview_pds_projection_write WHERE singleton=1)=0
					BEGIN
						INSERT INTO appview_pds_read_state_authority(viewer_did,legacy_revision) VALUES(`+row+`.viewer_did,1)
						ON CONFLICT(viewer_did) DO UPDATE SET legacy_revision=legacy_revision+1;
					END;`); err != nil {
					return err
				}
			}
		}
		return migrateReadStateProjectionTriggers(ctx, tx)
	})
}

func migrateReadStateProjectionTriggers(ctx context.Context, tx *sql.Tx) error {
	for _, operation := range []string{"INSERT", "UPDATE", "DELETE"} {
		row := "NEW"
		if operation == "DELETE" {
			row = "OLD"
		}
		if _, err := tx.ExecContext(ctx, `CREATE TRIGGER IF NOT EXISTS pds_scope_revision_`+operation+" AFTER "+operation+` ON appview_publication_scopes
			WHEN (SELECT active FROM appview_pds_projection_write WHERE singleton=1)=0
				AND NOT EXISTS(SELECT 1 FROM appview_pds_read_state_authority WHERE viewer_did=`+row+`.viewer_did AND manifest_cid IS NOT NULL)
			BEGIN
				INSERT INTO appview_pds_read_state_authority(viewer_did,legacy_revision) VALUES(`+row+`.viewer_did,1)
				ON CONFLICT(viewer_did) DO UPDATE SET legacy_revision=legacy_revision+1;
			END;`); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `CREATE TRIGGER IF NOT EXISTS pds_content_DELETE AFTER DELETE ON content_items
		WHEN (SELECT active FROM appview_pds_projection_write WHERE singleton=1)=0
		BEGIN
			UPDATE appview_pds_projection_write SET active=1 WHERE singleton=1;
			DELETE FROM read_marks WHERE subject_uri=OLD.uri AND viewer_did IN
				(SELECT viewer_did FROM appview_pds_read_state_authority WHERE manifest_cid IS NOT NULL);
			INSERT INTO read_marks(viewer_did,subject_uri,created_at)
				SELECT e.viewer_did,e.subject_uri,e.acted_at FROM appview_pds_read_state_exact e
				JOIN appview_pds_read_state_authority a ON a.viewer_did=e.viewer_did
				WHERE e.subject_uri=OLD.uri AND e.is_read=1 AND a.projection_ready=1;
			UPDATE appview_pds_projection_write SET active=0 WHERE singleton=1;
		END;`); err != nil {
		return err
	}
	for _, operation := range []string{"INSERT", "UPDATE"} {
		if _, err := tx.ExecContext(ctx, `CREATE TRIGGER IF NOT EXISTS pds_content_`+operation+" AFTER "+operation+` ON content_items
			WHEN (SELECT active FROM appview_pds_projection_write WHERE singleton=1)=0
			BEGIN
				UPDATE appview_pds_projection_write SET active=1 WHERE singleton=1;
				DELETE FROM read_marks WHERE subject_uri=NEW.uri AND viewer_did IN
					(SELECT viewer_did FROM appview_pds_read_state_authority WHERE manifest_cid IS NOT NULL);
				INSERT INTO read_marks(viewer_did,subject_uri,created_at)
					SELECT a.viewer_did,NEW.uri,COALESCE((SELECT acted_at FROM appview_pds_read_state_exact e
						WHERE e.viewer_did=a.viewer_did AND e.subject_uri=NEW.uri),NEW.created_at)
					FROM appview_pds_read_state_authority a
					WHERE a.manifest_cid IS NOT NULL AND a.projection_ready=1 AND COALESCE((SELECT state FROM (
						SELECT e.is_read AS state,e.sequence FROM appview_pds_read_state_exact e
							WHERE e.viewer_did=a.viewer_did AND e.subject_uri=NEW.uri
						UNION ALL
						SELECT b.is_read,b.sequence FROM appview_pds_read_state_boundaries b
							WHERE b.viewer_did=a.viewer_did AND b.author_did=NEW.author_did
							AND (json_array_length(b.scope_keys)=0 OR EXISTS(SELECT 1 FROM json_each(b.scope_keys) WHERE value=NEW.publication_site))
							AND (tsw_timestamp_us(NEW.created_at)<tsw_timestamp_us(b.boundary_at)
								OR (tsw_timestamp_us(NEW.created_at)=tsw_timestamp_us(b.boundary_at) AND (b.boundary_uri IS NULL OR NEW.uri<=b.boundary_uri)))
						) ORDER BY sequence DESC LIMIT 1),0)=1;
				UPDATE appview_unread_counters SET dirty=1 WHERE viewer_did IN
					(SELECT viewer_did FROM appview_pds_read_state_authority WHERE manifest_cid IS NOT NULL);
				UPDATE appview_pds_projection_write SET active=0 WHERE singleton=1;
			END;`); err != nil {
			return err
		}
	}
	return nil

}

func requireLegacyWrite(ctx context.Context, db rowsQueryer, viewer string) error {
	migrated, err := readAuthority(ctx, db, viewer)
	if migrated {
		return httpx.Errorf(http.StatusConflict, "PDSReadStateRequired: Update your client to change PDS read state")
	}
	return err
}

func changeReadMark(ctx context.Context, db *sqlitedb.DB, viewer, subject string, actedAt time.Time, read bool) error {
	return db.Write(ctx, func(tx *sql.Tx) error {
		if err := requireLegacyWrite(ctx, tx, viewer); err != nil {
			return err
		}
		add, remove := "read_marks", "appview_unread_overrides"
		if !read {
			add, remove = remove, add
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+remove+" WHERE viewer_did = ? AND subject_uri = ?", viewer, subject); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO "+add+" (viewer_did,subject_uri,created_at) VALUES(?,?,?) "+
			"ON CONFLICT(viewer_did,subject_uri) DO NOTHING", viewer, subject, thinstore.CursorTimestamp(actedAt)); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE appview_unread_counters SET dirty=1
			WHERE viewer_did = ? AND publication_id IN (
				SELECT ps.publication_id FROM appview_publication_scopes ps
				JOIN content_items ci ON ci.author_did = ps.author_did
				WHERE ps.viewer_did = ? AND ci.uri = ?
			)`, viewer, viewer, subject)
		if err != nil {
			return err
		}
		return invalidateCaches(ctx, tx, viewer)
	})
}

func invalidateCaches(ctx context.Context, tx *sql.Tx, viewer string) error {
	for _, table := range []string{"first_page_cache", "unread_counts_cache"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE viewer_did = ?", viewer); err != nil {
			return err
		}
	}
	return nil
}

type markAllResponse struct {
	Marked       int                               `json:"marked"`
	ConfirmedAt  isotime.Time                      `json:"confirmedAt"`
	Boundaries   []thinstore.ReadWatermarkBoundary `json:"boundaries"`
	UnreadCounts map[string]int                    `json:"unreadCounts"`
}

func markAll(ctx context.Context, db *sqlitedb.DB, viewer string, scopes []thinstore.PublicationScope) (markAllResponse, error) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	result := markAllResponse{
		ConfirmedAt: isotime.Time{Time: now}, Boundaries: []thinstore.ReadWatermarkBoundary{},
		UnreadCounts: map[string]int{},
	}
	err := db.Write(ctx, func(tx *sql.Tx) error {
		if err := requireLegacyWrite(ctx, tx, viewer); err != nil {
			return err
		}
		before, err := counts(ctx, tx, viewer, scopes)
		if err != nil {
			return err
		}
		for _, count := range before {
			result.Marked += count
		}
		rawMatches, err := matches(ctx, tx, scopes)
		if err != nil {
			return err
		}
		var matched []feedMatch
		if err := json.Unmarshal(rawMatches, &matched); err != nil {
			return err
		}
		byID := make(map[string]feedMatch, len(matched))
		for _, match := range matched {
			byID[match.PublicationID] = match
		}
		generation := now.UnixMilli()
		for _, scope := range scopes {
			boundary, err := setReadFloor(ctx, tx, viewer, scope, byID[scope.PublicationID], now, generation)
			if err != nil {
				return err
			}
			result.Boundaries = append(result.Boundaries, boundary)
		}
		result.UnreadCounts, err = counts(ctx, tx, viewer, scopes)
		if err != nil {
			return err
		}
		for id, count := range result.UnreadCounts {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO appview_unread_counters(viewer_did,publication_id,unread_count,generation,accuracy,dirty,counted_at)
				VALUES(?,?,?,?,'exact',0,?)
				ON CONFLICT(viewer_did,publication_id) DO UPDATE SET unread_count=excluded.unread_count,
					generation=excluded.generation,accuracy=excluded.accuracy,dirty=0,counted_at=excluded.counted_at`,
				viewer, id, count, generation, thinstore.CursorTimestamp(now)); err != nil {
				return err
			}
		}
		return invalidateCaches(ctx, tx, viewer)
	})
	return result, err
}

func setReadFloor(ctx context.Context, tx *sql.Tx, viewer string, scope thinstore.PublicationScope,
	match feedMatch, now time.Time, generation int64) (thinstore.ReadWatermarkBoundary, error) {
	boundary := thinstore.ReadWatermarkBoundary{PublicationID: scope.PublicationID, CreatedAt: now}
	var rawAt, uri string
	sites, _ := json.Marshal(match.Sites)
	scopePredicate := `ci.author_did = ? AND (? = 'null' OR ci.publication_site IN (SELECT value FROM json_each(?)))`
	scopeArgs := []any{match.AuthorDID, string(sites), string(sites)}
	err := tx.QueryRowContext(ctx, `SELECT ci.created_at, ci.uri FROM content_items ci WHERE `+scopePredicate+
		` AND ci.expires_at > ? ORDER BY `+thinstore.FeedTimestampSQL("ci.created_at")+` DESC, ci.uri DESC LIMIT 1`,
		append(scopeArgs, thinstore.CursorTimestamp(now))...).Scan(&rawAt, &uri)
	if err == nil {
		boundary.CreatedAt, err = time.Parse(time.RFC3339Nano, rawAt)
		boundary.EntryID = &uri
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return boundary, err
	}
	var existingAt string
	var existingURI *string
	err = tx.QueryRowContext(ctx, `SELECT read_floor_at, read_floor_uri FROM appview_publication_read_floors
		WHERE viewer_did=? AND publication_id=?`, viewer, scope.PublicationID).Scan(&existingAt, &existingURI)
	if err == nil {
		existingTime, err := time.Parse(time.RFC3339Nano, existingAt)
		if err != nil {
			return boundary, err
		}
		existing := thinstore.ReadWatermarkBoundary{PublicationID: scope.PublicationID, CreatedAt: existingTime, EntryID: existingURI}
		if existing.IsAfter(boundary) {
			boundary = existing
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return boundary, err
	}
	floor := thinstore.CursorTimestamp(boundary.CreatedAt)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO appview_publication_read_floors(viewer_did,publication_id,read_floor_at,read_floor_uri,generation,updated_at)
		VALUES(?,?,?,?,?,?) ON CONFLICT(viewer_did,publication_id) DO UPDATE SET read_floor_at=excluded.read_floor_at,
			read_floor_uri=excluded.read_floor_uri,generation=excluded.generation,updated_at=excluded.updated_at`,
		viewer, scope.PublicationID, floor, boundary.EntryID, generation, thinstore.CursorTimestamp(now))
	if err != nil {
		return boundary, err
	}
	args := append([]any{viewer, thinstore.CursorTimestamp(now)}, scopeArgs...)
	args = append(args, floor, floor, boundary.EntryID, boundary.EntryID)
	_, err = tx.ExecContext(ctx, `DELETE FROM appview_unread_overrides WHERE viewer_did = ?
		AND tsw_timestamp_us(created_at) <= tsw_timestamp_us(?) AND subject_uri IN
		(SELECT ci.uri FROM content_items ci WHERE `+scopePredicate+` AND (tsw_timestamp_us(ci.created_at) < tsw_timestamp_us(?)
			OR (tsw_timestamp_us(ci.created_at) = tsw_timestamp_us(?) AND (? IS NULL OR ci.uri <= ?))))`, args...)
	return boundary, err
}
