package reader

// unreadCountsQuery selects the relevant ordered index independently for each
// publication, rather than sorting or scanning all of an RSS author's feeds.
func unreadCountsQuery(scopeJSON []byte, migrated bool, viewer, now string) (string, []any) {
	var args []any
	branch := func(index, sitePredicate string) string {
		args = append(args, viewer, viewer, viewer, now, migrated)
		return `SELECT count(*) FROM content_items ci INDEXED BY ` + index + `
 LEFT JOIN read_marks rm ON rm.viewer_did=? AND rm.subject_uri=ci.uri
 LEFT JOIN appview_unread_overrides uo ON uo.viewer_did=? AND uo.subject_uri=ci.uri
 LEFT JOIN appview_publication_read_floors floor ON floor.viewer_did=? AND floor.publication_id=scopes.pub
 WHERE ci.author_did=scopes.author AND ` + sitePredicate + ` AND ci.expires_at>? AND (` + readExpression + `)=0`
	}
	singleSite := branch("idx_content_items_author_site_timestamp_uri", "ci.publication_site=json_extract(scopes.sites,'$[0]')")
	multipleSites := branch("idx_content_items_author_timestamp_uri", "(scopes.sites IS NULL OR ci.publication_site IN (SELECT value FROM json_each(scopes.sites)))")
	args = append(args, string(scopeJSON))
	return `SELECT pub,SUM(unread_count) FROM (
 SELECT scopes.pub,CASE WHEN json_array_length(scopes.sites)=1 THEN (` + singleSite + `) ELSE (` + multipleSites + `) END AS unread_count
 FROM ` + readerScopeRows + `) GROUP BY pub`, args
}
