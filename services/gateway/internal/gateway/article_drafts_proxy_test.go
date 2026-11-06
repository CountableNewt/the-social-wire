package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stygian-tech/the-social-wire/packages/go/gatewaycore"
)

const articleDraftPrefix = "/xrpc/app.thesocialwire.articleDraft."

func articleDraftRoutes() []Route {
	var result []Route
	for _, route := range Routes {
		if strings.HasPrefix(route.Path, articleDraftPrefix) {
			result = append(result, route)
		}
	}
	return result
}

func TestPrivateArticleDraftRoutesRequireOwnersAndNeverPublicCaches(t *testing.T) {
	routes := articleDraftRoutes()
	if len(routes) != 4 {
		t.Fatalf("want four article draft routes, got %d", len(routes))
	}
	for _, route := range routes {
		if route.Public || route.Optional || route.Service != "appview" || route.Target != route.Path || !isPrivateDraftPath(route.Path) {
			t.Fatalf("unsafe article draft route: %+v", route)
		}
		proxy := Proxy{Config: Config{AppViewURL: "http://unused.invalid"}}
		for _, owner := range []string{"", gatewaycore.AnonymousDiscoveryDID} {
			request := httptest.NewRequest(route.Method, route.Path, nil)
			if owner != "" {
				request = request.WithContext(gatewaycore.ContextWithAuth(request.Context(), gatewaycore.AuthContext{DID: owner}))
			}
			response := httptest.NewRecorder()
			proxy.Handler(route).ServeHTTP(response, request)
			if response.Code != 401 {
				t.Fatalf("%s: got %d", route.Path, response.Code)
			}
			if response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatalf("private error headers missing for %s", route.Path)
			}
		}
	}
}

func TestPrivateArticleDraftProxyBindsOwnerAndPreservesConflict(t *testing.T) {
	const secret = "article-draft-secret"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		err := gatewaycore.VerifyInternalHeaders(secret, request.Method, request.URL.Path, request.Header, time.Now())
		owner := request.Header.Get(gatewaycore.InternalDIDHeader)
		if err != nil || owner != "did:plc:owner" {
			t.Errorf("missing owner trust: %q %v", owner, err)
		}
		if request.URL.Path != articleDraftPrefix+"put" || request.Header.Get("Content-Type") != "application/json" {
			t.Error("incorrect upstream article request")
		}
		body, _ := io.ReadAll(request.Body)
		if string(body) != `{"expectedRevision":1}` {
			t.Errorf("body changed: %s", body)
		}
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Set("X-Content-Type-Options", "unsafe")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"Conflict"}`))
	}))
	defer upstream.Close()
	proxy := Proxy{Config: Config{AppViewURL: upstream.URL, AppViewSecret: secret}, Internal: upstream.Client()}
	request := httptest.NewRequest("POST", articleDraftPrefix+"put", strings.NewReader(`{"expectedRevision":1}`))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(gatewaycore.ContextWithAuth(request.Context(), gatewaycore.AuthContext{DID: "did:plc:owner"}))
	response := httptest.NewRecorder()
	for _, route := range articleDraftRoutes() {
		if route.Path == request.URL.Path {
			proxy.Handler(route).ServeHTTP(response, request)
		}
	}
	if response.Code != http.StatusConflict || response.Body.String() != `{"error":"Conflict"}` {
		t.Fatalf("conflict changed: %d %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("upstream public caching escaped private proxy")
	}
}

func TestPrivateArticleDraftProxyBoundsJSONBeforeForwarding(t *testing.T) {
	proxy := Proxy{Config: Config{AppViewURL: "http://unused.invalid"}}
	var route Route
	for _, candidate := range articleDraftRoutes() {
		if candidate.Path == articleDraftPrefix+"put" {
			route = candidate
		}
	}
	for _, test := range []struct {
		contentType, body string
		status            int
	}{
		{"text/plain", `{}`, 400},
		{"application/json", strings.Repeat("x", (2<<20)+1), 413},
	} {
		request := httptest.NewRequest("POST", route.Path, strings.NewReader(test.body))
		request.Header.Set("Content-Type", test.contentType)
		request = request.WithContext(gatewaycore.ContextWithAuth(request.Context(), gatewaycore.AuthContext{DID: "did:plc:owner"}))
		response := httptest.NewRecorder()
		proxy.Handler(route).ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("%s: got %d, want %d", test.contentType, response.Code, test.status)
		}
	}
}

func TestPrivateArticleDraftProxyResponseLimit(t *testing.T) {
	for _, test := range []struct {
		name, path string
		bytes      int
		status     int
	}{
		{"article list above social limit", articleDraftPrefix + "list", 9 << 20, 200},
		{"social limit unchanged", "/xrpc/app.thesocialwire.socialDraft.list", 9 << 20, 502},
		{"article list bounded", articleDraftPrefix + "list", (24 << 20) + 1, 502},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				_, _ = io.CopyN(w, strings.NewReader(strings.Repeat("x", test.bytes)), int64(test.bytes))
			}))
			defer upstream.Close()
			proxy := Proxy{Config: Config{AppViewURL: upstream.URL, AppViewSecret: "secret"}, Internal: upstream.Client()}
			request := httptest.NewRequest("GET", test.path, nil)
			request = request.WithContext(gatewaycore.ContextWithAuth(request.Context(), gatewaycore.AuthContext{DID: "did:plc:owner"}))
			response := httptest.NewRecorder()
			proxy.Handler(Route{Method: "GET", UpstreamMethod: "GET", Path: test.path, Service: "appview", Target: test.path}).ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("got %d, want %d", response.Code, test.status)
			}
			if test.status == 200 && response.Body.Len() != test.bytes {
				t.Fatalf("response truncated to %d bytes", response.Body.Len())
			}
		})
	}
}
