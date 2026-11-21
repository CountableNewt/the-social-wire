package publications

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

// Migrate creates the cache schema shared with the legacy backend.
func Migrate(ctx context.Context, db *sqlitedb.DB) error {
	return db.Write(ctx, func(tx *sql.Tx) error {
		return sqlitedb.ExecAll(tx, `
			CREATE TABLE IF NOT EXISTS sidebar_projection_cache (
				viewer_did TEXT PRIMARY KEY,
				json_body TEXT NOT NULL,
				cached_at TEXT NOT NULL,
				expires_at TEXT NOT NULL
			);
			CREATE INDEX IF NOT EXISTS idx_sidebar_projection_cache_expires ON sidebar_projection_cache (expires_at);
		`)
	})
}

func (s *Service) persist(ctx context.Context, sidebar *Sidebar) error {
	now := time.Now().UTC()
	updatedAt := thinstore.CursorTimestamp(now)
	viewer := sidebar.ViewerDID
	memberships := map[thinstore.FeedSelector][]Row{
		thinstore.NewFeedSelector(thinstore.FeedSubscribed, ""): sidebar.UnfolderedRows,
		thinstore.NewFeedSelector(thinstore.FeedFollowing, ""):  sidebar.FollowingRows,
	}
	for _, section := range sidebar.FolderSections {
		memberships[thinstore.NewFeedSelector(thinstore.FeedFolder, section.FolderRKey)] = section.Publications
		selector := thinstore.NewFeedSelector(thinstore.FeedSubscribed, "")
		memberships[selector] = append(memberships[selector], section.Publications...)
	}
	for _, row := range sidebar.AllRows {
		memberships[thinstore.NewFeedSelector(thinstore.FeedKindPublication, row.PublicationID)] = []Row{row}
	}
	type rowSections struct {
		row      Row
		sections []string
	}
	byPublication := map[string]rowSections{}
	for selector, rows := range memberships {
		key := string(selector.Kind)
		if selector.Kind == thinstore.FeedFolder {
			key += ":" + selector.ID
		}
		for _, row := range rows {
			value := byPublication[row.PublicationID]
			value.row = row
			if selector.Kind != thinstore.FeedKindPublication {
				value.sections = append(value.sections, key)
			}
			byPublication[row.PublicationID] = value
		}
	}
	body, err := json.Marshal(sidebar)
	if err != nil {
		return err
	}
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM appview_viewer_feeds WHERE viewer_did = ?", viewer); err != nil {
			return err
		}
		for selector, rows := range memberships {
			sel := []any{viewer, string(selector.Kind), selector.ID}
			if _, err := tx.ExecContext(ctx, "INSERT INTO appview_viewer_feeds (viewer_did,feed_kind,feed_id,updated_at) VALUES (?,?,?,?)",
				append(sel, updatedAt)...); err != nil {
				return err
			}
			for _, row := range rows {
				if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO appview_feed_publications (viewer_did,feed_kind,feed_id,publication_id) VALUES (?,?,?,?)",
					append(sel, row.PublicationID)...); err != nil {
					return err
				}
			}
		}
		ids := make([]string, 0, len(byPublication))
		for id, item := range byPublication {
			ids = append(ids, id)
			row := item.row
			scope := thinstore.NewPublicationScope(viewer, id, row.Scope.AuthorDID, row.Scope.PublicationAtURI,
				row.Scope.PublicationScopeAtURIs, row.Scope.PublicationSiteURLs, item.sections, now)
			_, err := tx.ExecContext(ctx, `
				INSERT INTO appview_publication_scopes
				(viewer_did,publication_id,author_did,publication_at_uri,publication_scope_at_uris,publication_site_urls,scope_keys,section_keys,updated_at)
				VALUES(?,?,?,?,?,?,?,?,?)
				ON CONFLICT(viewer_did,publication_id) DO UPDATE SET
					author_did=excluded.author_did,publication_at_uri=excluded.publication_at_uri,
					publication_scope_at_uris=excluded.publication_scope_at_uris,publication_site_urls=excluded.publication_site_urls,
					scope_keys=excluded.scope_keys,section_keys=excluded.section_keys,updated_at=excluded.updated_at
				WHERE author_did IS NOT excluded.author_did OR publication_at_uri IS NOT excluded.publication_at_uri
					OR publication_scope_at_uris IS NOT excluded.publication_scope_at_uris
					OR publication_site_urls IS NOT excluded.publication_site_urls
					OR scope_keys IS NOT excluded.scope_keys OR section_keys IS NOT excluded.section_keys`,
				viewer, id, scope.AuthorDID, scope.PublicationAtURI, stringJSON(scope.PublicationScopeAtURIs),
				stringJSON(scope.PublicationSiteURLs), stringJSON(scope.ScopeKeys), stringJSON(scope.SectionKeys), updatedAt)
			if err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM appview_publication_scopes
			WHERE viewer_did = ? AND publication_id NOT IN (SELECT value FROM json_each(?))`, viewer, stringJSON(ids)); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO sidebar_projection_cache (viewer_did,json_body,cached_at,expires_at) VALUES(?,?,?,?)
			ON CONFLICT(viewer_did) DO UPDATE SET json_body=excluded.json_body,cached_at=excluded.cached_at,expires_at=excluded.expires_at`,
			viewer, body, updatedAt, thinstore.CursorTimestamp(now.Add(24*time.Hour)))
		return err
	})
}

func stringJSON(values []string) string {
	data, _ := json.Marshal(values)
	return string(data)
}

// ScopeFor returns the visible rows addressed by a legacy bulk mutation.
func (s *Sidebar) ScopeFor(kind, publicationID, folderRKey string) []Row {
	switch strings.TrimSpace(kind) {
	case "publication":
		for _, row := range s.AllRows {
			if row.PublicationID == publicationID {
				return []Row{row}
			}
		}
	case "folder":
		for _, section := range s.FolderSections {
			if section.FolderRKey == folderRKey {
				return section.Publications
			}
		}
	case "subscribed":
		rows := append([]Row{}, s.UnfolderedRows...)
		for _, section := range s.FolderSections {
			rows = append(rows, section.Publications...)
		}
		return rows
	case "following":
		return s.FollowingRows
	}
	return nil
}
