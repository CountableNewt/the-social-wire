// Package richtext ports SkejKit's post canonicalization: the social-Markdown
// compiler, native facet detection and validation, and embed normalization.
//
// Swift indexes strings by Character (extended grapheme cluster). This package
// keeps that unit with a compact UAX #29 segmenter so facet boundary checks and
// length limits count what Swift counted.
package richtext

import (
	"unicode"
	"unicode/utf8"
)

// Clusters splits s into extended grapheme clusters.
func Clusters(s string) []string {
	offsets := boundaries(s)
	clusters := make([]string, 0, len(offsets))
	for i := 1; i < len(offsets); i++ {
		clusters = append(clusters, s[offsets[i-1]:offsets[i]])
	}
	return clusters
}

// GraphemeCount is Swift's String.count.
func GraphemeCount(s string) int {
	if s == "" {
		return 0
	}
	return len(boundaries(s)) - 1
}

// IsBoundary reports whether byte offset lies on a Character boundary, like
// String.UTF8View.Index.samePosition(in:) returning non-nil.
func IsBoundary(s string, offset int) bool {
	if offset == 0 || offset == len(s) {
		return true
	}
	if offset < 0 || offset > len(s) {
		return false
	}
	for _, b := range boundaries(s) {
		if b == offset {
			return true
		}
		if b > offset {
			return false
		}
	}
	return false
}

// boundaries returns every cluster boundary byte offset, including 0 and len(s).
func boundaries(s string) []int {
	result := []int{0}
	if s == "" {
		return result
	}
	var prev rune = -1
	regionalRun := 0
	lastBaseWasPictographic := false
	for offset, r := range s {
		if prev >= 0 && breaksBetween(prev, r, regionalRun, lastBaseWasPictographic) {
			result = append(result, offset)
		}
		if isRegionalIndicator(r) {
			regionalRun++
		} else {
			regionalRun = 0
		}
		if isExtendedPictographic(r) {
			lastBaseWasPictographic = true
		} else if !isExtend(r) && r != zwj {
			lastBaseWasPictographic = false
		}
		prev = r
	}
	if last := result[len(result)-1]; last != len(s) {
		result = append(result, len(s))
	}
	return result
}

const zwj = '\u200D'

func breaksBetween(prev, next rune, regionalRun int, pictographicBeforeZWJ bool) bool {
	if prev == '\r' && next == '\n' {
		return false
	}
	if isControl(prev) || isControl(next) {
		return true
	}
	if hangulJoins(prev, next) {
		return false
	}
	if isExtend(next) || next == zwj || unicode.Is(unicode.Mc, next) {
		return false
	}
	if prev == zwj && isExtendedPictographic(next) && pictographicBeforeZWJ {
		return false
	}
	if isRegionalIndicator(prev) && isRegionalIndicator(next) && regionalRun%2 == 1 {
		return false
	}
	return true
}

func isControl(r rune) bool {
	if r == zwj || r == '\u200C' {
		return false
	}
	return r == '\r' || r == '\n' || unicode.Is(unicode.Cc, r) ||
		unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) ||
		(unicode.Is(unicode.Cf, r) && !(r >= 0xE0020 && r <= 0xE007F))
}

func isExtend(r rune) bool {
	return unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || r == '\u200C' ||
		(r >= 0x1F3FB && r <= 0x1F3FF) || (r >= 0xE0020 && r <= 0xE007F)
}

func isRegionalIndicator(r rune) bool { return r >= 0x1F1E6 && r <= 0x1F1FF }

func isExtendedPictographic(r rune) bool {
	switch {
	case r == 0x00A9, r == 0x00AE, r == 0x203C, r == 0x2049, r == 0x2122, r == 0x2139,
		r >= 0x2194 && r <= 0x2199, r == 0x21A9, r == 0x21AA, r == 0x231A, r == 0x231B,
		r == 0x2328, r == 0x23CF, r >= 0x23E9 && r <= 0x23F3, r >= 0x23F8 && r <= 0x23FA,
		r == 0x24C2, r == 0x25AA, r == 0x25AB, r == 0x25B6, r == 0x25C0,
		r >= 0x25FB && r <= 0x25FE, r >= 0x2600 && r <= 0x27BF, r == 0x2934, r == 0x2935,
		r >= 0x2B05 && r <= 0x2B07, r == 0x2B1B, r == 0x2B1C, r == 0x2B50, r == 0x2B55,
		r == 0x3030, r == 0x303D, r == 0x3297, r == 0x3299:
		return true
	case r >= 0x1F000 && r <= 0x1FAFF:
		return !isRegionalIndicator(r) && !(r >= 0x1F3FB && r <= 0x1F3FF)
	case r >= 0x1FC00 && r <= 0x1FFFD:
		return true
	}
	return false
}

type hangulKind int

const (
	hangulNone hangulKind = iota
	hangulL
	hangulV
	hangulT
	hangulLV
	hangulLVT
)

func hangul(r rune) hangulKind {
	switch {
	case r >= 0x1100 && r <= 0x115F, r >= 0xA960 && r <= 0xA97C:
		return hangulL
	case r >= 0x1160 && r <= 0x11A7, r >= 0xD7B0 && r <= 0xD7C6:
		return hangulV
	case r >= 0x11A8 && r <= 0x11FF, r >= 0xD7CB && r <= 0xD7FB:
		return hangulT
	case r >= 0xAC00 && r <= 0xD7A3:
		if (r-0xAC00)%28 == 0 {
			return hangulLV
		}
		return hangulLVT
	}
	return hangulNone
}

func hangulJoins(prev, next rune) bool {
	a, b := hangul(prev), hangul(next)
	switch a {
	case hangulL:
		return b == hangulL || b == hangulV || b == hangulLV || b == hangulLVT
	case hangulLV, hangulV:
		return b == hangulV || b == hangulT
	case hangulLVT, hangulT:
		return b == hangulT
	}
	return false
}

// firstRune returns the first scalar of s, which Swift's Character
// properties (isWhitespace, isNumber, isLetter, isPunctuation) inspect.
func firstRune(s string) rune {
	r, _ := utf8.DecodeRuneInString(s)
	return r
}

func isWhitespaceCharacter(s string) bool {
	return s != "" && unicode.IsSpace(firstRune(s))
}

func isNumberCharacter(s string) bool { return s != "" && unicode.IsNumber(firstRune(s)) }

func isLetterCharacter(s string) bool { return s != "" && unicode.IsLetter(firstRune(s)) }

func isPunctuationCharacter(s string) bool { return s != "" && unicode.IsPunct(firstRune(s)) }
