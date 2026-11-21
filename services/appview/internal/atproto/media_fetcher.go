package atproto

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// MediaFetcher performs bounded public HTTPS fetches with the target's DNS
// answer validated and pinned on every hop, redirects followed manually
// (PublicMediaFetcher).
type MediaFetcher struct {
	dns *DNSAddressValidator
}

// NewMediaFetcher returns a fetcher validating hosts with dns.
func NewMediaFetcher(dns *DNSAddressValidator) *MediaFetcher { return &MediaFetcher{dns: dns} }

// PublicMedia is the process-wide fetcher using PublicDNS.
var PublicMedia = NewMediaFetcher(PublicDNS)

var redirectStatuses = map[int]bool{301: true, 302: true, 303: true, 307: true, 308: true}

// publicHop parses a hop URL that must be https with a host and no userinfo.
func publicHop(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return nil, ErrPublicTargetInvalid
	}
	return parsed, nil
}

func resolveLocation(base *url.URL, location string) (string, bool) {
	next, err := base.Parse(location)
	if err != nil {
		return "", false
	}
	return next.String(), true
}

// Open performs one public HTTPS exchange with pinned DNS and no redirect
// following. The caller owns body bounds and must close the body, which also
// shuts down the one-off client.
func (f *MediaFetcher) Open(ctx context.Context, rawURL string, timeout time.Duration, headers map[string]string) (*http.Response, error) {
	if _, err := publicHop(rawURL); err != nil {
		return nil, err
	}
	address, err := f.dns.ValidatedAddress(ctx, rawURL, time.Time{})
	if err != nil {
		return nil, err
	}
	client := NewPinnedHTTPClient(address, 10*time.Second, timeout)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	for name, value := range headers {
		request.Header.Add(name, value)
	}
	response, err := client.Do(request)
	if err != nil {
		client.CloseIdleConnections()
		return nil, err
	}
	response.Body = &closeIdleOnClose{ReadCloser: response.Body, client: client}
	return response, nil
}

// Stream opens a long-lived media stream (optionally ranged), following up
// to six redirect hops with each hop's DNS revalidated. A redirect without a
// usable Location is returned as-is.
func (f *MediaFetcher) Stream(ctx context.Context, rawURL, rangeHeader string) (*http.Response, error) {
	current := rawURL
	for range 6 {
		parsed, err := publicHop(current)
		if err != nil {
			return nil, err
		}
		address, err := f.dns.ValidatedAddress(ctx, current, time.Time{})
		if err != nil {
			return nil, err
		}
		client := NewPinnedHTTPClient(address, 10*time.Second, 60*time.Second)
		hopCtx, cancel := context.WithTimeout(ctx, 6*time.Hour)
		request, err := http.NewRequestWithContext(hopCtx, http.MethodGet, current, nil)
		if err != nil {
			cancel()
			return nil, err
		}
		if rangeHeader != "" {
			request.Header.Set("Range", rangeHeader)
		}
		response, err := client.Do(request)
		if err != nil {
			cancel()
			client.CloseIdleConnections()
			return nil, err
		}
		if redirectStatuses[response.StatusCode] {
			if location := response.Header.Get("Location"); location != "" {
				if next, ok := resolveLocation(parsed, location); ok {
					response.Body.Close()
					cancel()
					client.CloseIdleConnections()
					current = next
					continue
				}
			}
		}
		response.Body = &closeIdleOnClose{ReadCloser: response.Body, client: client, cancel: cancel}
		return response, nil
	}
	return nil, ErrPublicTargetUnavailable
}

// Fingerprint HEADs the media (following redirects) and returns
// "url|etag|last-modified|content-length" for a 200 carrying an ETag or
// Last-Modified, else "".
func (f *MediaFetcher) Fingerprint(ctx context.Context, rawURL string) (string, error) {
	current := rawURL
	for range 6 {
		parsed, err := publicHop(current)
		if err != nil {
			return "", err
		}
		address, err := f.dns.ValidatedAddress(ctx, current, time.Time{})
		if err != nil {
			return "", err
		}
		client := NewPinnedHTTPClient(address, 10*time.Second, 10*time.Second)
		hopCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		request, err := http.NewRequestWithContext(hopCtx, http.MethodHead, current, nil)
		if err != nil {
			cancel()
			return "", err
		}
		response, err := client.Do(request)
		if err != nil {
			cancel()
			client.CloseIdleConnections()
			return "", err
		}
		response.Body.Close()
		cancel()
		client.CloseIdleConnections()
		if redirectStatuses[response.StatusCode] {
			if location := response.Header.Get("Location"); location != "" {
				if next, ok := resolveLocation(parsed, location); ok {
					current = next
					continue
				}
			}
		}
		etag := response.Header.Get("ETag")
		modified := response.Header.Get("Last-Modified")
		if response.StatusCode != http.StatusOK || (etag == "" && modified == "") {
			return "", nil
		}
		return strings.Join([]string{rawURL, etag, modified, response.Header.Get("Content-Length")}, "|"), nil
	}
	return "", nil
}

// FetchOptions bounds MediaFetcher.Fetch.
type FetchOptions struct {
	// MaximumBytes bounds the response body (required).
	MaximumBytes int64
	// ValidateURL, when set, must accept every hop URL.
	ValidateURL func(string) bool
	// Timeout is the single deadline for all hops including bodies; 0 means 25s.
	Timeout time.Duration
	// RequestHeaders are added to every hop in order.
	RequestHeaders [][2]string
	// RequiredContentType, when set, must equal the response media type.
	RequiredContentType string
	// AllowPartialResponse also accepts 206.
	AllowPartialResponse bool
	// MaximumRedirects is the redirect budget (0...10); nil means 5.
	MaximumRedirects *int
}

// Fetch downloads a public resource under one deadline, following redirects
// with URL policy and DNS pinning rechecked per hop. Non-200 (or 206 when
// allowed) statuses fail with ErrPublicTargetUnavailable.
func (f *MediaFetcher) Fetch(ctx context.Context, rawURL string, options FetchOptions) ([]byte, error) {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 25 * time.Second
	}
	redirects := 5
	if options.MaximumRedirects != nil {
		redirects = *options.MaximumRedirects
	}
	deadline := time.Now().Add(timeout)
	return fetchFollowingRedirects(rawURL, redirects, deadline, options.ValidateURL, func(current string) (fetchHop, error) {
		address, err := f.dns.ValidatedAddress(ctx, current, deadline)
		if err != nil {
			return fetchHop{}, err
		}
		if !time.Now().Before(deadline) {
			return fetchHop{}, ErrPublicTargetUnavailable
		}
		client := NewPinnedHTTPClient(address, 10*time.Second, time.Until(deadline))
		defer client.CloseIdleConnections()
		hopCtx, cancel := context.WithDeadline(ctx, deadline)
		defer cancel()
		request, err := http.NewRequestWithContext(hopCtx, http.MethodGet, current, nil)
		if err != nil {
			return fetchHop{}, err
		}
		for _, header := range options.RequestHeaders {
			request.Header.Add(header[0], header[1])
		}
		response, err := client.Do(request)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
				return fetchHop{}, ErrPublicTargetUnavailable
			}
			return fetchHop{}, err
		}
		defer response.Body.Close()
		if redirectStatuses[response.StatusCode] {
			if location := response.Header.Get("Location"); location != "" {
				return fetchHop{redirect: location, isRedirect: true}, nil
			}
		}
		if response.StatusCode != http.StatusOK && !(options.AllowPartialResponse && response.StatusCode == http.StatusPartialContent) {
			return fetchHop{}, ErrPublicTargetUnavailable
		}
		body, err := CollectBeforeDeadline(response, options.MaximumBytes, deadline, options.RequiredContentType)
		if err != nil {
			return fetchHop{}, err
		}
		return fetchHop{body: body}, nil
	})
}

// fetchHop is one exchange's outcome: a redirect Location or a body.
type fetchHop struct {
	redirect   string
	isRedirect bool
	body       []byte
}

// fetchFollowingRedirects keeps URL policy and the single deadline on every
// hop; the hop function separately validates and pins DNS.
func fetchFollowingRedirects(rawURL string, maximumRedirects int, deadline time.Time,
	validateURL func(string) bool, fetch func(string) (fetchHop, error)) ([]byte, error) {
	if maximumRedirects < 0 || maximumRedirects > 10 {
		return nil, ErrPublicTargetInvalid
	}
	current := rawURL
	for hop := 0; hop <= maximumRedirects; hop++ {
		if !time.Now().Before(deadline) {
			return nil, ErrPublicTargetUnavailable
		}
		if validateURL != nil && !validateURL(current) {
			return nil, ErrPublicTargetInvalid
		}
		parsed, err := publicHop(current)
		if err != nil {
			return nil, err
		}
		result, err := fetch(current)
		if err != nil {
			return nil, err
		}
		if !result.isRedirect {
			return result.body, nil
		}
		if hop >= maximumRedirects {
			return nil, ErrPublicTargetUnavailable
		}
		next, ok := resolveLocation(parsed, result.redirect)
		if !ok {
			return nil, ErrPublicTargetUnavailable
		}
		current = next
	}
	return nil, ErrPublicTargetUnavailable
}

// CollectBeforeDeadline reads at most maximumBytes of the body before
// deadline (the response-header timeout does not bound the body). A required
// content type must match the response's media type exactly.
func CollectBeforeDeadline(response *http.Response, maximumBytes int64, deadline time.Time, requiredContentType string) ([]byte, error) {
	if requiredContentType != "" {
		mediaType, _, _ := strings.Cut(strings.ToLower(response.Header.Get("Content-Type")), ";")
		if strings.TrimSpace(mediaType) != strings.ToLower(requiredContentType) {
			return nil, ErrPublicTargetUnavailable
		}
	}
	type outcome struct {
		data []byte
		err  error
	}
	done := make(chan outcome, 1)
	go func() {
		data, err := readBounded(response.Body, maximumBytes)
		done <- outcome{data, err}
	}()
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case result := <-done:
		if result.err != nil {
			if errors.Is(result.err, context.DeadlineExceeded) {
				return nil, ErrPublicTargetUnavailable
			}
			return nil, result.err
		}
		if !time.Now().Before(deadline) {
			return nil, ErrPublicTargetUnavailable
		}
		return result.data, nil
	case <-timer.C:
		response.Body.Close()
		return nil, ErrPublicTargetUnavailable
	}
}
