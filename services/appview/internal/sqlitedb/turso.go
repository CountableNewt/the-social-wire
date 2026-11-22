package sqlitedb

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	turso "turso.tech/database/tursogo-serverless"
)

var ErrRemoteDatabase = errors.New("Turso database operation failed")

// OpenTurso opens the newer Turso engine over its official pure-Go HTTP driver.
// It does not create local files, enable WAL, migrate schemas or retry mutations.
func OpenTurso(ctx context.Context, endpoint, token string) (*DB, error) {
	return openTurso(ctx, endpoint, token, &http.Client{Timeout: 20 * time.Second})
}

func openTurso(ctx context.Context, endpoint, token string, client *http.Client) (*DB, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.ForceQuery || (parsed.Scheme != "turso" && parsed.Scheme != "https") ||
		strings.TrimSpace(token) != token || token == "" || strings.ContainsAny(token, "\r\n") {
		return nil, errors.New("Turso requires an HTTPS endpoint without credentials or parameters and a separate token")
	}
	parsed.Scheme, parsed.Path = "https", ""
	protectedClient := *client
	transport := protectedClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	protectedClient.Transport = &tursoTransport{origin: parsed.Host, next: transport}
	protectedClient.CheckRedirect = func(*http.Request, []*http.Request) error { return ErrRemoteDatabase }
	connector := turso.NewConnector(parsed.String(), token).WithHTTPClient(&protectedClient).WithSessionRetention()
	pool := sql.OpenDB(&tursoConnector{connector: connector})
	pool.SetMaxOpenConns(8)
	pool.SetMaxIdleConns(4)
	pool.SetConnMaxIdleTime(time.Minute)
	pool.SetConnMaxLifetime(5 * time.Minute)
	if err := pool.PingContext(ctx); err != nil {
		_ = pool.Close()
		return nil, remoteError(err)
	}
	return &DB{write: pool, read: pool, remote: true}, nil
}

// Keep credentials on the explicitly configured origin. Protocol routing that
// requires a different origin must be reviewed, never followed automatically.
type tursoTransport struct {
	origin string
	next   http.RoundTripper
}

func (t *tursoTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != "https" || !strings.EqualFold(request.URL.Host, t.origin) || request.URL.User != nil {
		return nil, ErrRemoteDatabase
	}
	return t.next.RoundTrip(request)
}

// Driver errors can contain SQL or server messages. Keep errors inspectable
// with errors.Is/As without putting those contents into normal logs/responses.
type tursoError struct{ cause error }

func (e *tursoError) Error() string { return ErrRemoteDatabase.Error() }
func (e *tursoError) Unwrap() error { return e.cause }

func remoteError(err error) error {
	if err == nil {
		return nil
	}
	var safe *tursoError
	if errors.As(err, &safe) {
		return err
	}
	return &tursoError{cause: err}
}
