package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling"
	"golang.org/x/net/html"
)

type blobUpload func(context.Context, string, string, []byte) ([]byte, error)

func (s *Service) hydratePreviews(ctx context.Context, did string, record scheduling.Record) {
	if s.uploadBlob == nil {
		return
	}
	for _, raw := range record.Array("posts") {
		if ctx.Err() != nil {
			return
		}
		post, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		embed := scheduling.Record(scheduling.Record(post).Object("embed"))
		external := scheduling.Record(embed.Object("external"))
		if embed.Text("$type") != "app.bsky.embed.external" || external["thumb"] != nil || external.Text("uri") == "" {
			continue
		}
		if thumb := s.preview(ctx, did, external.Text("uri")); thumb != nil {
			external["thumb"] = thumb
		}
	}
}

func (s *Service) preview(ctx context.Context, did, target string) json.RawMessage {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	page, base, err := previewResource(ctx, target, "text/html", 256<<10)
	if err != nil {
		return nil
	}
	image := previewImage(page, base)
	if image == "" {
		return nil
	}
	body, _, err := previewResource(ctx, image, "image/*", 1_000_000)
	if err != nil {
		return nil
	}
	contentType := http.DetectContentType(body)
	switch contentType {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
	default:
		return nil
	}
	data, err := s.uploadBlob(ctx, did, contentType, body)
	if err != nil {
		return nil
	}
	var result struct {
		Blob json.RawMessage `json:"blob"`
	}
	var details struct {
		Type string            `json:"$type"`
		Size int               `json:"size"`
		Ref  map[string]string `json:"ref"`
	}
	if json.Unmarshal(data, &result) != nil || json.Unmarshal(result.Blob, &details) != nil ||
		details.Type != "blob" || details.Size != len(body) || details.Ref["$link"] == "" {
		return nil
	}
	return result.Blob
}

func previewResource(ctx context.Context, target, accept string, limit int64) ([]byte, *url.URL, error) {
	invalid := errors.New("scheduling preview resource unavailable")
	for range 4 {
		endpoint, err := url.Parse(target)
		if err != nil || endpoint.Scheme != "https" || endpoint.User != nil || endpoint.Hostname() == "" || len(target) > 8192 {
			return nil, nil, invalid
		}
		response, err := atproto.PublicMedia.Open(ctx, endpoint.String(), 5*time.Second, map[string]string{"Accept": accept})
		if err != nil {
			return nil, nil, err
		}
		switch response.StatusCode {
		case 301, 302, 303, 307, 308:
			next, err := response.Location()
			response.Body.Close()
			if err != nil {
				return nil, nil, invalid
			}
			target = next.String()
		case http.StatusOK:
			body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
			response.Body.Close()
			if err != nil || int64(len(body)) > limit {
				return nil, nil, invalid
			}
			return body, endpoint, nil
		default:
			response.Body.Close()
			return nil, nil, invalid
		}
	}
	return nil, nil, invalid
}

func previewImage(page []byte, base *url.URL) string {
	tokenizer := html.NewTokenizer(bytes.NewReader(page))
	for kind := tokenizer.Next(); kind != html.ErrorToken; kind = tokenizer.Next() {
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		token := tokenizer.Token()
		if token.Data == "body" {
			return ""
		}
		if token.Data != "meta" {
			continue
		}
		var property, content string
		for _, pair := range token.Attr {
			switch pair.Key {
			case "property", "name":
				property = strings.ToLower(pair.Val)
			case "content":
				content = strings.TrimSpace(pair.Val)
			}
		}
		if content == "" || (property != "og:image" && property != "og:image:secure_url" && property != "twitter:image") {
			continue
		}
		target, err := base.Parse(content)
		if err == nil && target.Scheme == "https" && target.User == nil && target.Hostname() != "" {
			return target.String()
		}
	}
	return ""
}
