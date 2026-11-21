package thinstore

// FeedTimestampSQL is the built-in expression used in indexes, ORDER BY, and seek
// bounds. It normalizes the whole second separately so SQLite never rounds a
// fractional .999999 value into the next second, and preserves up to nine
// fractional digits; writers store six and older databases stored whole seconds.
func FeedTimestampSQL(column string) string {
	zone := "CASE WHEN substr(" + column + ",-1)='Z' THEN 'Z' ELSE substr(" + column + ",-6) END"
	fraction := "CASE WHEN substr(" + column + ",20,1)='.' THEN substr(" + column + ",21,length(" + column +
		")-(CASE WHEN substr(" + column + ",-1)='Z' THEN 21 ELSE 26 END)) ELSE '' END"
	return "(strftime('%Y-%m-%dT%H:%M:%S',substr(" + column + ",1,19)||(" + zone + "))||'.'||substr((" +
		fraction + ")||'000000000',1,9)||'Z')"
}
