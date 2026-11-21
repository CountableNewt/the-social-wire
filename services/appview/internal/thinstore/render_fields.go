package thinstore

import (
	"encoding/json"
	"strings"
)

// ContentRenderFields is the Level-1 render payload stored in content_items.render_json.
type ContentRenderFields struct {
	Title       string  `json:"title"`
	PublishedAt string  `json:"publishedAt"`
	Summary     *string `json:"summary,omitempty"`
	// ThumbnailURL is the list thumbnail, when one was extracted.
	ThumbnailURL *string `json:"thumbnailUrl,omitempty"`
	ContentHTML  *string `json:"contentHtml,omitempty"`
	// ArticleURL is the canonical article URL for RSS rows (used to collapse guid/link duplicate keys).
	ArticleURL    *string  `json:"articleUrl,omitempty"`
	ContentLabels []string `json:"contentLabels,omitempty"`
}

type renderFieldsWire struct {
	Title         *string  `json:"title"`
	PublishedAt   *string  `json:"publishedAt"`
	Summary       *string  `json:"summary"`
	ThumbnailURL  *string  `json:"thumbnailUrl"`
	ContentHTML   *string  `json:"contentHtml"`
	ArticleURL    *string  `json:"articleUrl"`
	ContentLabels []string `json:"contentLabels"`
}

// DecodeRenderFields decodes render JSON the way JSONDecoder decodes
// ContentRenderFields: title and publishedAt are required strings.
func DecodeRenderFields(raw string) (*ContentRenderFields, bool) {
	var wire renderFieldsWire
	if err := json.Unmarshal([]byte(raw), &wire); err != nil {
		return nil, false
	}
	if wire.Title == nil || wire.PublishedAt == nil {
		return nil, false
	}
	return &ContentRenderFields{
		Title:         *wire.Title,
		PublishedAt:   *wire.PublishedAt,
		Summary:       wire.Summary,
		ThumbnailURL:  wire.ThumbnailURL,
		ContentHTML:   wire.ContentHTML,
		ArticleURL:    wire.ArticleURL,
		ContentLabels: wire.ContentLabels,
	}, true
}

// EncodedJSON renders the stored form: JSONEncoder with .sortedKeys, nil fields omitted.
func (r ContentRenderFields) EncodedJSON() string {
	var builder strings.Builder
	builder.WriteByte('{')
	first := true
	key := func(name string) {
		if !first {
			builder.WriteByte(',')
		}
		first = false
		swiftJSONString(&builder, name)
		builder.WriteByte(':')
	}
	optional := func(name string, value *string) {
		if value != nil {
			key(name)
			swiftJSONString(&builder, *value)
		}
	}
	optional("articleUrl", r.ArticleURL)
	optional("contentHtml", r.ContentHTML)
	if r.ContentLabels != nil {
		key("contentLabels")
		builder.WriteString(swiftJSONStringArray(r.ContentLabels))
	}
	key("publishedAt")
	swiftJSONString(&builder, r.PublishedAt)
	optional("summary", r.Summary)
	optional("thumbnailUrl", r.ThumbnailURL)
	key("title")
	swiftJSONString(&builder, r.Title)
	builder.WriteByte('}')
	return builder.String()
}
