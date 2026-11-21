package operations

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"
	"unicode/utf8"
)

// TraceContext is a W3C trace context.
type TraceContext struct {
	TraceID string
	SpanID  string
	Sampled bool
}

// NewTraceContext starts a new root trace with random identifiers.
func NewTraceContext(sampled bool) TraceContext {
	return TraceContext{TraceID: RandomHex(16), SpanID: RandomHex(8), Sampled: sampled}
}

// ParseTraceparent parses a version 00 traceparent header, rejecting all-zero IDs.
// Like the Swift parser, it checks lengths but not hex digits.
func ParseTraceparent(value string) (TraceContext, bool) {
	var parts []string
	for part := range strings.SplitSeq(value, "-") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) != 4 || parts[0] != "00" ||
		utf8.RuneCountInString(parts[1]) != 32 || utf8.RuneCountInString(parts[2]) != 16 {
		return TraceContext{}, false
	}
	flags, err := strconv.ParseUint(parts[3], 16, 8)
	if err != nil {
		return TraceContext{}, false
	}
	if parts[1] == strings.Repeat("0", 32) || parts[2] == strings.Repeat("0", 16) {
		return TraceContext{}, false
	}
	return TraceContext{TraceID: parts[1], SpanID: parts[2], Sampled: flags&1 == 1}, true
}

// Traceparent renders the context as a traceparent header value.
func (t TraceContext) Traceparent() string {
	flags := "00"
	if t.Sampled {
		flags = "01"
	}
	return "00-" + t.TraceID + "-" + t.SpanID + "-" + flags
}

// Child keeps the trace ID and sampling decision under a new span ID.
func (t TraceContext) Child() TraceContext {
	return TraceContext{TraceID: t.TraceID, SpanID: RandomHex(8), Sampled: t.Sampled}
}

// RandomHex returns bytes random bytes as lowercase hex.
func RandomHex(bytes int) string {
	b := make([]byte, bytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
