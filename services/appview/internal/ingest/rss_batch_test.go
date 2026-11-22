package ingest

import (
	"context"
	"errors"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
	"testing"
	"time"
)

type rssBatchStore struct {
	batchErr                    error
	batchCalls, individualCalls int
	contents                    []thinstore.IndexedContentItem
	meta                        thinstore.RssFeedFetchMetadata
}

func (s *rssBatchStore) FetchRssFeedMetadata(context.Context, string) (*thinstore.RssFeedFetchMetadata, error) {
	return nil, nil
}
func (s *rssBatchStore) StoreRssFeedMetadata(context.Context, thinstore.RssFeedFetchMetadata) error {
	s.individualCalls++
	return nil
}
func (s *rssBatchStore) UpsertContentItem(context.Context, thinstore.IndexedContentItem) error {
	s.individualCalls++
	return nil
}
func (s *rssBatchStore) StoreRSSFeed(_ context.Context, items []thinstore.IndexedContentItem, meta thinstore.RssFeedFetchMetadata) error {
	s.batchCalls++
	s.contents = items
	s.meta = meta
	return s.batchErr
}

func TestRSSRefreshUsesAtomicStoreWithoutReplayingFailures(t *testing.T) {
	store := &rssBatchStore{}
	service := Service{Store: store}
	items := []thinstore.IndexedContentItem{{URI: "legacy-rss-identity", CID: "existing-cid"}}
	meta := thinstore.RssFeedFetchMetadata{NormalizedFeedURL: "https://publisher.example/rss"}
	count, err := service.storeFeed(t.Context(), items, meta)
	if err != nil || count != 1 || store.batchCalls != 1 || store.individualCalls != 0 || store.contents[0].URI != items[0].URI || store.meta.NormalizedFeedURL != meta.NormalizedFeedURL {
		t.Fatal("atomic store path changed identity or replayed individual writes")
	}
	failure := errors.New("database batch failed")
	store.batchErr = failure
	count, err = service.storeFeed(t.Context(), items, meta)
	if count != 0 || !errors.Is(err, failure) || store.batchCalls != 2 || store.individualCalls != 0 {
		t.Fatal("failed batch was masked or replayed")
	}
}

type rssFailureStore struct {
	previous *thinstore.RssFeedFetchMetadata
	saved    *thinstore.RssFeedFetchMetadata
	saveErr  error
}

func (s *rssFailureStore) FetchRssFeedMetadata(context.Context, string) (*thinstore.RssFeedFetchMetadata, error) {
	return s.previous, nil
}
func (s *rssFailureStore) StoreRssFeedMetadata(_ context.Context, m thinstore.RssFeedFetchMetadata) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saved = &m
	return nil
}
func (s *rssFailureStore) UpsertContentItem(context.Context, thinstore.IndexedContentItem) error {
	return errors.New("unexpected content write")
}

func TestRSSSourceFailurePersistsBackoffButMetadataFailureRemainsInfrastructureError(t *testing.T) {
	tag := "prior-validator"
	store := &rssFailureStore{previous: &thinstore.RssFeedFetchMetadata{ETag: &tag, ConsecutiveErrorCount: 1}}
	service := Service{Store: store}
	before := time.Now()
	// PublicMedia rejects HTTP before opening any connection.
	_, err := service.RefreshFeed(t.Context(), "http://publisher.example/rss")
	var source *thinstore.RSSSourceFailure
	if !errors.As(err, &source) || store.saved == nil || store.saved.ConsecutiveErrorCount != 2 || store.saved.BackoffUntil == nil || store.saved.BackoffUntil.Before(before.Add(10*time.Minute)) || store.saved.ETag == nil || *store.saved.ETag != tag {
		t.Fatalf("durable backoff not preserved: %v %+v", err, store.saved)
	}
	failure := errors.New("metadata database unavailable")
	store.saveErr = failure
	store.saved = nil
	_, err = service.RefreshFeed(t.Context(), "http://publisher.example/rss")
	if !errors.Is(err, failure) || store.saved != nil {
		t.Fatalf("metadata failure masked: %v", err)
	}
}
