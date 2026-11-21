package podcasts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
)

var readinessClient = &http.Client{
	Transport: &http.Transport{},
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// CheckReadiness requires both processor probes to succeed within a shared
// deadline. A loopback origin is required because the processor is in-process
// from the deployment's point of view.
func CheckReadiness(ctx context.Context, origin string) error {
	unavailable := httpx.Errorf(http.StatusServiceUnavailable, "Podcast media processor unavailable")
	origin, ok := loopbackOrigin(origin)
	if !ok {
		return unavailable
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	results := make(chan bool, 2)
	go func() {
		var body struct {
			Ready bool `json:"ready"`
		}
		results <- readProbe(ctx, origin+"/startupz", &body) && body.Ready
	}()
	go func() {
		var body struct {
			Status string `json:"status"`
		}
		results <- readProbe(ctx, origin+"/healthz", &body) && body.Status == "ok"
	}()

	for range 2 {
		if !<-results {
			return unavailable
		}
	}
	return nil
}

func loopbackOrigin(origin string) (string, bool) {
	base, err := url.Parse(origin)
	if err != nil || base.Scheme != "http" || base.User != nil ||
		base.RawQuery != "" || base.ForceQuery || base.Fragment != "" ||
		(base.EscapedPath() != "" && base.EscapedPath() != "/") {
		return "", false
	}
	switch base.Hostname() {
	case "127.0.0.1", "localhost", "::1":
	default:
		return "", false
	}

	return strings.TrimSuffix(origin, "/"), true
}

func readProbe(ctx context.Context, target string, result any) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return false
	}
	resp, err := readinessClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1025))
	return err == nil && len(body) <= 1024 && json.Unmarshal(body, result) == nil
}
