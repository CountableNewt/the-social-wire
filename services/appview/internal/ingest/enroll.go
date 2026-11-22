package ingest

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

const subscriptionCollection = "app.skyreader.feed.subscription"

type Repo interface {
	ListAllRecords(context.Context, *reqctx.Auth, string, string, int, int) ([]atproto.RepoRecord, error)
}

type Store interface {
	FetchRssFeedMetadata(context.Context, string) (*thinstore.RssFeedFetchMetadata, error)
	StoreRssFeedMetadata(context.Context, thinstore.RssFeedFetchMetadata) error
	UpsertContentItem(context.Context, thinstore.IndexedContentItem) error
}

type Service struct {
	Repo             Repo
	Store            Store
	Media            *atproto.MediaFetcher
	AuthorPDS        AuthorResolver
	ContentRetention time.Duration
	MaximumRSSItems  int
	feedRefreshes    sync.Map
	HistoryDB        *sqlitedb.DB
}

// Enroll refreshes requested RSS feeds that belong to the authenticated viewer.
func (s *Service) Enroll(ctx context.Context, auth *reqctx.Auth, input thinstore.EnrollRequest) (int, error) {
	if auth == nil || auth.DID == "" {
		return 0, errors.New("RSS enrollment requires an authenticated viewer")
	}
	total, err := s.RefreshAuthors(ctx, input.AuthorDIDs)
	if err != nil || len(input.FeedURLs) == 0 {
		return total, err
	}
	rows, err := s.Repo.ListAllRecords(ctx, auth, auth.DID, subscriptionCollection, 100, 20)
	if err != nil {
		return total, err
	}
	allowed := make(map[string]bool, len(rows))
	for _, row := range rows {
		if raw, ok := row.Value["feedUrl"].(string); ok {
			if feed, ok := thinstore.NormalizeFeedURL(raw); ok {
				allowed[feed] = true
			}
		}
	}
	partial := &thinstore.EnrollmentPartialFailure{}
	for _, raw := range input.FeedURLs {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		feed := strings.TrimSpace(raw)
		if !allowed[feed] {
			feed, _ = thinstore.NormalizeFeedURL(feed)
		}
		if !allowed[feed] {
			continue
		}
		delete(allowed, feed)
		indexed, err := s.RefreshFeed(ctx, feed)
		total += indexed
		if err != nil {
			var source *thinstore.RSSSourceFailure
			if !errors.As(err, &source) {
				return total, err
			}
			partial.Failures = append(partial.Failures, source)
		}
	}
	if len(partial.Failures) > 0 {
		return total, partial
	}
	return total, nil
}
