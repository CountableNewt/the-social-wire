package httpx

import (
	"errors"
	"net/http"
)

// Error is an HTTP failure with an optional client-visible message, mirroring
// Hummingbird's HTTPError: a message renders as {"error":{"message":"..."}},
// no message renders an empty body.
type Error struct {
	Status  int
	Message string
	Headers http.Header
	// Name overrides the XRPC error name derived from Status.
	Name string
	// Err is an optional cause kept for logs; it is never sent to clients.
	Err error
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return http.StatusText(e.Status) + ": " + e.Err.Error()
	}
	return http.StatusText(e.Status)
}

func (e *Error) Unwrap() error { return e.Err }

// Status builds an Error with no client-visible message.
func Status(status int) *Error { return &Error{Status: status} }

// Errorf builds an Error with a client-visible message.
func Errorf(status int, message string) *Error { return &Error{Status: status, Message: message} }

// Named builds an Error whose XRPC envelope uses name instead of the status default.
func Named(status int, name, message string) *Error {
	return &Error{Status: status, Name: name, Message: message}
}

// Common errors.
var (
	ErrBadRequest   = Status(http.StatusBadRequest)
	ErrUnauthorized = Status(http.StatusUnauthorized)
	ErrForbidden    = Status(http.StatusForbidden)
	ErrNotFound     = Status(http.StatusNotFound)
)

// StatusOf reports the HTTP status err maps to: an *Error's status, else 500.
func StatusOf(err error) int {
	var httpErr *Error
	if errors.As(err, &httpErr) {
		return httpErr.Status
	}
	return http.StatusInternalServerError
}

// XRPCErrorName maps a status to the AT Protocol error name XRPCErrorMiddleware used.
func XRPCErrorName(status int) string {
	switch status {
	case 400:
		return "InvalidRequest"
	case 401:
		return "AuthRequired"
	case 403:
		return "Forbidden"
	case 404:
		return "NotFound"
	case 410:
		return "CursorExpired"
	case 429:
		return "RateLimitExceeded"
	case 503:
		return "ServiceUnavailable"
	case 504:
		return "UpstreamTimeout"
	default:
		return "InternalServerError"
	}
}
