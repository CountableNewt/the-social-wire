package repo

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func pinnedTestTransport(server *httptest.Server) PublicHTTPTransport {
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	return PublicHTTPTransport{
		UserAgent:       "tsw-test",
		ValidateAddress: func(context.Context, string) (string, error) { return "127.0.0.1", nil },
		Port:            port,
		TLSConfigure: func(transport *http.Transport) {
			transport.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
		},
	}
}

func TestPublicHTTPTransportPinsAddressAndDisallowsRedirects(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" || r.Header.Get("User-Agent") != "tsw-test" || r.Host != "example.com" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "https://example.com/ok", http.StatusFound)
		case "/limited":
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer server.Close()
	transport := pinnedTestTransport(server)
	get := func(path string, limit int) (HTTPResponse, error) {
		u, _ := url.Parse("https://example.com" + path)
		return transport.Get(context.Background(), u, "application/json", limit)
	}
	response, err := get("/ok", 1024)
	if err != nil || response.Status != 200 || string(response.Body) != `{"ok":true}` {
		t.Fatalf("response %+v err %v", response, err)
	}
	if response, err := get("/redirect", 1024); err != nil || response.Status != http.StatusFound {
		t.Fatalf("redirect response %+v err %v", response, err)
	}
	if response, err := get("/limited", 1024); err != nil || response.Status != 429 || response.RetryAfter == nil || *response.RetryAfter != "2" {
		t.Fatalf("limited response %+v err %v", response, err)
	}
	if _, err := get("/ok", 4); err == nil {
		t.Fatal("expected body limit failure")
	}
	for _, raw := range []string{"http://example.com/", "https://u:p@example.com/", "https://example.com:8443/"} {
		u, _ := url.Parse(raw)
		if _, err := transport.Get(context.Background(), u, "application/json", 1024); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
