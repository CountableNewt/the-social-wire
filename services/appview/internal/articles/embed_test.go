package articles

import (
	"context"
	"errors"
	"testing"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
)

func TestVerifiedArticleRequiresDeclaredPublicationURL(t *testing.T) {
	const article = "https://publisher.example/story"
	const did = "did:plc:publisher"
	const document = "at://" + did + "/site.standard.document/story"
	const publication = "at://" + did + "/site.standard.publication/blog"
	calls := 0
	service := &resolver{
		page: func(_ context.Context, target string) ([]byte, error) {
			calls++
			return []byte(`<html><head><meta property="at:canonical" content="` + document + `"></head></html>`), nil
		},
		record: func(_ context.Context, subject atproto.ATURI) (*atproto.RepoRecord, error) {
			record := &atproto.RepoRecord{URI: subject.String(), Value: map[string]any{"$type": subject.Collection}}
			if subject.Collection == "site.standard.document" {
				record.Value["site"] = publication
				record.Value["title"] = "Story"
				record.Value["path"] = "/story"
			} else {
				record.Value["url"] = "https://publisher.example"
				record.Value["name"] = "Publisher"
			}
			return record, nil
		},
		permits: make(chan struct{}, 1), cache: make(map[string]cachedEmbed),
	}
	result, err := service.resolve(t.Context(), article+"#section")
	if err != nil || result.ArticleURL != article || result.DocumentURI != document || result.PublicationURI != publication {
		t.Fatalf("unexpected resolution: %+v, %v", result, err)
	}
	if _, err := service.resolve(t.Context(), article); err != nil || calls != 1 {
		t.Fatalf("verified result was not cached: calls=%d err=%v", calls, err)
	}
	if _, err := service.resolve(t.Context(), "https://other.example/story"); !errors.Is(err, missingArticle) {
		t.Fatalf("unrelated page accepted: %v", err)
	}
}

func TestCanonicalRecordMustBeInExplicitHead(t *testing.T) {
	const marker = `<meta name="at:canonical" content="at://did:plc:author/site.standard.document/story">`
	for _, page := range []string{marker, "<body>" + marker, "<head><script>" + marker + "</script></head>"} {
		if uri, ok := headDocument([]byte(page)); ok {
			t.Fatalf("accepted %s outside head metadata", uri.String())
		}
	}
}
