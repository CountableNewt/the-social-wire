package reader

import (
	"encoding/json"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

// jsonEntryBatch keeps SQL size independent of subscription count. Each scope
// performs its own bounded ordered index scan; only compact candidate metadata
// crosses the JSON table function before the final merge loads render bodies.
func jsonEntryBatch(scopes []feedMatch, migrated bool, viewer, now string, filter thinstore.EntryListFilter, cursor *string, limit int) (string, []any) {
	scopeJSON, _ := json.Marshal(scopes)
	args := []any{string(scopeJSON)}
	timestamp := thinstore.FeedTimestampSQL("ci.created_at")
	branch := func(index, sitePredicate string) string {
		query := `SELECT json_group_array(json_object('uri',uri,'position',position,'isRead',is_read)) FROM (
 SELECT ci.uri,` + timestamp + " AS position," + readExpression + ` AS is_read
 FROM content_items ci INDEXED BY ` + index + `
 LEFT JOIN read_marks rm ON rm.viewer_did=? AND rm.subject_uri=ci.uri
 LEFT JOIN appview_unread_overrides uo ON uo.viewer_did=? AND uo.subject_uri=ci.uri
 LEFT JOIN appview_publication_read_floors floor ON floor.viewer_did=? AND floor.publication_id=scopes.pub
 WHERE ci.author_did=scopes.author AND ` + sitePredicate + ` AND ci.expires_at>?`
		args = append(args, migrated, viewer, viewer, viewer, now)
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
	singleSite := branch("idx_content_items_author_site_timestamp_uri", "ci.publication_site=json_extract(scopes.sites,'$[0]')")
	multipleSites := branch("idx_content_items_author_timestamp_uri", "(scopes.sites IS NULL OR ci.publication_site IN (SELECT value FROM json_each(scopes.sites)))")
	args = append(args, limit)
	return `SELECT ci.uri,ci.render_json,ci.created_at,batch.pub,batch.is_read
FROM (SELECT json_extract(candidate.value,'$.uri') AS uri,scopes.pub,
 json_extract(candidate.value,'$.isRead') AS is_read,json_extract(candidate.value,'$.position') AS position
FROM (
 SELECT json_extract(value,'$.author') AS author,json_extract(value,'$.publication') AS pub,
 json_extract(value,'$.sites') AS sites FROM json_each(?)
) scopes
JOIN json_each(CASE WHEN json_array_length(scopes.sites)=1 THEN (` + singleSite + `) ELSE (` + multipleSites + `) END) candidate
ORDER BY position DESC,uri DESC,scopes.pub ASC LIMIT ?) batch
JOIN content_items ci ON ci.uri=batch.uri
ORDER BY batch.position DESC,batch.uri DESC,batch.pub ASC`, args
}
