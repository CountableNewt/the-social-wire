package richtext

import "strings"

// URLComponents mirrors Foundation's strict URLComponents(string:) parse: the
// string must be a valid RFC 3986 URI reference without any characters that
// would need percent-encoding. It reports the scheme and host when valid.
func URLComponents(value string) (scheme, host string, ok bool) {
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c == '%' {
			if i+2 >= len(value) || !isHex(value[i+1]) || !isHex(value[i+2]) {
				return "", "", false
			}
			continue
		}
		if !isURIChar(c) {
			return "", "", false
		}
	}
	rest := value
	if colon := strings.IndexByte(rest, ':'); colon > 0 && validScheme(rest[:colon]) {
		before := rest[:colon]
		if !strings.ContainsAny(before, "/?#") {
			scheme = before
			rest = rest[colon+1:]
		}
	}
	fragment := ""
	if hash := strings.IndexByte(rest, '#'); hash >= 0 {
		fragment = rest[hash+1:]
		rest = rest[:hash]
		if strings.ContainsRune(fragment, '#') || !allIn(fragment, isQueryChar) {
			return "", "", false
		}
	}
	if q := strings.IndexByte(rest, '?'); q >= 0 {
		if !allIn(rest[q+1:], isQueryChar) {
			return "", "", false
		}
		rest = rest[:q]
	}
	hasAuthority := strings.HasPrefix(rest, "//")
	if hasAuthority {
		rest = rest[2:]
		end := strings.IndexByte(rest, '/')
		if end < 0 {
			end = len(rest)
		}
		authority := rest[:end]
		rest = rest[end:]
		if at := strings.LastIndexByte(authority, '@'); at >= 0 {
			if !allIn(authority[:at], isUserInfoChar) {
				return "", "", false
			}
			authority = authority[at+1:]
		}
		parsed, valid := parseHostPort(authority)
		if !valid {
			return "", "", false
		}
		host = parsed
	} else if scheme == "" && strings.Contains(firstSegment(rest), ":") {
		return "", "", false
	}
	if !allIn(rest, func(c byte) bool { return isPChar(c) || c == '/' }) {
		return "", "", false
	}
	return scheme, host, true
}

func firstSegment(path string) string {
	if slash := strings.IndexByte(path, '/'); slash >= 0 {
		return path[:slash]
	}
	return path
}

func parseHostPort(authority string) (string, bool) {
	host, port := authority, ""
	if strings.HasPrefix(authority, "[") {
		end := strings.IndexByte(authority, ']')
		if end < 0 {
			return "", false
		}
		host = authority[:end+1]
		after := authority[end+1:]
		if after != "" {
			if after[0] != ':' {
				return "", false
			}
			port = after[1:]
		}
		inner := host[1 : len(host)-1]
		if inner == "" || !allIn(inner, func(c byte) bool { return isHex(c) || c == ':' || c == '.' || isUnreserved(c) || isSubDelim(c) }) {
			return "", false
		}
	} else {
		if colon := strings.LastIndexByte(authority, ':'); colon >= 0 {
			host, port = authority[:colon], authority[colon+1:]
		}
		if !allIn(host, func(c byte) bool { return isUnreserved(c) || isSubDelim(c) || c == '%' }) {
			return "", false
		}
	}
	if !allIn(port, func(c byte) bool { return c >= '0' && c <= '9' }) {
		return "", false
	}
	return host, true
}

func validScheme(s string) bool {
	if s == "" || !isAlpha(s[0]) {
		return false
	}
	return allIn(s, func(c byte) bool { return isAlpha(c) || (c >= '0' && c <= '9') || c == '+' || c == '-' || c == '.' })
}

func allIn(s string, allowed func(byte) bool) bool {
	for i := 0; i < len(s); i++ {
		if !allowed(s[i]) {
			return false
		}
	}
	return true
}

func isAlpha(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func isUnreserved(c byte) bool {
	return isAlpha(c) || (c >= '0' && c <= '9') || c == '-' || c == '.' || c == '_' || c == '~'
}

func isSubDelim(c byte) bool { return strings.IndexByte("!$&'()*+,;=", c) >= 0 }

func isPChar(c byte) bool {
	return isUnreserved(c) || isSubDelim(c) || c == ':' || c == '@' || c == '%'
}

func isQueryChar(c byte) bool { return isPChar(c) || c == '/' || c == '?' }

func isUserInfoChar(c byte) bool { return isUnreserved(c) || isSubDelim(c) || c == ':' || c == '%' }

func isURIChar(c byte) bool {
	return isUnreserved(c) || isSubDelim(c) || strings.IndexByte(":/?#[]@", c) >= 0
}

// IsSafeHTTPURL is the facet link check: a strict http(s) URL with a host.
func IsSafeHTTPURL(value string) bool {
	scheme, host, ok := URLComponents(value)
	scheme = strings.ToLower(scheme)
	return ok && (scheme == "http" || scheme == "https") && host != ""
}
