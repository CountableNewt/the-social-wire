package reader

import (
	"context"
	"time"
)

func readerQueryBudget(remote bool) time.Duration {
	if remote {
		// Remote checkout includes retained-session admission, verified foreign
		// keys and multiple HTTP round trips. Keep cold requests bounded without
		// treating that setup as a failed local SQLite query.
		return 5 * time.Second
	}
	return 2 * time.Second
}

func readerQueryContext(parent context.Context, remote bool) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, readerQueryBudget(remote))
}
