package repo

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// HTTPResponse is the bounded result of one snapshot GET.
type HTTPResponse struct {
	Status     int
	RetryAfter *string
	Body       []byte
}

// HTTPTransport performs a GET for the snapshot reader. The context carries the
// whole-snapshot deadline; implementations must stop promptly when it ends and
// must not return bodies longer than maximumBytes.
type HTTPTransport interface {
	Get(ctx context.Context, u *url.URL, accept string, maximumBytes int) (HTTPResponse, error)
}

// AddressValidator resolves and validates the address a public URL may be dialed at.
type AddressValidator func(ctx context.Context, rawURL string) (string, error)

// PublicHTTPTransport fetches from untrusted public HTTPS hosts: port 443 only,
// no credentials, no redirects, DNS pinned to a validated public address,
// 10s connect and 15s per-request bounds.
type PublicHTTPTransport struct {
	UserAgent string
	// ValidateAddress defaults to ValidatedAddress; tests may override it.
	ValidateAddress AddressValidator
	// Port defaults to 443; tests may override it to reach a local TLS server.
	Port string
	// TLSConfigure, when set, adjusts the per-request transport (tests only).
	TLSConfigure func(*http.Transport)
}

const (
	publicTransportRequestTimeout = 15 * time.Second
	publicTransportConnectTimeout = 10 * time.Second
)

// Get implements HTTPTransport.
func (t PublicHTTPTransport) Get(ctx context.Context, u *url.URL, accept string, maximumBytes int) (HTTPResponse, error) {
	if u == nil || u.Hostname() == "" || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return HTTPResponse{}, Invalid("invalid public URL")
	}
	requestCtx, cancel := context.WithTimeout(ctx, publicTransportRequestTimeout)
	defer cancel()
	validate := t.ValidateAddress
	if validate == nil {
		validate = ValidatedAddress
	}
	address, err := validate(requestCtx, u.String())
	if err != nil {
		return HTTPResponse{}, t.mapDeadline(ctx, requestCtx, err)
	}
	port := t.Port
	if port == "" {
		port = "443"
	}
	dialer := &net.Dialer{Timeout: publicTransportConnectTimeout}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, net.JoinHostPort(address, port))
		},
		TLSHandshakeTimeout:   publicTransportConnectTimeout,
		ResponseHeaderTimeout: publicTransportRequestTimeout,
		DisableCompression:    true,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          1,
	}
	if t.TLSConfigure != nil {
		t.TLSConfigure(transport)
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return HTTPResponse{}, err
	}
	request.Host = u.Host
	request.Header.Set("Accept", accept)
	if t.UserAgent != "" {
		request.Header.Set("User-Agent", t.UserAgent)
	}
	response, err := client.Do(request)
	if err != nil {
		return HTTPResponse{}, t.mapDeadline(ctx, requestCtx, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, int64(maximumBytes)+1))
	if err != nil {
		return HTTPResponse{}, t.mapDeadline(ctx, requestCtx, err)
	}
	if len(body) > maximumBytes {
		return HTTPResponse{}, Invalid("response body limit exceeded")
	}
	var retryAfter *string
	if values := response.Header.Values("Retry-After"); len(values) > 0 {
		retryAfter = &values[0]
	}
	return HTTPResponse{Status: response.StatusCode, RetryAfter: retryAfter, Body: body}, nil
}

// mapDeadline reports the per-request deadline distinctly from caller cancellation.
func (PublicHTTPTransport) mapDeadline(parent, request context.Context, err error) error {
	if parent.Err() == nil && errors.Is(request.Err(), context.DeadlineExceeded) {
		return Invalid("request deadline exceeded")
	}
	return err
}
