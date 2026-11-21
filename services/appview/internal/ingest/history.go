package ingest

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
)

type authorHistoryQueue struct {
	mu      sync.Mutex
	pending chan string
	active  map[string]bool
	retry   map[string]time.Time
}

func (q *authorHistoryQueue) init() {
	if q.pending == nil {
		q.pending = make(chan string, 32)
		q.active = map[string]bool{}
		q.retry = map[string]time.Time{}
	}
}

func (s *Service) scheduleAuthorHistory(did string) {
	q := &s.history
	q.mu.Lock()
	defer q.mu.Unlock()
	q.init()
	if q.active[did] || q.retry[did].After(time.Now()) {
		return
	}
	select {
	case q.pending <- did:
		q.active[did] = true
	default:
	}
}

// RunAuthorHistory consumes request-scoped work. Two workers match the
// standalone service's existing background concurrency.
func (s *Service) RunAuthorHistory(ctx context.Context, logFailure func(error)) {
	q := &s.history
	q.mu.Lock()
	q.init()
	q.mu.Unlock()
	var workers sync.WaitGroup
	for range 2 {
		workers.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case author := <-q.pending:
					s.runAuthorHistory(ctx, author, logFailure)
				}
			}
		})
	}
	workers.Wait()
}

func (s *Service) runAuthorHistory(ctx context.Context, author string, logFailure func(error)) {
	deadline, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	err := s.walkAuthorHistory(deadline, author)
	if err != nil && ctx.Err() == nil && logFailure != nil {
		logFailure(err)
	}
	retry := 6 * time.Hour
	if err != nil {
		retry = time.Minute
	}
	q := &s.history
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.active, author)
	now := time.Now()
	if len(q.retry) >= 512 {
		clear(q.retry)
	}
	q.retry[author] = now.Add(retry)
}

func (s *Service) walkAuthorHistory(ctx context.Context, author string) error {
	base, err := s.AuthorPDS.ResolvePDSBaseStrict(ctx, author, atproto.EndpointPublicHTTPS)
	if err != nil || base == "" {
		return errors.New("scoped PDS history endpoint unavailable")
	}
	budget := 1000
	for _, collection := range documentCollections {
		cursor := ""
		seen := map[string]bool{}
		for {
			records, next, err := s.loadAuthorPage(ctx, base, author, collection, cursor)
			if err != nil {
				return err
			}
			if len(records) > budget {
				_, err := s.indexAuthorPage(ctx, author, collection, records[:budget])
				return errors.Join(err, errors.New("scoped PDS history reached record limit"))
			}
			if _, err := s.indexAuthorPage(ctx, author, collection, records); err != nil {
				return err
			}
			budget -= len(records)
			if next == "" {
				break
			}
			if budget == 0 || seen[next] {
				return errors.New("scoped PDS history did not complete")
			}
			seen[next] = true
			cursor = next
		}
	}
	return nil
}
