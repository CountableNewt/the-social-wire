package thinstore

import (
	"strconv"
	"strings"
)

// swiftJSONString renders s the way Foundation's JSONEncoder does: quotes,
// backslashes and forward slashes are escaped, control characters use short
// escapes or \u00XX, and everything else (including non-ASCII) is emitted raw.
// Stored JSON columns keep byte-identical spellings with Swift-written rows so
// change-detecting upserts do not rewrite unchanged content.
func swiftJSONString(builder *strings.Builder, s string) {
	builder.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			builder.WriteString(`\"`)
		case '\\':
			builder.WriteString(`\\`)
		case '/':
			builder.WriteString(`\/`)
		case '\n':
			builder.WriteString(`\n`)
		case '\r':
			builder.WriteString(`\r`)
		case '\t':
			builder.WriteString(`\t`)
		case '\b':
			builder.WriteString(`\b`)
		case '\f':
			builder.WriteString(`\f`)
		default:
			if c < 0x20 {
				builder.WriteString(`\u00`)
				hex := strconv.FormatInt(int64(c), 16)
				if len(hex) < 2 {
					builder.WriteByte('0')
				}
				builder.WriteString(hex)
			} else {
				builder.WriteByte(c)
			}
		}
	}
	builder.WriteByte('"')
}

// swiftJSONStringArray encodes values like JSONEncoder().encode([String]).
func swiftJSONStringArray(values []string) string {
	var builder strings.Builder
	builder.WriteByte('[')
	for i, value := range values {
		if i > 0 {
			builder.WriteByte(',')
		}
		swiftJSONString(&builder, value)
	}
	builder.WriteByte(']')
	return builder.String()
}
