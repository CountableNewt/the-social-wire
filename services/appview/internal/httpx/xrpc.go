package httpx

import (
	"errors"
	"net/http"
	"strings"
)

// XRPCErrors renders failures under /xrpc/ as the AT Protocol JSON error
// envelope {"error": Name, "message": ...}. Server errors never expose detail.
func XRPCErrors() Middleware {
	return func(next Handler) Handler {
		return func(w http.ResponseWriter, r *http.Request) error {
			err := next(w, r)
			if err == nil || !strings.HasPrefix(r.URL.Path, "/xrpc/") {
				return err
			}
			if recorder, ok := w.(*Recorder); ok && recorder.Written() {
				return nil
			}
			status := StatusOf(err)
			name := XRPCErrorName(status)
			message := "The request could not be completed."
			var httpErr *Error
			if errors.As(err, &httpErr) {
				if httpErr.Name != "" {
					name = httpErr.Name
				}
				for key, values := range httpErr.Headers {
					for _, value := range values {
						w.Header().Add(key, value)
					}
				}
			}
			if status < 500 {
				message = err.Error()
			}
			return JSON(w, status, map[string]string{"error": name, "message": message})
		}
	}
}
