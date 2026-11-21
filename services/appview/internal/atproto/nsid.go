package atproto

import (
	"strings"
	"unicode"
)

// NSID is a parsed AT Protocol namespaced identifier (com.example.collection).
type NSID struct {
	Authority string
	Name      string
}

// String returns the canonical lowercased NSID.
func (n NSID) String() string { return n.Authority + "." + n.Name }

// ParseNSID trims, lowercases, and validates raw (at least three labels of
// letters, digits, and hyphens, each starting with a letter or digit).
func ParseNSID(raw string) (NSID, bool) {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	if trimmed == "" {
		return NSID{}, false
	}
	labels := strings.Split(trimmed, ".")
	if len(labels) < 3 {
		return NSID{}, false
	}
	for _, label := range labels {
		if !validNSIDLabel(label) {
			return NSID{}, false
		}
	}
	return NSID{Authority: strings.Join(labels[:len(labels)-1], "."), Name: labels[len(labels)-1]}, true
}

// NormalizeNSID returns the canonical NSID or ErrInvalidNSID.
func NormalizeNSID(raw string) (string, error) {
	nsid, ok := ParseNSID(raw)
	if !ok {
		return "", ErrInvalidNSID
	}
	return nsid.String(), nil
}

func validNSIDLabel(label string) bool {
	runes := []rune(label)
	if len(runes) == 0 || len(runes) > 63 {
		return false
	}
	if !unicode.IsLetter(runes[0]) && !unicode.IsNumber(runes[0]) {
		return false
	}
	for _, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '-' {
			return false
		}
	}
	return true
}
