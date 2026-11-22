package reader

import (
	"encoding/json"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

// Lists do not use article HTML. Keep malformed JSON and invalid contentHtml
// types intact so the existing decoder still rejects the same records. Detail
// routes continue reading the full stored render document.
const listRenderSQL = `CASE WHEN json_valid(ci.render_json) THEN
 CASE WHEN json_type(ci.render_json,'$.contentHtml')='text'
 THEN json_remove(ci.render_json,'$.contentHtml') ELSE ci.render_json END
 ELSE ci.render_json END`

// jsonEntryBatch keeps SQL size independent of subscription count. Each scope
// performs its own bounded ordered index scan; only compact candidate metadata
// crosses the JSON table function before the final merge loads render bodies.
func jsonEntryBatch(scopes []feedMatch, migrated bool, viewer, now string, filter thinstore.EntryListFilter, cursor *string, limit int) (string, []any) {
	scopeJSON, _ := json.Marshal(scopes)
	args := []any{string(scopeJSON)}
	timestamp := thinstore.FeedTimestampSQL("ci.created_at")
	filterReadState := filter != thinstore.FilterAll
	scopeJoins := ""
	if filterReadState {
		// A floor belongs to a publication, not an entry. Resolve it before the
		// correlated candidate scan instead of probing its index for each row.
		scopeJoins = `
 LEFT JOIN appview_publication_read_floors floor ON floor.viewer_did=? AND floor.publication_id=scopes.pub`
		args = append(args, viewer)
	}
	branch := func(index, sitePredicate string) string {
		fields, candidateFields := "", ""
		joins := ""
		if filterReadState {
			fields = ",'isRead',is_read"
			candidateFields = "," + readExpression + " AS is_read"
			joins = `
 LEFT JOIN read_marks rm ON rm.viewer_did=? AND rm.subject_uri=ci.uri
 LEFT JOIN appview_unread_overrides uo ON uo.viewer_did=? AND uo.subject_uri=ci.uri`
			args = append(args, migrated, viewer, viewer)
		}
		query := `SELECT json_group_array(json_object('uri',uri,'position',position` + fields + `)) FROM (
 SELECT ci.uri,` + timestamp + " AS position" + candidateFields + `
 FROM content_items ci INDEXED BY ` + index + joins + `
 WHERE ci.author_did=scopes.author AND ` + sitePredicate + ` AND ci.expires_at>?`
		args = append(args, now)
		if cursor != nil {
			at, uri, _ := thinstore.DecodeCursor(*cursor)
			query += " AND (" + timestamp + ",ci.uri) < (?,?)"
			args = append(args, at.UTC().Format("2006-01-02T15:04:05.000000000Z"), uri)
		}
		if filter == thinstore.FilterRead {
			query += " AND (" + readExpression + ")=1"
			args = append(args, migrated)
		} else if filter == thinstore.FilterUnread {
			query += " AND (" + readExpression + ")=0"
			args = append(args, migrated)
		}
		args = append(args, limit)
		return query + " ORDER BY position DESC,ci.uri DESC LIMIT ?)"
	}
	// The input already proves which site policies can occur. Avoid asking the
	// remote engine to describe and optimize an unreachable correlated branch.
	onlySingleSite, onlyNonSingleSite := len(scopes) > 0, len(scopes) > 0
	for _, scope := range scopes {
		onlySingleSite = onlySingleSite && len(scope.Sites) == 1
		onlyNonSingleSite = onlyNonSingleSite && len(scope.Sites) != 1
	}
	var candidates string
	switch {
	case onlySingleSite:
		candidates = "(" + branch("idx_content_items_author_site_timestamp_uri", "ci.publication_site=json_extract(scopes.sites,'$[0]')") + ")"
	case onlyNonSingleSite:
		candidates = "(" + branch("idx_content_items_author_timestamp_uri", "(scopes.sites IS NULL OR ci.publication_site IN (SELECT value FROM json_each(scopes.sites)))") + ")"
	default:
		singleSite := branch("idx_content_items_author_site_timestamp_uri", "ci.publication_site=json_extract(scopes.sites,'$[0]')")
		multipleSites := branch("idx_content_items_author_timestamp_uri", "(scopes.sites IS NULL OR ci.publication_site IN (SELECT value FROM json_each(scopes.sites)))")
		candidates = "CASE WHEN json_array_length(scopes.sites)=1 THEN (" + singleSite + ") ELSE (" + multipleSites + ") END"
	}
	args = append(args, limit)
	stateColumn, stateJoins := "batch.is_read", ""
	if !filterReadState {
		// All does not use read state for filtering or ordering. Join only the
		// globally selected page instead of every candidate from every scope.
		stateColumn = readExpression
		stateJoins = `
 LEFT JOIN read_marks rm ON rm.viewer_did=? AND rm.subject_uri=ci.uri
 LEFT JOIN appview_unread_overrides uo ON uo.viewer_did=? AND uo.subject_uri=ci.uri
 LEFT JOIN appview_publication_read_floors floor ON floor.viewer_did=? AND floor.publication_id=batch.pub`
		args = append([]any{migrated}, args...)
		args = append(args, viewer, viewer, viewer)
	}
	return `SELECT ci.uri,` + listRenderSQL + ` AS render_json,ci.created_at,batch.pub,` + stateColumn + `
FROM (SELECT json_extract(candidate.value,'$.uri') AS uri,scopes.pub,
 json_extract(candidate.value,'$.isRead') AS is_read,json_extract(candidate.value,'$.position') AS position
FROM (
 SELECT json_extract(value,'$.author') AS author,json_extract(value,'$.publication') AS pub,
 json_extract(value,'$.sites') AS sites FROM json_each(?)
) scopes` + scopeJoins + `
JOIN json_each(` + candidates + `) candidate
ORDER BY position DESC,uri DESC,scopes.pub ASC LIMIT ?) batch
JOIN content_items ci ON ci.uri=batch.uri` + stateJoins + `
ORDER BY batch.position DESC,batch.uri DESC,batch.pub ASC`, args
}
