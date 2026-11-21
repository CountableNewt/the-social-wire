package edge

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/auth"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
)

func TestMetadataAndCORS(t *testing.T) {
	router := httpx.NewRouter()
	core := config.Core{OAuthPublicOrigin: "https://web.social", OAuthIOSMetadataOrigin: "https://api.ios.social", AppEnv: config.Prod}
	router.Use(CORS(core))
	RegisterMetadata(router, core)
	for _, tc := range []struct{ path, clientID, redirect string }{
		{webPath, "https://gateway.social/oauth-client-metadata.json", "https://web.social/callback"},
		{nativePath, "https://api.ios.social/ios-client-metadata.json", "social.ios.api:/oauth/callback"},
	} {
		req := httptest.NewRequest(http.MethodGet, "https://gateway.social"+tc.path, nil)
		reply := httptest.NewRecorder()
		router.ServeHTTP(reply, req)
		body := reply.Body.String()
		if reply.Code != http.StatusOK || !strings.Contains(body, tc.clientID) || !strings.Contains(body, tc.redirect) ||
			strings.Contains(body, "podcast.clip") {
			t.Errorf("wrong metadata for %s: %d %s", tc.path, reply.Code, body)
		}
	}
	req := httptest.NewRequest(http.MethodOptions, "https://gateway.social/v1/read", nil)
	req.Header.Set("Origin", "https://web.social")
	req.Header.Set("Access-Control-Request-Method", "GET")
	reply := httptest.NewRecorder()
	router.ServeHTTP(reply, req)
	if reply.Code != http.StatusNoContent || reply.Header().Get("Access-Control-Allow-Origin") != "https://web.social" ||
		!strings.Contains(reply.Header().Get("Access-Control-Expose-Headers"), auth.SessionNonceHeader) ||
		!strings.Contains(reply.Header().Get("Access-Control-Allow-Headers"), auth.PreparedHeader) {
		t.Errorf("wrong OAuth preflight: %d %+v", reply.Code, reply.Header())
	}
}

func TestAdmissionReleasesPerViewerTransfer(t *testing.T) {
	admission := NewAdmission(config.Core{})
	now := time.Now()
	if !admission.acquire("owner", true, now) || !admission.acquire("owner", true, now) ||
		admission.acquire("owner", true, now) || !admission.acquire("another", true, now) {
		t.Fatal("draft transfer concurrency limits were not isolated by viewer")
	}
	admission.release("owner")
	if !admission.acquire("owner", true, now) {
		t.Fatal("finished transfer did not release its lease")
	}
}

func TestGIFProviderAllowsOnlyMatchedStaticRenditions(t *testing.T) {
	record := gifProviderRecord{ID: "sample", Media: map[string]gifRendition{
		"gif":     {URL: "https://static.klipy.com/ii/media/full.gif", Dimensions: []int{240, 120}},
		"preview": {URL: "https://static.klipy.com/ii/media/poster.webp"},
		"mp4":     {URL: "https://static.klipy.com/ii/media/full.mp4"},
	}}
	if asset, ok := normalizeGIF(record); !ok || asset.Width != 240 || asset.MP4 == "" {
		t.Fatalf("valid GIF rejected: %+v", asset)
	}
	for _, unsafe := range []string{"https://static.klipy.com/ii/another/full.mp4", "https://static.klipy.com:443/ii/media/full.mp4", "https://different.social/ii/media/full.mp4"} {
		record.Media["mp4"] = gifRendition{URL: unsafe}
		if _, ok := normalizeGIF(record); ok {
			t.Errorf("accepted unsafe or mismatched rendition: %s", unsafe)
		}
	}
}

func TestLatrUsesLatrProofAndPreservesMethodAndNonce(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/xrpc/link.latr.bookmarks.setState" || r.URL.RawQuery != "a=b%2Bc" {
			t.Errorf("unexpected upstream request %s %s", r.Method, r.URL.String())
		}
		for name, want := range map[string]string{
			"Authorization": "DPoP access-token", "DPoP": "latr-proof", auth.UpstreamHeader: "pds-proof",
			"X-Latr-Official-Client": "credential", "Content-Type": "application/json",
		} {
			if got := r.Header.Get(name); got != want {
				t.Errorf("upstream header %s: %q, want %q", name, got, want)
			}
		}
		w.Header().Set("DPoP-Nonce", "latr-nonce")
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer upstream.Close()
	router := httpx.NewRouter()
	group := router.Group(func(next httpx.Handler) httpx.Handler {
		return func(w http.ResponseWriter, r *http.Request) error {
			viewer := &reqctx.Auth{DID: "did:plc:viewer", AuthorizationForwardingValue: "DPoP access-token", DPoPProof: "gateway-proof", UpstreamDPoPProof: "pds-proof"}
			return next(w, r.WithContext(reqctx.WithAuth(r.Context(), viewer)))
		}
	})
	RegisterLatr(group, config.Env{"LATR_IOS_PROXY_URL": upstream.URL, "LATR_IOS_PROXY_CLIENT_CREDENTIAL": "credential"}, upstream.Client())
	req := httptest.NewRequest(http.MethodPatch, "/xrpc/link.latr.bookmarks.setState?a=b%2Bc", strings.NewReader(`{"state":"archived"}`))
	req.Header.Set("X-Latr-Gateway-DPoP", "latr-proof")
	reply := httptest.NewRecorder()
	router.ServeHTTP(reply, req)
	if reply.Code != http.StatusUnauthorized || reply.Header().Get("DPoP-Nonce") != "latr-nonce" {
		t.Errorf("wrong proxied nonce response: %d %+v", reply.Code, reply.Header())
	}
}
