package richtext

import (
	"unicode"
	"unicode/utf8"
)

// byteRange is a half-open UTF-8 byte range, Swift's Range<Int>.
type byteRange struct{ start, end int }

func (r byteRange) overlaps(other byteRange) bool {
	if r.start == r.end || other.start == other.end {
		return false
	}
	return r.start < other.end && other.start < r.end
}

// tagInvisible are the separators the @atproto/api tag pattern excludes.
func tagInvisible(r rune) bool {
	switch r {
	case '\u00AD', '\u2060', '\u200A', '\u200B', '\u200C', '\u200D', '\u20E2':
		return true
	}
	return false
}

// tagBodyRune is [^\s<invisible>].
func tagBodyRune(r rune) bool { return !unicode.IsSpace(r) && !tagInvisible(r) }

// tagQualifyingRune is [^\d\s\p{P}<invisible>].
func tagQualifyingRune(r rune) bool {
	return tagBodyRune(r) && !unicode.IsDigit(r) && !unicode.IsPunct(r)
}

// tagCaptures emulates matching
//
//	(?:^|\s)([#＃](?!\uFE0F)[^\s…]*[^\d\s\p{P}…]+[^\s…]*)
//
// left to right without overlaps, returning each capture-group byte range
// (the hashtag including its leading marker).
func tagCaptures(text string) []byteRange {
	var captures []byteRange
	position := 0
	for position <= len(text) {
		if capture, matchEnd, ok := tagMatchAt(text, position); ok {
			captures = append(captures, capture)
			position = matchEnd
			continue
		}
		if position == len(text) {
			break
		}
		_, size := utf8.DecodeRuneInString(text[position:])
		position += size
	}
	return captures
}

func tagMatchAt(text string, position int) (byteRange, int, bool) {
	if position == 0 {
		if capture, ok := tagGroupAt(text, 0); ok {
			return capture, capture.end, true
		}
	}
	if position < len(text) {
		r, size := utf8.DecodeRuneInString(text[position:])
		if unicode.IsSpace(r) {
			if capture, ok := tagGroupAt(text, position+size); ok {
				return capture, capture.end, true
			}
		}
	}
	return byteRange{}, 0, false
}

func tagGroupAt(text string, start int) (byteRange, bool) {
	if start >= len(text) {
		return byteRange{}, false
	}
	marker, size := utf8.DecodeRuneInString(text[start:])
	if marker != '#' && marker != '＃' {
		return byteRange{}, false
	}
	cursor := start + size
	if cursor < len(text) {
		if next, _ := utf8.DecodeRuneInString(text[cursor:]); next == '\uFE0F' {
			return byteRange{}, false
		}
	}
	qualified := false
	for cursor < len(text) {
		r, width := utf8.DecodeRuneInString(text[cursor:])
		if !tagBodyRune(r) {
			break
		}
		if tagQualifyingRune(r) {
			qualified = true
		}
		cursor += width
	}
	if !qualified {
		return byteRange{}, false
	}
	return byteRange{start, cursor}, true
}

// firstClusterLength is the byte width of the first Character of s.
func firstClusterLength(s string) int {
	offsets := boundaries(s)
	if len(offsets) < 2 {
		return 0
	}
	return offsets[1]
}
