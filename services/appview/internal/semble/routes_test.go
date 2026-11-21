package semble

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
)

func TestRoutesGuardAuthenticationAndInputs(t *testing.T) {
	router := httpx.NewRouter()
	register(router.Group(), Service{})
	for _, tc := range []struct {
		url, viewer, code string
		status            int
	}{
		{"/v1/semble/collections", "", "", 401},
		{"/v1/semble/collection", "", "", 401},
		{"/v1/semble/connections", "", "", 401},
		{"/v1/semble/collections?limit=0", "did:plc:viewer", "invalid_request", 400},
		{"/v1/semble/collection", "did:plc:viewer", "invalid_request", 400},
		{"/v1/semble/connections", "did:plc:viewer", "invalid_request", 400},
		{"/v1/semble/collection?collectionUri=at://did:plc:other/network.cosmik.collection/record", "did:plc:viewer", "collection_owner_mismatch", 403},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.url, nil)
		if tc.viewer != "" {
			req = req.WithContext(reqctx.WithAuth(req.Context(), &reqctx.Auth{DID: tc.viewer}))
		}
		reply := httptest.NewRecorder()
		router.ServeHTTP(reply, req)
		if reply.Code != tc.status {
			t.Fatalf("%s: got %d expected %d", tc.url, reply.Code, tc.status)
		}
		if tc.code != "" {
			var body Error
			if err := json.Unmarshal(reply.Body.Bytes(), &body); err != nil || body.Code != tc.code || body.Retryable {
				t.Fatalf("%s: invalid error %s (%v)", tc.url, reply.Body.String(), err)
			}
		}
	}
}
