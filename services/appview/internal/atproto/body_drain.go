package atproto

import (
	"context"
	"io"
)

// MaximumDiscardBytes bounds DrainAndClose by default.
const MaximumDiscardBytes = 64 * 1024

// DrainAndClose consumes up to maximumBytes of a small response body so its
// connection can be reused, then closes it; larger or failing bodies are
// simply closed. Read errors are swallowed, but context cancellation is
// returned (HTTPResponseBodyDrain.drainOrCancel).
func DrainAndClose(ctx context.Context, body io.ReadCloser, maximumBytes int64) error {
	if body == nil {
		return ctx.Err()
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(body, max(0, maximumBytes)))
	_ = body.Close()
	return ctx.Err()
}
