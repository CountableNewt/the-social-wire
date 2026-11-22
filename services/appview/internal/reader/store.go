package reader

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

type rowsQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func scopes(ctx context.Context, db rowsQueryer, viewer, kind, id string) ([]thinstore.PublicationScope, error) {
	query := `SELECT ps.publication_id, ps.author_did, ps.publication_at_uri, ps.publication_scope_at_uris,
		ps.publication_site_urls, ps.scope_keys, ps.section_keys, ps.updated_at FROM appview_publication_scopes ps
		WHERE ps.viewer_did = ?`
	args := []any{viewer}
	if kind != "" {
		query += ` AND EXISTS (SELECT 1 FROM appview_feed_publications fp
			WHERE fp.viewer_did = ps.viewer_did AND fp.publication_id = ps.publication_id AND fp.feed_kind = ? AND fp.feed_id = ?)`
		args = append(args, kind, id)
	}
	rows, err := db.QueryContext(ctx, query+" ORDER BY ps.publication_id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []thinstore.PublicationScope{}
	for rows.Next() {
		var scope thinstore.PublicationScope
		var atURIs, sites, keys, sections []byte
		var updated string
		scope.ViewerDID = viewer
		if err := rows.Scan(&scope.PublicationID, &scope.AuthorDID, &scope.PublicationAtURI,
			&atURIs, &sites, &keys, &sections, &updated); err != nil {
			return nil, err
		}
		destinations := []*[]string{&scope.PublicationScopeAtURIs, &scope.PublicationSiteURLs, &scope.ScopeKeys, &scope.SectionKeys}
		for i, raw := range [][]byte{atURIs, sites, keys, sections} {
			if err := json.Unmarshal(raw, destinations[i]); err != nil {
				return nil, err
			}
		}
		scope.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		result = append(result, scope)
	}
	return result, rows.Err()
}

// readAuthority makes a stale migrated projection unavailable rather than using
// pre-migration legacy rows for a PDS-authoritative viewer.
func readAuthority(ctx context.Context, db rowsQueryer, viewer string) (migrated bool, err error) {
	var ready bool
	err = db.QueryRowContext(ctx, `SELECT manifest_cid IS NOT NULL, projection_ready
		FROM appview_pds_read_state_authority WHERE viewer_did = ?`, viewer).Scan(&migrated, &ready)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err == nil && migrated && !ready {
		return true, httpx.Named(http.StatusServiceUnavailable, "ReadStateProjectionUnavailable", "PDS read-state projection is unavailable")
	}
	return migrated, err
}

type feedMatch struct {
	AuthorDID     string   `json:"author"`
	PublicationID string   `json:"publication"`
	Sites         []string `json:"sites"`
}

// matches maps canonical scope identities onto the raw publication_site values
// used by historical SQLite rows.
func matches(ctx context.Context, db rowsQueryer, scopes []thinstore.PublicationScope) ([]byte, error) {
	authors := map[string]bool{}
	for _, scope := range scopes {
		if scope.PublicationAtURI != nil || len(scope.PublicationScopeAtURIs)+len(scope.PublicationSiteURLs) > 0 {
			authors[scope.AuthorDID] = true
		}
	}
	ids := make([]string, 0, len(authors))
	for author := range authors {
		ids = append(ids, author)
	}
	authorJSON, _ := json.Marshal(ids)
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT author_did, publication_site FROM content_items
		WHERE author_did IN (SELECT value FROM json_each(?)) AND publication_site IS NOT NULL`, string(authorJSON))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byAuthor := map[string][]string{}
	for rows.Next() {
		var author, site string
		if err := rows.Scan(&author, &site); err != nil {
			return nil, err
		}
		byAuthor[author] = append(byAuthor[author], site)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]feedMatch, 0, len(scopes))
	for _, scope := range scopes {
		var sites []string
		if authors[scope.AuthorDID] && (scope.PublicationAtURI != nil || len(scope.PublicationScopeAtURIs)+len(scope.PublicationSiteURLs) > 0) {
			sites = []string{}
			for _, site := range byAuthor[scope.AuthorDID] {
				if thinstore.MatchesPublication(&site, scope.PublicationAtURI, scope.PublicationScopeAtURIs, scope.PublicationSiteURLs) {
					sites = append(sites, site)
				}
			}
			if len(sites) == 0 {
				continue
			}
		}
		result = append(result, feedMatch{scope.AuthorDID, scope.PublicationID, sites})
	}
	return json.Marshal(result)
}

const readExpression = `CASE WHEN ? THEN rm.subject_uri IS NOT NULL
	ELSE rm.subject_uri IS NOT NULL OR (uo.subject_uri IS NULL AND floor.read_floor_at IS NOT NULL AND
	(ci.created_at_us < floor.read_floor_at_us
	OR (ci.created_at_us = floor.read_floor_at_us
	AND (floor.read_floor_uri IS NULL OR ci.uri <= floor.read_floor_uri)))) END`

const readerCTEStart = `WITH scopes AS MATERIALIZED (
		SELECT json_extract(value,'$.author') AS author, json_extract(value,'$.publication') AS pub,
			json_extract(value,'$.sites') AS sites FROM json_each(?)
	), candidates AS (`

const readerJoins = `
	FROM scopes
	JOIN content_items ci ON ci.author_did = scopes.author
		AND (scopes.sites IS NULL OR ci.publication_site IN (SELECT value FROM json_each(scopes.sites)))
	LEFT JOIN read_marks rm ON rm.viewer_did = ? AND rm.subject_uri = ci.uri
	LEFT JOIN appview_unread_overrides uo ON uo.viewer_did = ? AND uo.subject_uri = ci.uri
	LEFT JOIN appview_publication_read_floors floor ON floor.viewer_did = ? AND floor.publication_id = scopes.pub
	WHERE ci.expires_at > ?
`

func listEntries(ctx context.Context, db rowsQueryer, viewer string, scopes []thinstore.PublicationScope,
	filter thinstore.EntryListFilter, cursor *string, limit int, includePublication bool) (thinstore.EntryListResponse, error) {
	page := thinstore.EntryListResponse{Entries: []thinstore.EntryListItem{}}
	if len(scopes) == 0 {
		return page, nil
	}
	migrated, err := readAuthority(ctx, db, viewer)
	if err != nil {
		return page, err
	}
	scopeJSON, err := matches(ctx, db, scopes)
	if err != nil {
		return page, err
	}
	var matched []feedMatch
	if err := json.Unmarshal(scopeJSON, &matched); err != nil {
		return page, err
	}
	now := thinstore.CursorTimestamp(time.Now())
	timestamp := thinstore.FeedTimestampSQL("ci.created_at")
	baseQuery := readerCTEStart + "SELECT ci.uri, ci.render_json, ci.created_at, scopes.pub, " + timestamp + " AS position, " +
		readExpression + " AS is_read" + readerJoins
	suffix := ") SELECT uri,render_json,created_at,pub,is_read FROM candidates"
	switch filter {
	case thinstore.FilterRead:
		suffix += " WHERE is_read = 1"
	case thinstore.FilterUnread:
		suffix += " WHERE is_read = 0"
	}
	suffix += " ORDER BY position DESC, uri DESC, pub ASC LIMIT ?"

	batchSize := max(100, 2*limit)
	seen := map[string]bool{}
	for {
		query := baseQuery
		args := []any{string(scopeJSON), migrated, viewer, viewer, viewer, now}
		if cursor != nil {
			at, uri, valid := thinstore.DecodeCursor(*cursor)
			if !valid {
				return page, httpx.ErrBadRequest
			}
			query += " AND (" + timestamp + ", ci.uri) < (?, ?)"
			args = append(args, at.UTC().Format("2006-01-02T15:04:05.000000000Z"), uri)
		}
		query += suffix
		args = append(args, batchSize)
		// A global sort of every matching author's history defeats pagination.
		// No scope can contribute more than batchSize rows to the global batch,
		// so bound each ordered index scan before merging. Nested groups stay
		// below SQLite's compound SELECT limit for large subscription sets.
		if len(matched) > 1 {
			query, args = boundedEntryBatch(matched, migrated, viewer, now, filter, cursor, batchSize)
		}
		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			return page, err
		}
		var scanned int
		batch, last, scanned, err := readEntries(rows, includePublication)
		if err != nil {
			return page, err
		}
		for _, item := range batch {
			identity := item.EntryID
			if item.OriginalURL != nil {
				identity = *item.OriginalURL
			}
			if item.PublicationID != nil {
				identity = *item.PublicationID + "\x00" + identity
			}
			if !seen[identity] {
				page.Entries = append(page.Entries, item)
				seen[identity] = true
			}
		}
		if len(page.Entries) > limit {
			page.Entries = page.Entries[:limit]
			last := page.Entries[limit-1]
			next := thinstore.EncodeCursor(last.FeedPositionAt, last.EntryID)
			page.Cursor = &next
			return page, nil
		}
		if scanned < batchSize || last == nil {
			return page, nil
		}
		cursor = last
	}
}

func boundedEntryBatch(scopes []feedMatch, migrated bool, viewer, now string, filter thinstore.EntryListFilter, cursor *string, limit int) (string, []any) {
	// Small groups bound compound-select parser depth as subscription sets grow.
	const groupSize = 32
	branches := make([]string, 0, min(len(scopes), groupSize))
	groups := make([]string, 0, (len(scopes)+groupSize-1)/groupSize)
	var args []any
	timestamp := thinstore.FeedTimestampSQL("ci.created_at")
	for i, scope := range scopes {
		sites, _ := json.Marshal(scope.Sites)
		index := "idx_content_items_author_timestamp_uri"
		sitePredicate := "(ci.publication_site IN (SELECT value FROM json_each(?)) OR ?)"
		siteArgs := []any{string(sites), scope.Sites == nil}
		if len(scope.Sites) == 1 {
			index = "idx_content_items_author_site_timestamp_uri"
			sitePredicate = "ci.publication_site=?"
			siteArgs = []any{scope.Sites[0]}
		}
		// The author/time index permits each scope to stop after its own limit;
		// the expiry index would instead scan and sort its complete live history.
		branch := "SELECT ci.uri,ci.render_json,ci.created_at,? AS pub," + timestamp + " AS position," + readExpression + ` AS is_read
 FROM content_items ci INDEXED BY ` + index + `
 LEFT JOIN read_marks rm ON rm.viewer_did=? AND rm.subject_uri=ci.uri
 LEFT JOIN appview_unread_overrides uo ON uo.viewer_did=? AND uo.subject_uri=ci.uri
 LEFT JOIN appview_publication_read_floors floor ON floor.viewer_did=? AND floor.publication_id=?
 WHERE ci.author_did=? AND ` + sitePredicate + ` AND ci.expires_at>?`
		args = append(args, scope.PublicationID, migrated, viewer, viewer, viewer, scope.PublicationID, scope.AuthorDID)
		args = append(args, siteArgs...)
		args = append(args, now)
		if cursor != nil {
			at, uri, _ := thinstore.DecodeCursor(*cursor)
			branch += " AND (" + timestamp + ",ci.uri) < (?,?)"
			args = append(args, at.UTC().Format("2006-01-02T15:04:05.000000000Z"), uri)
		}
		if filter == thinstore.FilterRead {
			branch += " AND (" + readExpression + ")=1"
			args = append(args, migrated)
		} else if filter == thinstore.FilterUnread {
			branch += " AND (" + readExpression + ")=0"
			args = append(args, migrated)
		}
		branch += " ORDER BY position DESC,uri DESC LIMIT ?"
		args = append(args, limit)
		branches = append(branches, "SELECT * FROM ("+branch+")")
		if len(branches) == groupSize || i == len(scopes)-1 {
			group := "SELECT uri,render_json,created_at,pub,position,is_read FROM (" + strings.Join(branches, " UNION ALL ") + ") ORDER BY position DESC,uri DESC,pub ASC LIMIT ?"
			groups = append(groups, "SELECT * FROM ("+group+")")
			args = append(args, limit)
			branches = branches[:0]
		}
	}
	args = append(args, limit)
	return "SELECT uri,render_json,created_at,pub,is_read FROM (" + strings.Join(groups, " UNION ALL ") + ") ORDER BY position DESC,uri DESC,pub ASC LIMIT ?", args
}

func readEntries(rows *sql.Rows, includePublication bool) ([]thinstore.EntryListItem, *string, int, error) {
	defer rows.Close()
	var items []thinstore.EntryListItem
	var last *string
	var count int
	for rows.Next() {
		count++
		var uri, render, created, publication string
		var isRead bool
		if err := rows.Scan(&uri, &render, &created, &publication, &isRead); err != nil {
			return nil, nil, 0, err
		}
		position, err := time.Parse(time.RFC3339Nano, created)
		if err != nil {
			continue
		}
		cursor := thinstore.EncodeCursor(position, uri)
		last = &cursor
		item, _, ok := makeEntry(uri, render, position, isRead)
		if !ok {
			continue
		}
		if includePublication {
			item = item.WithPublicationID(publication)
		}
		items = append(items, item)
	}
	return items, last, count, rows.Err()
}

func makeEntry(uri, raw string, position time.Time, isRead bool) (thinstore.EntryListItem, *thinstore.ContentRenderFields, bool) {
	render, ok := thinstore.DecodeRenderFields(raw)
	if !ok {
		return thinstore.EntryListItem{}, nil, false
	}
	published, err := time.Parse(time.RFC3339Nano, render.PublishedAt)
	if err != nil {
		published = position
	}
	original := render.ArticleURL
	if original == nil {
		if link, ok := thinstore.CanonicalArticleURL(uri); ok {
			original = &link
		} else if render.Summary != nil {
			if link, ok := thinstore.CanonicalArticleURL(*render.Summary); ok {
				original = &link
			}
		}
	}
	return thinstore.EntryListItem{
		EntryID: uri, Title: render.Title, Summary: render.Summary, PublishedAt: published, FeedPositionAt: position,
		ThumbnailURL: render.ThumbnailURL, OriginalURL: original, ContentLabels: render.ContentLabels, IsRead: isRead,
	}, render, true
}

func counts(ctx context.Context, db rowsQueryer, viewer string, scopes []thinstore.PublicationScope) (map[string]int, error) {
	result := make(map[string]int, len(scopes))
	if len(scopes) == 0 {
		return result, nil
	}
	migrated, err := readAuthority(ctx, db, viewer)
	if err != nil {
		return nil, err
	}
	for _, scope := range scopes {
		result[scope.PublicationID] = 0
	}
	scopeJSON, err := matches(ctx, db, scopes)
	if err != nil {
		return nil, err
	}
	query := readerCTEStart + "SELECT scopes.pub, " + readExpression + " AS is_read" + readerJoins +
		") SELECT pub, COUNT(*) FROM candidates WHERE is_read = 0 GROUP BY pub"
	rows, err := db.QueryContext(ctx, query, string(scopeJSON), migrated, viewer, viewer, viewer, thinstore.CursorTimestamp(time.Now()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var count int
		if err := rows.Scan(&id, &count); err != nil {
			return nil, err
		}
		result[id] = count
	}
	return result, rows.Err()
}
