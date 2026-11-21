package podcasts

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
)

func TestReadinessRequiresBothProcessorEndpoints(t *testing.T) {
	for _, healthy := range []bool{true, false} {
		t.Run(fmt.Sprint(healthy), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/startupz":
					fmt.Fprint(w, `{"ready":true}`)
				case "/healthz":
					if !healthy {
						w.WriteHeader(503)
					}
					fmt.Fprint(w, `{"status":"ok"}`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			err := CheckReadiness(context.Background(), server.URL)
			if healthy && err != nil || !healthy && httpx.StatusOf(err) != 503 {
				t.Fatalf("healthy = %v, readiness error = %v", healthy, err)
			}
		})
	}
}
