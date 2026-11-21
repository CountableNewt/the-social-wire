package oauthclient

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
)

const responseLimit = 8 << 20

// XRPCError exposes only the status and recognized upstream error names.
type XRPCError struct {
	Status     int
	Code       string
	RetryAfter string
}

func (e *XRPCError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("scheduling upstream returned HTTP %d", e.Status)
	}
	return fmt.Sprintf("scheduling upstream returned HTTP %d (%s)", e.Status, e.Code)
}

func IsAuthFailure(err error) bool {
	var upstream *XRPCError
	return errors.Is(err, ErrNotConfigured) ||
		(errors.As(err, &upstream) && (upstream.Status == http.StatusUnauthorized || upstream.Status == http.StatusForbidden ||
			(upstream.Status == http.StatusBadRequest && upstream.Code == "invalid_grant")))
}

func sanitizedCode(body []byte) string {
	if len(body) > 64<<10 {
		return ""
	}
	var response struct {
		Code string `json:"error"`
	}
	if json.Unmarshal(body, &response) != nil {
		return ""
	}
	switch response.Code {
	case "invalid_grant", "invalid_client", "invalid_client_metadata", "invalid_scope", "use_dpop_nonce",
		"RecordNotFound", "InvalidRequest", "ExpiredToken", "AuthenticationRequired", "InvalidToken",
		"RecordAlreadyExists", "InvalidSwap", "RateLimitExceeded":
		return response.Code
	}
	return ""
}

type response struct {
	body  []byte
	nonce string
}

func (c *Client) do(ctx context.Context, method string, target *url.URL, body []byte, contentType string, private *ecdsa.PrivateKey, token, nonce string) (response, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, target.String(), bytes.NewReader(body))
	if err != nil {
		return response{}, errors.New("could not construct scheduling upstream request")
	}
	request.Header.Set("Accept", "application/json")
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if private != nil {
		signed, err := proof(private, method, target, token, nonce)
		if err != nil {
			return response{}, err
		}
		request.Header.Set("DPoP", signed)
	}
	if token != "" {
		request.Header.Set("Authorization", "DPoP "+token)
	}
	reply, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return response{}, ctx.Err()
		}
		return response{}, errors.New("scheduling upstream request unavailable")
	}
	defer reply.Body.Close()
	result := response{nonce: boundedHeader(reply.Header.Get("DPoP-Nonce"))}
	if reply.StatusCode < 200 || reply.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(reply.Body, (64<<10)+1))
		return result, &XRPCError{
			Status: reply.StatusCode, Code: sanitizedCode(data), RetryAfter: boundedHeader(reply.Header.Get("Retry-After")),
		}
	}
	data, err := io.ReadAll(io.LimitReader(reply.Body, responseLimit+1))
	if err != nil || len(data) > responseLimit {
		return result, errors.New("invalid scheduling upstream response")
	}
	result.body = data
	return result, nil
}

func boundedHeader(raw string) string {
	value := strings.TrimSpace(raw)
	if len(value) > 2048 || strings.ContainsAny(value, "\r\n") {
		return ""
	}
	return value
}

func (c *Client) signedWithNonceRetry(ctx context.Context, method string, target *url.URL, body []byte, contentType string, key *ecdsa.PrivateKey, token, nonce string) (response, error) {
	reply, err := c.do(ctx, method, target, body, contentType, key, token, nonce)
	var status *XRPCError
	if errors.As(err, &status) && reply.nonce != "" &&
		(status.Status == http.StatusBadRequest || status.Status == http.StatusUnauthorized) &&
		(status.Code == "" || status.Code == "use_dpop_nonce") {
		challenge := reply.nonce
		reply, err = c.do(ctx, method, target, body, contentType, key, token, challenge)
		if reply.nonce == "" {
			reply.nonce = challenge
		}
	}
	return reply, err
}

// publicTransport is used when a caller does not supply the application transport.
type publicTransport struct{}

func (publicTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	address, err := atproto.PublicDNS.ValidatedAddress(r.Context(), r.URL.String(), time.Now().Add(5*time.Second))
	if err != nil {
		return nil, err
	}
	client := atproto.NewPinnedHTTPClient(address, 5*time.Second, 10*time.Second)
	transport := client.Transport.(*http.Transport)
	transport.DisableKeepAlives = true
	return transport.RoundTrip(r)
}
