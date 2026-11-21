package atproto

import (
	"context"
	"io"
	"net"
	"net/http"
	"time"
)

// NewPinnedHTTPClient returns a client that dials address for every request
// (DNS override for the validated host), never follows redirects, ignores
// proxy environment variables, and bounds connect and response-header time.
// TLS still verifies the certificate against the request URL's host.
func NewPinnedHTTPClient(address string, connectTimeout, headerTimeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: connectTimeout}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(address, port))
		},
		TLSHandshakeTimeout:   connectTimeout,
		ResponseHeaderTimeout: headerTimeout,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          1,
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// closeIdleOnClose shuts a one-off pinned client down with its response body.
type closeIdleOnClose struct {
	io.ReadCloser
	client *http.Client
	cancel context.CancelFunc
}

func (c *closeIdleOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.client.CloseIdleConnections()
	if c.cancel != nil {
		c.cancel()
	}
	return err
}
