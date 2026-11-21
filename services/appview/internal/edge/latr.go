package edge

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/auth"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
)

// RegisterLatr registers the native authenticated L@tr bookmarks proxy when configured.
func RegisterLatr(group *httpx.Group, env config.Env, client *http.Client) {
	base := strings.TrimRight(env.String("LATR_IOS_PROXY_URL"), "/")
	if base == "" {
		return
	}
	official := env.String("LATR_IOS_PROXY_CLIENT_CREDENTIAL")
	id := env.String("LATR_IOS_PROXY_CLIENT_ID")
	key := env.String("LATR_IOS_PROXY_API_KEY")
	noRedirect := *client
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	proxy := func(w http.ResponseWriter, r *http.Request) error {
		if official == "" && (id == "" || key == "") {
			return httpx.Errorf(http.StatusServiceUnavailable, "Set LATR_IOS_PROXY_URL plus LATR_IOS_PROXY_CLIENT_ID and LATR_IOS_PROXY_API_KEY, or LATR_IOS_PROXY_CLIENT_CREDENTIAL for the official first-party credential.")
		}
		viewer := reqctx.AuthFrom(r.Context())
		if viewer == nil {
			return httpx.ErrUnauthorized
		}
		proof := strings.TrimSpace(r.Header.Get("X-Latr-Gateway-DPoP"))
		if proof == "" {
			return httpx.Errorf(http.StatusBadRequest, "Missing X-Latr-Gateway-DPoP")
		}
		target, err := url.Parse(base + r.URL.Path)
		if err != nil || target.Scheme != "https" || target.User != nil || target.Fragment != "" || target.Host == "" {
			return httpx.Status(http.StatusServiceUnavailable)
		}
		target.RawQuery = r.URL.RawQuery
		var body []byte
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			body, err = io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
			if err != nil {
				return httpx.Status(http.StatusRequestEntityTooLarge)
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		out, err := http.NewRequestWithContext(ctx, r.Method, target.String(), bytes.NewReader(body))
		if err != nil {
			return httpx.Status(http.StatusServiceUnavailable)
		}
		for _, header := range []string{"Accept", "Content-Type"} {
			if value := strings.TrimSpace(r.Header.Get(header)); value != "" {
				out.Header.Set(header, value)
			}
		}
		if out.Header.Get("Accept") == "" {
			out.Header.Set("Accept", "application/json")
		}
		if len(body) != 0 && out.Header.Get("Content-Type") == "" {
			out.Header.Set("Content-Type", "application/json")
		}
		out.Header.Set("Authorization", viewer.AuthorizationForwardingValue)
		out.Header.Set("DPoP", proof)
		if viewer.UpstreamDPoPProof != "" {
			out.Header.Set(auth.UpstreamHeader, viewer.UpstreamDPoPProof)
		}
		if official != "" {
			out.Header.Set("X-Latr-Official-Client", official)
		} else {
			out.Header.Set("X-Latr-Client-Id", id)
			out.Header.Set("X-Latr-API-Key", key)
		}
		reply, err := noRedirect.Do(out)
		if err != nil {
			return httpx.Errorf(http.StatusBadGateway, "L@tr gateway request failed.")
		}
		defer reply.Body.Close()
		const limit = 8 << 20
		payload, err := io.ReadAll(io.LimitReader(reply.Body, limit+1))
		if err != nil || len(payload) > limit {
			return httpx.Errorf(http.StatusBadGateway, "L@tr gateway request failed.")
		}
		media := reply.Header.Get("Content-Type")
		if media == "" {
			media = "application/json"
		}
		w.Header().Set("Content-Type", media)
		if nonce := reply.Header.Get("DPoP-Nonce"); nonce != "" {
			w.Header().Set("DPoP-Nonce", nonce)
		}
		w.WriteHeader(reply.StatusCode)
		_, err = w.Write(payload)
		return err
	}
	group.Get("/xrpc/link.latr.preview.getOpenGraph", proxy)
	for _, method := range []string{"listBookmarks", "listTags", "getBookmark"} {
		group.Get("/xrpc/link.latr.bookmarks."+method, proxy)
	}
	for _, method := range []string{"saveBookmark", "setTags", "renameTag", "deleteTag", "setState", "deleteBookmark", "migrateLegacy"} {
		group.Post("/xrpc/link.latr.bookmarks."+method, proxy)
	}
	group.Patch("/xrpc/link.latr.bookmarks.setState", proxy)
}
