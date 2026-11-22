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

// feedSnapshot reads membership metadata and its authority fence together. Remote
// databases otherwise need three separate Describe/query round trips before the
// entry query can begin. Callers must keep this and entry reads in one transaction.
func feedSnapshot(ctx context.Context, db rowsQueryer, viewer, kind, id string) (string, []thinstore.PublicationScope, bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT f.updated_at,
		a.manifest_cid IS NOT NULL, COALESCE(a.projection_ready, 0),
		ps.publication_id, ps.author_did, ps.publication_at_uri,
		ps.publication_scope_at_uris, ps.publication_site_urls, ps.scope_keys, ps.section_keys, ps.updated_at
		FROM appview_viewer_feeds f
		LEFT JOIN appview_pds_read_state_authority a ON a.viewer_did=f.viewer_did
		LEFT JOIN appview_feed_publications fp ON fp.viewer_did=f.viewer_did
			AND fp.feed_kind=f.feed_kind AND fp.feed_id=f.feed_id
		LEFT JOIN appview_publication_scopes ps ON ps.viewer_did=f.viewer_did AND ps.publication_id=fp.publication_id
		WHERE f.viewer_did=? AND f.feed_kind=? AND f.feed_id=? ORDER BY ps.publication_id`, viewer, kind, id)
	if err != nil {
		return "", nil, false, err
	}
	defer rows.Close()
	var updated string
	var migrated, ready, found bool
	result := []thinstore.PublicationScope{}
	for rows.Next() {
		found = true
		var publication, author, publicationURI, scopeUpdated sql.NullString
		var atURIs, sites, keys, sections []byte
		if err := rows.Scan(&updated, &migrated, &ready, &publication, &author, &publicationURI,
			&atURIs, &sites, &keys, &sections, &scopeUpdated); err != nil {
			return "", nil, false, err
		}
		if !publication.Valid {
			continue
		}
		scope := thinstore.PublicationScope{ViewerDID: viewer, PublicationID: publication.String, AuthorDID: author.String}
		if publicationURI.Valid {
			scope.PublicationAtURI = &publicationURI.String
		}
		for i, destination := range []*[]string{&scope.PublicationScopeAtURIs, &scope.PublicationSiteURLs, &scope.ScopeKeys, &scope.SectionKeys} {
			if err := json.Unmarshal([][]byte{atURIs, sites, keys, sections}[i], destination); err != nil {
				return "", nil, false, err
			}
		}
		scope.UpdatedAt, _ = time.Parse(time.RFC3339Nano, scopeUpdated.String)
		result = append(result, scope)
	}
	if err := rows.Err(); err != nil {
		return "", nil, false, err
	}
	if !found {
		return "", nil, false, sql.ErrNoRows
	}
	// Empty feeds previously returned without consulting read authority. Keep that
	// behavior while rejecting stale migrated projections before any entry reads.
	if len(result) > 0 && migrated && !ready {
		return "", nil, true, httpx.Named(http.StatusServiceUnavailable, "ReadStateProjectionUnavailable", "PDS read-state projection is unavailable")
	}
	return updated, result, migrated, nil
}
