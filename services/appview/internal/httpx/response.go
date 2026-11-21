package httpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
)

// Recorder tracks whether a response has started and with which status.
type Recorder struct {
	http.ResponseWriter
	status  int
	written bool
}

// Record wraps w, or returns w itself when it is already a Recorder.
func Record(w http.ResponseWriter) *Recorder {
	if recorder, ok := w.(*Recorder); ok {
		return recorder
	}
	return &Recorder{ResponseWriter: w}
}

// WriteHeader records status before forwarding it.
func (r *Recorder) WriteHeader(status int) {
	if r.written {
		return
	}
	r.status = status
	r.written = true
	r.ResponseWriter.WriteHeader(status)
}

func (r *Recorder) Write(p []byte) (int, error) {
	if !r.written {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(p)
}

// Status is the response status, or 0 before the header is written.
func (r *Recorder) Status() int { return r.status }

// Written reports whether the header has been sent.
func (r *Recorder) Written() bool { return r.written }

// Flush supports streaming (NDJSON) responses.
func (r *Recorder) Flush() {
	if !r.written {
		r.WriteHeader(http.StatusOK)
	}
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack forwards to the underlying writer when supported.
func (r *Recorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := r.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, errors.New("httpx: hijack unsupported")
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *Recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// WriteError renders err as Hummingbird did: an *Error with a message becomes
// {"error":{"message":"..."}}, anything else an empty body; non-*Error values
// are 500s and their text is logged, never sent.
func WriteError(w http.ResponseWriter, err error) {
	if recorder, ok := w.(*Recorder); ok && recorder.Written() {
		return
	}
	var httpErr *Error
	if !errors.As(err, &httpErr) {
		slog.Error("unhandled request error", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	for name, values := range httpErr.Headers {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	if httpErr.Status >= 500 && httpErr.Err != nil {
		slog.Error("request failed", "status", httpErr.Status, "error", httpErr.Err)
	}
	if httpErr.Message == "" {
		w.WriteHeader(httpErr.Status)
		return
	}
	body := map[string]map[string]string{"error": {"message": httpErr.Message}}
	_ = writeJSON(w, httpErr.Status, body)
}

// JSON writes v with status as application/json.
func JSON(w http.ResponseWriter, status int, v any) error {
	return writeJSON(w, status, v)
}

// OK writes v as a 200 application/json response.
func OK(w http.ResponseWriter, v any) error { return writeJSON(w, http.StatusOK, v) }

// Marshal encodes v like responses do: no HTML escaping, no trailing newline.
func Marshal(v any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}

func writeJSON(w http.ResponseWriter, status int, v any) error {
	data, err := Marshal(v)
	if err != nil {
		return err
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	}
	w.WriteHeader(status)
	_, err = w.Write(data)
	return err
}

// DefaultMaxBody matches Hummingbird's default request body collection limit.
const DefaultMaxBody = 2 << 20

// DecodeJSON decodes the request body into v, failing with 400 for malformed
// JSON and 413 when the body exceeds limit bytes (DefaultMaxBody when 0).
func DecodeJSON(r *http.Request, v any, limit int64) error {
	if limit <= 0 {
		limit = DefaultMaxBody
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return &Error{Status: http.StatusBadRequest, Err: err}
	}
	if int64(len(body)) > limit {
		return Status(http.StatusRequestEntityTooLarge)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return &Error{Status: http.StatusBadRequest, Message: "Invalid request body", Err: err}
	}
	return nil
}

// Query returns the first value of a query parameter, like Hummingbird's queryParameters.get.
func Query(r *http.Request, name string) string { return r.URL.Query().Get(name) }

// QueryAll returns every value of a query parameter, splitting comma-joined values
// only when split is true.
func QueryAll(r *http.Request, name string, split bool) []string {
	values := r.URL.Query()[name]
	if !split {
		return values
	}
	var out []string
	for _, value := range values {
		for part := range strings.SplitSeq(value, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}
