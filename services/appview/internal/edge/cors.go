package edge

import (
	"maps"
	"net/http"
	"strings"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/auth"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
)

var requestHeaders = strings.Join([]string{
	"Accept", "Authorization", "Content-Type", "Origin", "If-None-Match", "DPoP",
	"X-Request-ID", "traceparent", "Idempotency-Key", "Last-Event-ID", auth.UpstreamHeader,
	auth.SessionProofHeader, "X-Latr-Gateway-DPoP",
	auth.ReceiptHeader, auth.RequiredHeader, auth.PreparedHeader,
}, ", ")

var exposedHeaders = strings.Join([]string{
	auth.SessionNonceHeader, auth.ReceiptHeader, auth.RequiredHeader, auth.PreparedHeader,
}, ", ")

// CORS lets configured web origins present OAuth and read session nonce headers.
func CORS(core config.Core) httpx.Middleware {
	origins := make(map[string]struct{}, len(core.CORSAllowedOrigins)+5)
	maps.Copy(origins, core.CORSAllowedOrigins)
	if len(origins) == 0 && core.OAuthPublicOrigin != "" {
		origins[core.OAuthPublicOrigin] = struct{}{}
	}
	if core.OAuthOperationsOrigin != "" {
		origins[core.OAuthOperationsOrigin] = struct{}{}
	}
	if core.AppEnv == config.Local {
		for _, origin := range []string{"http://localhost:3000", "http://127.0.0.1:3000", "http://localhost:3001", "http://127.0.0.1:3001"} {
			origins[origin] = struct{}{}
		}
	}
	return func(next httpx.Handler) httpx.Handler {
		return func(w http.ResponseWriter, r *http.Request) error {
			w.Header().Add("Vary", "Origin")
			origin := r.Header.Get("Origin")
			if origin == "" {
				return next(w, r)
			}
			_, allowed := origins[origin]
			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Expose-Headers", exposedHeaders)
			}
			if r.Method != http.MethodOptions || r.Header.Get("Access-Control-Request-Method") == "" {
				return next(w, r)
			}
			if allowed {
				w.Header().Set("Access-Control-Allow-Headers", requestHeaders)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS")
				w.Header().Set("Access-Control-Max-Age", "3600")
			}
			w.WriteHeader(http.StatusNoContent)
			return nil
		}
	}
}
