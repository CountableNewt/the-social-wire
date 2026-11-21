package richtext

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Mention is a handle the client resolved to a DID (ResolvedMention).
type Mention struct {
	Handle string `json:"handle"`
	DID    string `json:"did"`
}

// Compilation is a post text projection with its UTF-8 facets.
type Compilation struct {
	Text   string
	Facets []any
}

// mentionExpression mirrors the Swift pattern with ICU's Unicode \s.
var mentionExpression = regexp.MustCompile(`(?i)(?:^|[\s\p{Z}\x{85}\x{0B}\(\[\{\:>"'])(@[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?)`)

// Compile compiles Skej's small social-Markdown profile into post text and
// link, tag, and mention facets. Unsupported or malformed constructs stay
// literal so input is never lost.
func Compile(markdown string, mentions []Mention) Compilation {
	buffer := &compilationBuffer{}
	normalized := strings.ReplaceAll(strings.ReplaceAll(markdown, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(normalized, "\n")
	fenced := ""
	for index, line := range lines {
		trimmed := trimWhitespaces(line)
		delimiter := fenceDelimiter(trimmed)
		switch {
		case fenced != "":
			buffer.appendSuppressed(line)
			if delimiter == fenced {
				fenced = ""
			}
		case delimiter != "":
			fenced = delimiter
			buffer.appendSuppressed(line)
		case isExcludedBlock(line):
			buffer.appendSuppressed(line)
		default:
			prefix, body := structuralPrefix(line)
			buffer.append(prefix)
			compileInline(Clusters(body), buffer)
		}
		if index < len(lines)-1 {
			buffer.append("\n")
		}
	}
	appendTagFacets(buffer)
	appendMentionFacets(mentions, buffer)
	buffer.sortFacets()
	facets := buffer.facets
	if facets == nil {
		facets = []any{}
	}
	return Compilation{Text: buffer.text.String(), Facets: facets}
}

// Project compiles a Markdown source and trims surrounding whitespace,
// rebasing facets onto the trimmed text.
func Project(sourceText string, mentions []Mention) Compilation {
	compilation := Compile(sourceText, mentions)
	clusters := Clusters(compilation.Text)
	first, last := -1, -1
	for i, cluster := range clusters {
		if !isWhitespaceCharacter(cluster) {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		return Compilation{Text: "", Facets: []any{}}
	}
	leading := len(strings.Join(clusters[:first], ""))
	text := strings.Join(clusters[first:last+1], "")
	facets := []any{}
	for _, facet := range compilation.Facets {
		r, ok := bufferByteRange(facet)
		object, isObject := facet.(map[string]any)
		if !ok || r.start < leading || r.end > leading+len(text) || !isObject {
			continue
		}
		rebased := make(map[string]any, len(object))
		for key, value := range object {
			rebased[key] = value
		}
		rebased["index"] = map[string]any{"byteStart": float64(r.start - leading), "byteEnd": float64(r.end - leading)}
		facets = append(facets, rebased)
	}
	return Compilation{Text: text, Facets: facets}
}

func compileInline(source []string, buffer *compilationBuffer) {
	index := 0
	for index < len(source) {
		if source[index] == "\\" && index+1 < len(source) && isEscapable(source[index+1]) {
			buffer.appendSuppressed(source[index+1])
			index += 2
			continue
		}
		if source[index] == "<" {
			if end := indexOf(source, ">", index); end >= 0 {
				buffer.appendSuppressed(join(source[index : end+1]))
				index = end + 1
				continue
			}
		}
		if hasPrefix(source, index, "!", "[") {
			if syntax, ok := linkSyntax(source, index+1); ok {
				buffer.appendSuppressed(join(source[index:syntax.end]))
				index = syntax.end
				continue
			}
		}
		if source[index] == "[" {
			if syntax, ok := linkSyntax(source, index); ok {
				destination := trimWhitespaces(join(source[syntax.destinationStart:syntax.destinationEnd]))
				if syntax.labelEnd > syntax.labelStart && isSafeMarkdownURL(destination) {
					start := buffer.utf8Count()
					compileInline(source[syntax.labelStart:syntax.labelEnd], buffer)
					end := buffer.utf8Count()
					if start < end {
						buffer.removeFacets(byteRange{start, end})
						buffer.appendFacet(start, end, map[string]any{"$type": "app.bsky.richtext.facet#link", "uri": destination})
					}
				} else {
					buffer.appendSuppressed(join(source[index:syntax.end]))
				}
				index = syntax.end
				continue
			}
		}
		if source[index] == "`" {
			if closing := closingMarker(source, "`", index); closing >= 0 && index+1 < closing {
				buffer.appendSuppressed(join(source[index : closing+1]))
				index = closing + 1
				continue
			}
		}
		if marker := emphasisMarker(source, index); marker != "" {
			width := len(marker)
			if closing := closingMarker(source, marker, index); closing >= 0 && index+width < closing {
				compileInline(source[index+width:closing], buffer)
				index = closing + width
				continue
			}
		}
		if hasCaseInsensitivePrefix(source, index, "https://") || hasCaseInsensitivePrefix(source, index, "http://") {
			candidateEnd := bareURLCandidateEnd(source, index)
			candidate := join(source[index:candidateEnd])
			link := trimmedBareURL(candidate)
			if isSafeMarkdownURL(link) {
				start := buffer.utf8Count()
				buffer.append(candidate)
				buffer.appendFacet(start, start+len(link), map[string]any{"$type": "app.bsky.richtext.facet#link", "uri": link})
				index = candidateEnd
				continue
			}
		}
		buffer.append(source[index])
		index++
	}
}

func structuralPrefix(line string) (string, string) {
	body := Clusters(line)
	indentation := ""
	for len(body) > 0 && (body[0] == " " || body[0] == "\t") {
		indentation += body[0]
		body = body[1:]
	}
	quotePrefix := ""
	for len(body) > 0 && body[0] == ">" {
		body = body[1:]
		if len(body) > 0 && body[0] == " " {
			body = body[1:]
		}
		quotePrefix += "\u203A "
	}
	if len(body) >= 2 && (body[0] == "-" || body[0] == "+" || body[0] == "*") && body[1] == " " {
		return indentation + quotePrefix + "\u2022 ", join(body[2:])
	}
	digitEnd := 0
	for digitEnd < len(body) && isNumberCharacter(body[digitEnd]) {
		digitEnd++
	}
	if digitEnd > 0 && digitEnd < len(body) && (body[digitEnd] == "." || body[digitEnd] == ")") {
		space := digitEnd + 1
		if space < len(body) && body[space] == " " {
			return indentation + quotePrefix + join(body[:digitEnd]) + ". ", join(body[space+1:])
		}
	}
	return indentation + quotePrefix, join(body)
}

func fenceDelimiter(trimmed string) string {
	if strings.HasPrefix(trimmed, "```") {
		return "```"
	}
	if strings.HasPrefix(trimmed, "~~~") {
		return "~~~"
	}
	return ""
}

func isExcludedBlock(line string) bool {
	trimmed := Clusters(trimWhitespaces(line))
	hashes := 0
	for hashes < len(trimmed) && trimmed[hashes] == "#" {
		hashes++
	}
	if hashes >= 1 && hashes <= 6 && hashes < len(trimmed) && isWhitespaceCharacter(trimmed[hashes]) {
		return true
	}
	if len(trimmed) > 0 && trimmed[0] == "|" {
		return true
	}
	if len(trimmed) > 0 && (trimmed[0] == "-" || trimmed[0] == "+" || trimmed[0] == "*") {
		remainder := trimmed[1:]
		for len(remainder) > 0 && isWhitespaceCharacter(remainder[0]) {
			remainder = remainder[1:]
		}
		rest := join(remainder)
		if strings.HasPrefix(rest, "[ ] ") || strings.HasPrefix(rest, "[x] ") || strings.HasPrefix(rest, "[X] ") {
			return true
		}
	}
	joined := join(trimmed)
	return joined == "---" || joined == "***" || joined == "___"
}

func emphasisMarker(source []string, index int) string {
	for _, marker := range []string{"**", "__", "~~", "*", "_"} {
		if !hasPrefix(source, index, strings.Split(marker, "")...) {
			continue
		}
		contentStart := index + len(marker)
		if contentStart >= len(source) || isWhitespaceCharacter(source[contentStart]) {
			continue
		}
		if strings.Contains(marker, "_") && index > 0 && isAlphanumeric(source[index-1]) {
			continue
		}
		return marker
	}
	return ""
}

func closingMarker(source []string, marker string, opening int) int {
	parts := strings.Split(marker, "")
	index := opening + len(parts)
	for index < len(source) {
		if source[index] == "\\" {
			index++
			if index < len(source) {
				index++
			}
			continue
		}
		if hasPrefix(source, index, parts...) {
			before := index - 1
			after := index + len(parts)
			if !isWhitespaceCharacter(source[before]) &&
				!(strings.Contains(marker, "_") && after < len(source) && isAlphanumeric(source[after])) {
				return index
			}
		}
		index++
	}
	return -1
}

type linkSyntaxRange struct {
	labelStart, labelEnd             int
	destinationStart, destinationEnd int
	end                              int
}

func linkSyntax(source []string, opening int) (linkSyntaxRange, bool) {
	if opening >= len(source) || source[opening] != "[" {
		return linkSyntaxRange{}, false
	}
	closeBracket := opening + 1
	for closeBracket < len(source) {
		if source[closeBracket] == "\\" {
			closeBracket++
			if closeBracket < len(source) {
				closeBracket++
			}
			continue
		}
		if source[closeBracket] == "]" {
			break
		}
		closeBracket++
	}
	if closeBracket >= len(source) {
		return linkSyntaxRange{}, false
	}
	openParen := closeBracket + 1
	if openParen >= len(source) || source[openParen] != "(" {
		return linkSyntaxRange{}, false
	}
	depth := 1
	cursor := openParen + 1
	for cursor < len(source) {
		if source[cursor] == "\\" {
			cursor++
			if cursor < len(source) {
				cursor++
			}
			continue
		}
		if source[cursor] == "(" {
			depth++
		}
		if source[cursor] == ")" {
			depth--
			if depth == 0 {
				return linkSyntaxRange{
					labelStart: opening + 1, labelEnd: closeBracket,
					destinationStart: openParen + 1, destinationEnd: cursor,
					end: cursor + 1,
				}, true
			}
		}
		cursor++
	}
	return linkSyntaxRange{}, false
}

func bareURLCandidateEnd(source []string, start int) int {
	index := start
	for index < len(source) {
		character := source[index]
		if isWhitespaceCharacter(character) || character == "<" || character == ">" || character == "\"" || character == "'" {
			break
		}
		index++
	}
	return index
}

func trimmedBareURL(candidate string) string {
	result := Clusters(candidate)
	for len(result) > 0 && len(result[len(result)-1]) == 1 && strings.Contains(".,!?;:", result[len(result)-1]) {
		result = result[:len(result)-1]
	}
	for _, pair := range [][2]string{{"(", ")"}, {"[", "]"}, {"{", "}"}} {
		for len(result) > 0 && result[len(result)-1] == pair[1] && count(result, pair[1]) > count(result, pair[0]) {
			result = result[:len(result)-1]
		}
	}
	return join(result)
}

func isSafeMarkdownURL(value string) bool {
	for _, r := range value {
		if unicode.IsSpace(r) {
			return false
		}
	}
	return IsSafeHTTPURL(value)
}

func appendTagFacets(buffer *compilationBuffer) {
	text := buffer.text.String()
	occupied := append(buffer.facetRanges(), buffer.suppressed...)
	for _, capture := range tagCaptures(text) {
		group := text[capture.start:capture.end]
		markerWidth := firstClusterLength(group)
		bodyClusters := Clusters(group[markerWidth:])
		for len(bodyClusters) > 0 && isPunctuationCharacter(bodyClusters[len(bodyClusters)-1]) {
			bodyClusters = bodyClusters[:len(bodyClusters)-1]
		}
		if len(bodyClusters) == 0 || len(bodyClusters) > maxTagLength {
			continue
		}
		body := join(bodyClusters)
		r := byteRange{capture.start, capture.start + markerWidth + len(body)}
		if overlapsAny(occupied, r) {
			continue
		}
		buffer.appendFacet(r.start, r.end, map[string]any{"$type": "app.bsky.richtext.facet#tag", "tag": body})
	}
}

func appendMentionFacets(mentions []Mention, buffer *compilationBuffer) {
	resolved := map[string]string{}
	for _, mention := range mentions {
		key := strings.ToLower(strings.TrimPrefix(mention.Handle, "@"))
		if _, exists := resolved[key]; !exists {
			resolved[key] = mention.DID
		}
	}
	if len(resolved) == 0 {
		return
	}
	text := buffer.text.String()
	for _, match := range mentionExpression.FindAllStringSubmatchIndex(text, -1) {
		start, end := match[2], match[3]
		if end < len(text) {
			next := firstRune(text[end:])
			if unicode.IsLetter(next) || unicode.IsNumber(next) || next == '.' || next == '-' {
				continue
			}
		}
		display := text[start:end]
		did, ok := resolved[strings.ToLower(display[1:])]
		if !ok || !strings.HasPrefix(did, "did:") {
			continue
		}
		r := byteRange{start, end}
		if overlapsAny(append(buffer.facetRanges(), buffer.suppressed...), r) {
			continue
		}
		buffer.appendFacet(start, end, map[string]any{"$type": "app.bsky.richtext.facet#mention", "did": did})
	}
}

func isEscapable(character string) bool {
	return len(character) == 1 && strings.Contains("\\`*_{}[]()#+-.!~>", character)
}

func isAlphanumeric(character string) bool {
	return isLetterCharacter(character) || isNumberCharacter(character)
}

func hasPrefix(source []string, index int, prefix ...string) bool {
	if index+len(prefix) > len(source) {
		return false
	}
	for i, part := range prefix {
		if source[index+i] != part {
			return false
		}
	}
	return true
}

func hasCaseInsensitivePrefix(source []string, index int, prefix string) bool {
	end := min(index+len(prefix), len(source))
	return strings.ToLower(join(source[index:end])) == prefix
}

func indexOf(source []string, target string, from int) int {
	for i := from; i < len(source); i++ {
		if source[i] == target {
			return i
		}
	}
	return -1
}

func count(source []string, target string) int {
	n := 0
	for _, s := range source {
		if s == target {
			n++
		}
	}
	return n
}

func join(clusters []string) string { return strings.Join(clusters, "") }

// trimWhitespaces trims CharacterSet.whitespaces (Zs plus tab).
func trimWhitespaces(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return r == '\t' || unicode.Is(unicode.Zs, r) })
}

type compilationBuffer struct {
	text       strings.Builder
	facets     []any
	suppressed []byteRange
}

func (b *compilationBuffer) utf8Count() int { return b.text.Len() }

func (b *compilationBuffer) append(value string) { b.text.WriteString(value) }

func (b *compilationBuffer) appendSuppressed(value string) {
	start := b.utf8Count()
	b.text.WriteString(value)
	if end := b.utf8Count(); start < end {
		b.suppressed = append(b.suppressed, byteRange{start, end})
	}
}

func (b *compilationBuffer) appendFacet(start, end int, feature map[string]any) {
	b.facets = append(b.facets, facetValue(byteRange{start, end}, feature))
}

func (b *compilationBuffer) removeFacets(r byteRange) {
	kept := b.facets[:0]
	for _, facet := range b.facets {
		existing, ok := bufferByteRange(facet)
		if !ok || existing.overlaps(r) {
			continue
		}
		kept = append(kept, facet)
	}
	b.facets = kept
}

func (b *compilationBuffer) facetRanges() []byteRange {
	var ranges []byteRange
	for _, facet := range b.facets {
		if r, ok := bufferByteRange(facet); ok {
			ranges = append(ranges, r)
		}
	}
	return ranges
}

func (b *compilationBuffer) sortFacets() {
	sort.SliceStable(b.facets, func(i, j int) bool {
		return bufferLowerBound(b.facets[i]) < bufferLowerBound(b.facets[j])
	})
}

func bufferLowerBound(facet any) int {
	if r, ok := bufferByteRange(facet); ok {
		return r.start
	}
	return int(^uint(0) >> 1)
}

// bufferByteRange is CompilationBuffer.byteRange: numeric indexes truncated.
func bufferByteRange(facet any) (byteRange, bool) {
	object, ok := facet.(map[string]any)
	if !ok {
		return byteRange{}, false
	}
	index, ok := object["index"].(map[string]any)
	if !ok {
		return byteRange{}, false
	}
	start, ok1 := index["byteStart"].(float64)
	end, ok2 := index["byteEnd"].(float64)
	if !ok1 || !ok2 {
		return byteRange{}, false
	}
	return byteRange{int(start), int(end)}, true
}
