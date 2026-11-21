package thinstore

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// This file ports HtmlTextDecoder.decodePlainText for titles and summaries.

var htmlNamedEntities = map[string]string{
	"amp":    "&",
	"lt":     "<",
	"gt":     ">",
	"quot":   "\"",
	"apos":   "'",
	"nbsp":   " ",
	"ndash":  "–",
	"mdash":  "—",
	"hellip": "…",
	"lsquo":  "‘",
	"rsquo":  "’",
	"ldquo":  "“",
	"rdquo":  "”",
}

var htmlTagPattern = regexp.MustCompile(`<[^>]+>`)

// DecodePlainText strips markup and decodes HTML entities in a plain-text field.
func DecodePlainText(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return trimmed
	}
	return decodeHTMLEntities(stripHTMLTags(trimmed))
}

func stripHTMLTags(text string) string {
	if !strings.Contains(text, "<") {
		return text
	}
	return htmlTagPattern.ReplaceAllString(text, "")
}

func decodeHTMLEntities(text string) string {
	if !strings.Contains(text, "&") {
		return text
	}
	var out strings.Builder
	index := 0
	for index < len(text) {
		if text[index] != '&' {
			_, size := utf8.DecodeRuneInString(text[index:])
			out.WriteString(text[index : index+size])
			index += size
			continue
		}
		if decoded, next, ok := decodeHTMLEntity(text, index); ok {
			out.WriteString(decoded)
			index = next
		} else {
			out.WriteByte('&')
			index++
		}
	}
	return out.String()
}

func decodeHTMLEntity(text string, start int) (string, int, bool) {
	afterAmp := start + 1
	if afterAmp >= len(text) {
		return "", 0, false
	}
	if text[afterAmp] == '#' {
		afterHash := afterAmp + 1
		if afterHash >= len(text) {
			return "", 0, false
		}
		if text[afterHash] == 'x' || text[afterHash] == 'X' {
			return decodeNumericHTMLEntity(text, afterHash+1, 16)
		}
		return decodeNumericHTMLEntity(text, afterHash, 10)
	}
	semi := strings.IndexByte(text[afterAmp:], ';')
	if semi <= 0 {
		return "", 0, false
	}
	name := strings.ToLower(text[afterAmp : afterAmp+semi])
	decoded, ok := htmlNamedEntities[name]
	if !ok {
		return "", 0, false
	}
	return decoded, afterAmp + semi + 1, true
}

func decodeNumericHTMLEntity(text string, start, radix int) (string, int, bool) {
	if start >= len(text) {
		return "", 0, false
	}
	semi := strings.IndexByte(text[start:], ';')
	if semi <= 0 {
		return "", 0, false
	}
	digits := text[start : start+semi]
	codePoint, err := strconv.ParseInt(digits, radix, 64)
	if err != nil || codePoint < 0 || codePoint > utf8.MaxRune || !utf8.ValidRune(rune(codePoint)) {
		return "", 0, false
	}
	return string(rune(codePoint)), start + semi + 1, true
}
