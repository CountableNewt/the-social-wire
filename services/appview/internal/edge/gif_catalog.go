package edge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
)

var gifID = regexp.MustCompile("^[a-zA-Z0-9_-]{1,128}$")

type gifAsset struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Poster      string `json:"poster"`
	MP4         string `json:"mp4,omitempty"`
	WebM        string `json:"webm,omitempty"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Attribution string `json:"attribution"`
}

type gifRendition struct {
	URL        string `json:"url"`
	Dimensions []int  `json:"dims"`
}

type gifProviderRecord struct {
	ID          string                  `json:"id"`
	Title       string                  `json:"title"`
	Description string                  `json:"content_description"`
	Media       map[string]gifRendition `json:"media_formats"`
}

type gifPage struct {
	Results []gifAsset `json:"results"`
	Next    string     `json:"next"`
}

func gifMediaURL(raw string) (*url.URL, bool) {
	target, err := url.Parse(raw)
	return target, err == nil && target.Scheme == "https" && target.Host == "static.klipy.com" &&
		target.User == nil && target.Fragment == "" && len(target.Path) >= 4 && target.Path[:4] == "/ii/"
}

func normalizeGIF(record gifProviderRecord) (gifAsset, bool) {
	gif := record.Media["gif"]
	dims := gif.Dimensions
	if !gifID.MatchString(record.ID) || len(dims) != 2 || dims[0] < 1 || dims[1] < 1 || dims[0] > 20_000 || dims[1] > 20_000 {
		return gifAsset{}, false
	}
	full, fullOK := gifMediaURL(gif.URL)
	poster, posterOK := gifMediaURL(record.Media["preview"].URL)
	if !fullOK || !posterOK {
		return gifAsset{}, false
	}
	asset := gifAsset{ID: record.ID, Title: record.Title, Description: record.Description, URL: full.String(),
		Poster: poster.String(), Width: dims[0], Height: dims[1], Attribution: "Powered by KLIPY"}
	for rendition, output := range map[string]*string{"mp4": &asset.MP4, "webm": &asset.WebM} {
		if video, ok := gifMediaURL(record.Media[rendition].URL); ok {
			if path.Dir(video.Path) != path.Dir(full.Path) {
				return gifAsset{}, false
			}
			*output = video.String()
		}
	}
	return asset, asset.MP4 != "" || asset.WebM != ""
}

type gifService struct{ key string }

func (s gifService) fetch(ctx context.Context, endpoint string, query url.Values) (gifPage, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	query.Set("key", s.key)
	query.Set("client_key", "social-wire")
	query.Set("contentfilter", "medium")
	query.Set("limit", "24")
	var page gifPage
	reply, err := atproto.PublicMedia.Open(ctx, "https://api.klipy.com/v2/"+endpoint+"?"+query.Encode(), 15*time.Second, nil)
	unavailable := httpx.Errorf(http.StatusBadGateway, "GIF service unavailable")
	if err != nil {
		return page, unavailable
	}
	defer reply.Body.Close()
	if reply.StatusCode == http.StatusTooManyRequests {
		return page, httpx.Errorf(http.StatusTooManyRequests, "GIF Search Rate Limited. Try Again Shortly.")
	}
	if reply.StatusCode != http.StatusOK {
		return page, unavailable
	}
	const limit = 2_000_000
	data, err := io.ReadAll(io.LimitReader(reply.Body, limit+1))
	var provider struct {
		Results []gifProviderRecord `json:"results"`
		Next    string              `json:"next"`
	}
	if err != nil || len(data) > limit || json.Unmarshal(data, &provider) != nil || len(provider.Results) > 100 || len(provider.Next) > 512 {
		return page, httpx.Errorf(http.StatusBadGateway, "Invalid GIF response")
	}
	page.Results = make([]gifAsset, 0, len(provider.Results))
	page.Next = provider.Next
	for _, record := range provider.Results {
		if asset, ok := normalizeGIF(record); ok {
			page.Results = append(page.Results, asset)
		}
	}
	return page, nil
}
