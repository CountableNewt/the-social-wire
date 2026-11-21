package edge

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
)

type gifLimit struct {
	mu     sync.Mutex
	owners map[string]ownerAdmission
}

func (l *gifLimit) consume(did string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	state, found := l.owners[did]
	if !found {
		if len(l.owners) >= 20_000 {
			for key, value := range l.owners {
				if now.Sub(value.updated) > 2*time.Minute {
					delete(l.owners, key)
				}
			}
		}
		if len(l.owners) >= 20_000 {
			return false
		}
		state = ownerAdmission{requests: 60, updated: now}
	}
	state.requests = min(60, state.requests+max(0, now.Sub(state.updated).Seconds())*2)
	state.updated = now
	allowed := state.requests >= 1
	if allowed {
		state.requests--
	}
	l.owners[did] = state
	return allowed
}

// RegisterGIF registers provider discovery, posters, and private draft media transfers.
func RegisterGIF(group *httpx.Group, core config.Core, admission *Admission) {
	service := gifService{key: core.KlipyAPIKey}
	limit := &gifLimit{owners: make(map[string]ownerAdmission)}
	for _, route := range []string{"/v1/media/gifs", "/v1/media/gifs/thumbnail"} {
		group.Get(route, func(w http.ResponseWriter, r *http.Request) error {
			did := reqctx.ViewerDID(r.Context())
			if did == "" {
				return gifError(w, http.StatusUnauthorized, "Authentication required")
			}
			if !limit.consume(did, time.Now()) {
				return gifError(w, http.StatusTooManyRequests, "Too many discovery requests")
			}
			return service.search(w, r, strings.HasSuffix(route, "/thumbnail"))
		})
	}
	group.Get("/xrpc/app.thesocialwire.socialDraft.getGifFile", func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Cache-Control", "private, no-store")
		did := reqctx.ViewerDID(r.Context())
		if did == "" {
			return gifError(w, http.StatusUnauthorized, "Authentication required")
		}
		if !admission.acquire(did, true, time.Now()) {
			w.Header().Set("Retry-After", "1")
			return gifError(w, http.StatusTooManyRequests, "GIF transfer unavailable")
		}
		defer admission.release(did)
		query := r.URL.Query()
		id, rendition := query.Get("id"), query.Get("rendition")
		if len(query) != 2 || len(query["id"]) != 1 || len(query["rendition"]) != 1 || !gifID.MatchString(id) ||
			(rendition != "gif" && rendition != "poster" && rendition != "mp4" && rendition != "webm") {
			return gifError(w, http.StatusBadRequest, "Invalid GIF file selector")
		}
		if service.key == "" {
			return gifError(w, http.StatusServiceUnavailable, "GIF files unavailable")
		}
		return service.download(w, r, id, rendition)
	})
}

func (s gifService) search(w http.ResponseWriter, r *http.Request, thumbnail bool) error {
	if s.key == "" {
		return gifError(w, http.StatusServiceUnavailable, "GIF request unavailable")
	}
	query := r.URL.Query()
	id, text, cursor := query.Get("id"), strings.TrimSpace(query.Get("q")), query.Get("cursor")
	if len(text) > 200 || len(cursor) > 512 || (id != "" && !gifID.MatchString(id)) || (thumbnail && id == "") {
		return gifError(w, http.StatusBadRequest, "GIF request unavailable")
	}
	endpoint, parameters := "featured", url.Values{}
	if text != "" {
		endpoint = "search"
		parameters.Set("q", text)
	}
	if cursor != "" {
		parameters.Set("pos", cursor)
	}
	if id != "" {
		endpoint, parameters = "posts", url.Values{"ids": {id}}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	page, err := s.fetch(ctx, endpoint, parameters)
	if err != nil {
		return gifError(w, httpx.StatusOf(err), err.Error())
	}
	if id != "" {
		selected := make([]gifAsset, 0, 1)
		for _, asset := range page.Results {
			if asset.ID == id {
				selected = append(selected, asset)
			}
		}
		if len(selected) == 0 {
			return gifError(w, http.StatusNotFound, "GIF request unavailable")
		}
		page.Results = selected
	}
	w.Header().Set("Cache-Control", "no-store")
	if !thumbnail {
		return httpx.JSON(w, http.StatusOK, page)
	}
	reply, err := atproto.PublicMedia.Open(ctx, page.Results[0].Poster, 15*time.Second, nil)
	if err != nil {
		return gifError(w, http.StatusBadGateway, "GIF service unavailable")
	}
	defer reply.Body.Close()
	media, _, _ := mime.ParseMediaType(reply.Header.Get("Content-Type"))
	if reply.StatusCode != http.StatusOK || !allowedGIFType(media, "poster") {
		return gifError(w, http.StatusBadGateway, "GIF request unavailable")
	}
	const limit = 1_000_000
	content, err := io.ReadAll(io.LimitReader(reply.Body, limit+1))
	if err != nil || len(content) == 0 || len(content) > limit {
		return gifError(w, http.StatusBadGateway, "GIF service unavailable")
	}
	w.Header().Set("Content-Type", media)
	_, err = w.Write(content)
	return err
}

func (s gifService) download(w http.ResponseWriter, r *http.Request, id, rendition string) error {
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	controller := http.NewResponseController(w)
	if deadline, ok := ctx.Deadline(); ok {
		_ = controller.SetWriteDeadline(deadline)
		defer controller.SetWriteDeadline(time.Time{})
	}
	unavailable := "GIF file unavailable"
	page, err := s.fetch(ctx, "posts", url.Values{"ids": {id}})
	if err != nil {
		return gifError(w, httpx.StatusOf(err), unavailable)
	}
	raw := ""
	for _, asset := range page.Results {
		if asset.ID == id {
			raw = map[string]string{"gif": asset.URL, "poster": asset.Poster, "mp4": asset.MP4, "webm": asset.WebM}[rendition]
			break
		}
	}
	if raw == "" {
		return gifError(w, http.StatusNotFound, unavailable)
	}
	target, ok := gifMediaURL(raw)
	if !ok || target.ForceQuery || target.RawQuery != "" || target.EscapedPath() != target.Path ||
		target.Path == "/ii/" || path.Clean(target.Path) != target.Path {
		return gifError(w, http.StatusBadGateway, unavailable)
	}
	reply, err := atproto.PublicMedia.Open(ctx, raw, 120*time.Second, map[string]string{"Accept-Encoding": "identity"})
	if err != nil {
		return gifError(w, http.StatusBadGateway, unavailable)
	}
	defer reply.Body.Close()
	media, _, _ := mime.ParseMediaType(reply.Header.Get("Content-Type"))
	encoding := reply.Header.Get("Content-Encoding")
	if reply.StatusCode != http.StatusOK || !allowedGIFType(media, rendition) || (encoding != "" && encoding != "identity") {
		return gifError(w, http.StatusBadGateway, unavailable)
	}
	limit := int64(100 << 20)
	if rendition == "poster" {
		limit = 1_000_000
	}
	if reply.ContentLength > limit {
		return gifError(w, http.StatusRequestEntityTooLarge, unavailable)
	}
	file, err := os.CreateTemp("", "social-draft-gif-*")
	if err != nil {
		return gifError(w, http.StatusBadGateway, unavailable)
	}
	defer os.Remove(file.Name())
	defer file.Close()
	head := make([]byte, 512)
	count, err := io.ReadFull(reply.Body, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return gifError(w, http.StatusBadGateway, unavailable)
	}
	head = head[:count]
	if detectedGIFType(head) != media {
		return gifError(w, http.StatusBadGateway, unavailable)
	}
	size, err := io.Copy(file, io.LimitReader(io.MultiReader(bytes.NewReader(head), reply.Body), limit+1))
	if size > limit {
		return gifError(w, http.StatusRequestEntityTooLarge, unavailable)
	}
	if err != nil || ctx.Err() != nil || (reply.ContentLength >= 0 && size != reply.ContentLength) {
		return gifError(w, http.StatusBadGateway, unavailable)
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return gifError(w, http.StatusBadGateway, unavailable)
	}
	w.Header().Set("Content-Type", media)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, err = io.Copy(w, file)
	return err
}

func allowedGIFType(media, rendition string) bool {
	switch rendition {
	case "gif":
		return media == "image/gif"
	case "mp4":
		return media == "video/mp4"
	case "webm":
		return media == "video/webm"
	case "poster":
		return media == "image/jpeg" || media == "image/png" || media == "image/webp"
	default:
		return false
	}
}

func detectedGIFType(head []byte) string {
	switch {
	case bytes.HasPrefix(head, []byte("GIF87a")), bytes.HasPrefix(head, []byte("GIF89a")):
		return "image/gif"
	case bytes.HasPrefix(head, []byte{0xff, 0xd8, 0xff}):
		return "image/jpeg"
	case bytes.HasPrefix(head, []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}):
		return "image/png"
	case len(head) >= 14 && string(head[:4]) == "RIFF" && string(head[8:14]) == "WEBPVP":
		return "image/webp"
	case bytes.HasPrefix(head, []byte{0x1a, 0x45, 0xdf, 0xa3}):
		return "video/webm"
	case len(head) >= 12 && string(head[4:8]) == "ftyp":
		size := int(binary.BigEndian.Uint32(head[:4]))
		if size >= 12 && size <= len(head) && size%4 == 0 {
			for i := 8; i < size; i += 4 {
				if i != 12 && string(head[i:i+3]) == "mp4" {
					return "video/mp4"
				}
			}
		}
	}
	return ""
}

func gifError(w http.ResponseWriter, status int, message string) error {
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	return httpx.JSON(w, status, map[string]string{"error": message})
}
