package richtext

import (
	"errors"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// ErrInvalidFacet is PostRecordValidationError.invalidFacet.
var ErrInvalidFacet = errors.New("richtext: invalid facet")

const maxTagLength = 64

// urlExpression is https?://[^\s<>"']+ with ICU's Unicode \s.
var urlExpression = regexp.MustCompile(`(?i)https?://[^\s\p{Z}\x{85}\x{0B}<>"']+`)

// Facets merges authored (compiler) facets, preserved explicit facets, and
// detected link and tag facets, returning nil when there are none.
// existing distinguishes an absent array (nil) from an empty one.
func Facets(text string, existing, authored []any, detectTextFacets bool) []any {
	shouldDetect := detectTextFacets && existing == nil
	var preserved []byteRange
	var facets []any
	for _, group := range [][]any{authored, existing} {
		for _, facet := range sortedByStart(group) {
			r, ok := facetByteRange(facet)
			if !ok || !isValidFacetRange(r, text) || !isValidNativeFacet(facet, r, text) || overlapsAny(preserved, r) {
				continue
			}
			facets = append(facets, facet)
			preserved = append(preserved, r)
		}
	}
	var linkRanges []byteRange
	if shouldDetect {
		for _, link := range detectedLinks(text) {
			if overlapsAny(preserved, link.r) {
				continue
			}
			facets = append(facets, facetValue(link.r, map[string]any{
				"$type": "app.bsky.richtext.facet#link",
				"uri":   link.url,
			}))
			linkRanges = append(linkRanges, link.r)
		}
		occupied := append(append([]byteRange{}, preserved...), linkRanges...)
		for _, tag := range detectedTags(text) {
			if overlapsAny(occupied, tag.r) {
				continue
			}
			facets = append(facets, facetValue(tag.r, map[string]any{
				"$type": "app.bsky.richtext.facet#tag",
				"tag":   tag.tag,
			}))
		}
	}
	facets = sortedByStart(facets)
	if len(facets) == 0 {
		return nil
	}
	return facets
}

// NormalizeEmbed gives an untyped {external:{uri}} embed its $type and
// default title/description.
func NormalizeEmbed(embed any) any {
	object, ok := embed.(map[string]any)
	if !ok {
		return embed
	}
	if _, typed := object["$type"]; typed {
		return embed
	}
	external, ok := object["external"].(map[string]any)
	if !ok {
		return embed
	}
	uri, ok := external["uri"].(string)
	if !ok {
		return embed
	}
	normalized := make(map[string]any, len(external)+2)
	for key, value := range external {
		normalized[key] = value
	}
	if _, present := normalized["title"]; !present {
		title := uri
		if parsed, err := url.Parse(uri); err == nil && parsed.Hostname() != "" {
			title = parsed.Hostname()
		}
		normalized["title"] = title
	}
	if _, present := normalized["description"]; !present {
		normalized["description"] = ""
	}
	return map[string]any{"$type": "app.bsky.embed.external", "external": normalized}
}

// CanonicalizeFeedPost canonicalizes a shadow app.bsky.feed.post record.
func CanonicalizeFeedPost(value any) any {
	post, ok := value.(map[string]any)
	if !ok {
		return value
	}
	text, ok := post["text"].(string)
	if !ok {
		return value
	}
	copied := make(map[string]any, len(post))
	for key, field := range post {
		copied[key] = field
	}
	_, hasExplicit := copied["facets"]
	existing, isArray := copied["facets"].([]any)
	if isArray && existing == nil {
		existing = []any{}
	}
	if facets := Facets(text, existing, nil, !hasExplicit); facets != nil {
		copied["facets"] = facets
	} else {
		delete(copied, "facets")
	}
	if embed := NormalizeEmbed(copied["embed"]); embed != nil {
		copied["embed"] = embed
	}
	return copied
}

// ValidateFacets rejects explicit facets that are malformed, overlap, or do not
// describe what the text shows.
func ValidateFacets(facets []any, text string) error {
	var ranges []byteRange
	for _, facet := range facets {
		r, ok := facetByteRange(facet)
		if !ok || !isValidFacetRange(r, text) || !isValidNativeFacet(facet, r, text) || overlapsAny(ranges, r) {
			return ErrInvalidFacet
		}
		ranges = append(ranges, r)
	}
	return nil
}

type detectedLink struct {
	url string
	r   byteRange
}

func detectedLinks(text string) []detectedLink {
	var links []detectedLink
	for _, match := range urlExpression.FindAllStringIndex(text, -1) {
		candidate := text[match[0]:match[1]]
		trimmed := trimTrailingPunctuation(candidate)
		if trimmed == "" {
			continue
		}
		scheme, _, ok := URLComponents(trimmed)
		scheme = strings.ToLower(scheme)
		if !ok || (scheme != "http" && scheme != "https") || !strings.Contains(trimmed, "//") {
			continue
		}
		links = append(links, detectedLink{url: trimmed, r: byteRange{match[0], match[0] + len(trimmed)}})
	}
	return links
}

type detectedTag struct {
	tag string
	r   byteRange
}

func detectedTags(text string) []detectedTag {
	var tags []detectedTag
	for _, capture := range tagCaptures(text) {
		group := text[capture.start:capture.end]
		markerWidth := firstClusterLength(group)
		body := trimTrailingTagPunctuation(group[markerWidth:])
		if body == "" || GraphemeCount(body) > maxTagLength {
			continue
		}
		tags = append(tags, detectedTag{tag: body, r: byteRange{capture.start, capture.start + markerWidth + len(body)}})
	}
	return tags
}

// trimTrailingTagPunctuation drops trailing Characters made only of punctuation.
func trimTrailingTagPunctuation(value string) string {
	for value != "" {
		clusters := Clusters(value)
		last := clusters[len(clusters)-1]
		if !allPunctuation(last) {
			break
		}
		value = value[:len(value)-len(last)]
	}
	return value
}

func allPunctuation(cluster string) bool {
	for _, r := range cluster {
		if !unicode.IsPunct(r) {
			return false
		}
	}
	return cluster != ""
}

func trimTrailingPunctuation(value string) string {
	for value != "" && strings.ContainsRune(".,!?;:", rune(value[len(value)-1])) {
		value = value[:len(value)-1]
	}
	pairs := [][2]byte{{'(', ')'}, {'[', ']'}, {'{', '}'}}
	changed := true
	for changed {
		changed = false
		for _, pair := range pairs {
			if value != "" && value[len(value)-1] == pair[1] &&
				strings.Count(value, string(pair[1])) > strings.Count(value, string(pair[0])) {
				value = value[:len(value)-1]
				changed = true
			}
		}
	}
	return value
}

func facetValue(r byteRange, feature map[string]any) any {
	return map[string]any{
		"index":    map[string]any{"byteStart": float64(r.start), "byteEnd": float64(r.end)},
		"features": []any{feature},
	}
}

func isValidMention(facet map[string]any, r byteRange, text string) bool {
	features, ok := facet["features"].([]any)
	if !ok {
		return false
	}
	displayed := text[r.start:r.end]
	clusters := Clusters(displayed)
	if len(clusters) < 2 || clusters[0] != "@" || len(features) == 0 {
		return false
	}
	for _, feature := range features {
		object, ok := feature.(map[string]any)
		if !ok {
			return false
		}
		if kind, _ := object["$type"].(string); kind != "app.bsky.richtext.facet#mention" {
			return false
		}
		did, ok := object["did"].(string)
		if !ok || !strings.HasPrefix(did, "did:") || GraphemeCount(did) <= 4 {
			return false
		}
	}
	return true
}

func isValidNativeFacet(facet any, r byteRange, text string) bool {
	object, ok := facet.(map[string]any)
	if !ok {
		return false
	}
	features, ok := object["features"].([]any)
	if !ok || len(features) != 1 {
		return false
	}
	feature, ok := features[0].(map[string]any)
	if !ok {
		return false
	}
	kind, ok := feature["$type"].(string)
	if !ok {
		return false
	}
	switch kind {
	case "app.bsky.richtext.facet#link":
		uri, ok := feature["uri"].(string)
		if !ok {
			return false
		}
		if !IsSafeHTTPURL(uri) || strings.TrimFunc(text[r.start:r.end], unicode.IsSpace) == "" {
			return false
		}
		for _, link := range detectedLinks(text) {
			if link.r.overlaps(r) && link.r != r {
				return false
			}
		}
		return true
	case "app.bsky.richtext.facet#mention":
		return isValidMention(object, r, text)
	case "app.bsky.richtext.facet#tag":
		tag, ok := feature["tag"].(string)
		if !ok {
			return false
		}
		displayed := text[r.start:r.end]
		if tag == "" || GraphemeCount(tag) > maxTagLength || (displayed != "#"+tag && displayed != "＃"+tag) {
			return false
		}
		for _, detected := range detectedTags(text) {
			if detected.r == r && detected.tag == tag {
				return true
			}
		}
		return false
	}
	return false
}

func isValidFacetRange(r byteRange, text string) bool {
	if r.start < 0 || r.start >= r.end || r.end > len(text) {
		return false
	}
	return IsBoundary(text, r.start) && IsBoundary(text, r.end)
}

// facetByteRange reads an integral index.byteStart/byteEnd pair.
func facetByteRange(facet any) (byteRange, bool) {
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
	if !ok1 || !ok2 || math.IsInf(start, 0) || math.IsNaN(start) || math.IsInf(end, 0) || math.IsNaN(end) ||
		math.Trunc(start) != start || math.Trunc(end) != end {
		return byteRange{}, false
	}
	return byteRange{int(start), int(end)}, true
}

func sortedByStart(facets []any) []any {
	sorted := append([]any(nil), facets...)
	sort.SliceStable(sorted, func(i, j int) bool { return lowerBound(sorted[i]) < lowerBound(sorted[j]) })
	return sorted
}

func lowerBound(facet any) int {
	if r, ok := facetByteRange(facet); ok {
		return r.start
	}
	return math.MaxInt
}

func overlapsAny(ranges []byteRange, r byteRange) bool {
	for _, existing := range ranges {
		if existing.overlaps(r) {
			return true
		}
	}
	return false
}
