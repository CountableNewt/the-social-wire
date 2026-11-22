package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Services ingress must preserve the external host and path: a proof for one
// deployment or service endpoint cannot authorize another via client headers.
func TestServicesRoutingProofBindsActualRequest(t *testing.T) {
	private, public, thumbprint := key(t)
	now := time.Unix(1800000000, 0)
	const token = "routing-fixture-token"
	cases := []struct {
		name, requestURL, proofURL, proofMethod string
		headers                                 map[string]string
		wantValid                               bool
	}{
		{
			name: "deployment-local browser stream", requestURL: "https://isolated-preview.vercel.app/v1/appview/bootstrap-stream?limit=50",
			proofURL: "https://isolated-preview.vercel.app/v1/appview/bootstrap-stream", proofMethod: http.MethodGet, wantValid: true,
		},
		{
			name: "native API host", requestURL: "https://api.thesocialwire.app/v1/appview/bootstrap-stream",
			proofURL: "https://api.thesocialwire.app/v1/appview/bootstrap-stream", proofMethod: http.MethodGet, wantValid: true,
		},
		{
			name: "development API host", requestURL: "https://api.testing.thesocialwire.app/v1/appview/bootstrap-stream",
			proofURL: "https://api.testing.thesocialwire.app/v1/appview/bootstrap-stream", proofMethod: http.MethodGet, wantValid: true,
		},
		{
			name: "forwarded host cannot substitute Production", requestURL: "https://isolated-preview.vercel.app/v1/appview/bootstrap-stream",
			proofURL: "https://api.thesocialwire.app/v1/appview/bootstrap-stream", proofMethod: http.MethodGet,
			headers: map[string]string{"X-Forwarded-Host": "api.thesocialwire.app", "Forwarded": "host=api.thesocialwire.app;proto=https"},
		},
		{
			name: "original URL cannot substitute signed endpoint", requestURL: "https://isolated-preview.vercel.app/v1/read-state",
			proofURL: "https://api.thesocialwire.app/v1/appview/bootstrap-stream", proofMethod: http.MethodGet,
			headers: map[string]string{"X-Original-URL": "https://api.thesocialwire.app/v1/appview/bootstrap-stream", "X-Rewrite-URL": "/v1/appview/bootstrap-stream"},
		},
		{
			name: "original path cannot substitute signed path", requestURL: "https://isolated-preview.vercel.app/v1/read-state",
			proofURL: "https://isolated-preview.vercel.app/v1/appview/bootstrap-stream", proofMethod: http.MethodGet,
			headers: map[string]string{"X-Original-URL": "/v1/appview/bootstrap-stream", "X-Forwarded-Uri": "/v1/appview/bootstrap-stream"},
		},
		{
			name: "method cannot change between services", requestURL: "https://isolated-preview.vercel.app/v1/read-state",
			proofURL: "https://isolated-preview.vercel.app/v1/read-state", proofMethod: http.MethodPost,
		},
		{
			name: "irrelevant forwarded headers do not reject correct proof", requestURL: "https://isolated-preview.vercel.app/v1/appview/bootstrap-stream",
			proofURL: "https://isolated-preview.vercel.app/v1/appview/bootstrap-stream", proofMethod: http.MethodGet, wantValid: true,
			headers: map[string]string{"X-Forwarded-Host": "api.thesocialwire.app", "X-Original-URL": "/other"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.requestURL, nil)
			for name, value := range tc.headers {
				req.Header.Set(name, value)
			}
			proof := signed(t, private, map[string]any{"typ": "dpop+jwt", "alg": "ES256", "jwk": public},
				proofClaims{ID: tc.name, IssuedAt: now.Unix(), Method: tc.proofMethod, URL: tc.proofURL, TokenHash: digestString(token)})
			_, err := verifyProof(proof, req.Method, requestTarget(req), token, thumbprint, now)
			if (err == nil) != tc.wantValid {
				t.Fatalf("proof validity = %v, want %v (actual target %q)", err == nil, tc.wantValid, requestTarget(req))
			}
		})
	}
}
