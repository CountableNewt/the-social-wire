package atproto

import (
	"io"
	"net/http"
)

// HTTPDoer executes requests; *http.Client satisfies it (PDSHTTPTransport).
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// RateLimitedDoer waits for a limiter permit before each request
// (RateLimitedPDSHTTPTransport).
type RateLimitedDoer struct {
	Upstream HTTPDoer
	Limiter  *PDSRequestRateLimiter
}

// Do waits for a permit, honoring the request context, then forwards req.
func (d RateLimitedDoer) Do(req *http.Request) (*http.Response, error) {
	if err := d.Limiter.WaitForPermit(req.Context()); err != nil {
		return nil, err
	}
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	return d.Upstream.Do(req)
}

// readBounded reads at most limit bytes, failing with ErrBodyTooLarge beyond
// it (AsyncHTTPClient collect(upTo:)).
func readBounded(body io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrBodyTooLarge
	}
	return data, nil
}
