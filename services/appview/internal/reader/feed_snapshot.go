package reader

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

// feedSnapshot retains the scope-only interface for independent projection reads.
func feedSnapshot(ctx context.Context, db rowsQueryer, viewer, kind, id string) (string, []thinstore.PublicationScope, bool, error) {
	updated, scopes, _, migrated, err := feedSnapshotWithMatches(ctx, db, viewer, kind, id)
	return updated, scopes, migrated, err
}

// feedSnapshotWithMatches reads membership, its authority fence, and raw site
// candidates in one query. Matching still uses the shared Go identity rules.
// Two fixed UNION arms keep SQL compact and server-described read-only; callers
// keep this query and entry reads in the same transaction.
func feedSnapshotWithMatches(ctx context.Context, db rowsQueryer, viewer, kind, id string) (string, []thinstore.PublicationScope, []feedMatch, bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT 0 AS phase,f.updated_at,
		a.manifest_cid IS NOT NULL AS migrated,COALESCE(a.projection_ready,0) AS ready,
		ps.publication_id,ps.author_did,ps.publication_at_uri,
		ps.publication_scope_at_uris,ps.publication_site_urls,ps.scope_keys,ps.section_keys,ps.updated_at AS scope_updated,NULL AS candidate_site
		FROM appview_viewer_feeds f
		LEFT JOIN appview_pds_read_state_authority a ON a.viewer_did=f.viewer_did
		LEFT JOIN appview_feed_publications fp ON fp.viewer_did=f.viewer_did
			AND fp.feed_kind=f.feed_kind AND fp.feed_id=f.feed_id
		LEFT JOIN appview_publication_scopes ps ON ps.viewer_did=f.viewer_did AND ps.publication_id=fp.publication_id
		WHERE f.viewer_did=? AND f.feed_kind=? AND f.feed_id=?
		UNION ALL
		SELECT DISTINCT 1,NULL,NULL,NULL,NULL,ci.author_did,NULL,NULL,NULL,NULL,NULL,NULL,ci.publication_site
		FROM content_items ci WHERE ci.publication_site IS NOT NULL AND ci.author_did IN (
			SELECT ps.author_did FROM appview_publication_scopes ps
			JOIN appview_feed_publications fp ON fp.viewer_did=ps.viewer_did AND fp.publication_id=ps.publication_id
			JOIN appview_viewer_feeds f ON f.viewer_did=fp.viewer_did AND f.feed_kind=fp.feed_kind AND f.feed_id=fp.feed_id
			WHERE f.viewer_did=? AND f.feed_kind=? AND f.feed_id=?
			AND (ps.publication_at_uri IS NOT NULL OR json_array_length(ps.publication_scope_at_uris)>0 OR json_array_length(ps.publication_site_urls)>0))
		ORDER BY 1,5,6,13`, viewer, kind, id, viewer, kind, id)
	if err != nil {
		return "", nil, nil, false, err
	}
	defer rows.Close()
	var updated string
	var migrated, ready, found bool
	result := []thinstore.PublicationScope{}
	byAuthor := map[string][]string{}
	for rows.Next() {
		var phase int
		var stamp, publication, author, publicationURI, scopeUpdated, site sql.NullString
		var rowMigrated, rowReady sql.NullBool
		var atURIs, sites, keys, sections []byte
		if err := rows.Scan(&phase, &stamp, &rowMigrated, &rowReady, &publication, &author, &publicationURI, &atURIs, &sites, &keys, &sections, &scopeUpdated, &site); err != nil {
			return "", nil, nil, false, err
		}
		if phase == 1 {
			byAuthor[author.String] = append(byAuthor[author.String], site.String)
			continue
		}
		found = true
		updated, migrated, ready = stamp.String, rowMigrated.Bool, rowReady.Bool
		if !publication.Valid {
			continue
		}
		scope := thinstore.PublicationScope{ViewerDID: viewer, PublicationID: publication.String, AuthorDID: author.String}
		if publicationURI.Valid {
			scope.PublicationAtURI = &publicationURI.String
		}
		for i, destination := range []*[]string{&scope.PublicationScopeAtURIs, &scope.PublicationSiteURLs, &scope.ScopeKeys, &scope.SectionKeys} {
			if err := json.Unmarshal([][]byte{atURIs, sites, keys, sections}[i], destination); err != nil {
				return "", nil, nil, false, err
			}
		}
		scope.UpdatedAt, _ = time.Parse(time.RFC3339Nano, scopeUpdated.String)
		result = append(result, scope)
	}
	if err := rows.Err(); err != nil {
		return "", nil, nil, false, err
	}
	if !found {
		return "", nil, nil, false, sql.ErrNoRows
	}
	// Empty feeds retain their existing behavior while stale migrated projections
	// remain unavailable before any entry reads.
	if len(result) > 0 && migrated && !ready {
		return "", nil, nil, true, httpx.Named(http.StatusServiceUnavailable, "ReadStateProjectionUnavailable", "PDS read-state projection is unavailable")
	}
	return updated, result, matchScopeSites(result, byAuthor), migrated, nil
}
