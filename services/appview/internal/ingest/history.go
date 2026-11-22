package ingest

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
)

func normalizeHistoryAuthor(raw string) string {
	did, err := atproto.NormalizeDID(raw)
	if err != nil || len(did) > 2048 || did == atproto.RSSAuthorDID || (!strings.HasPrefix(did, "did:plc:") && !strings.HasPrefix(did, "did:web:")) {
		return ""
	}
	return did
}

func (s *Service) scheduleAuthorHistory(ctx context.Context, did string) error {
	did = normalizeHistoryAuthor(did)
	if did == "" {
		return nil
	}
	return (historyQueue{s.HistoryDB}).enqueue(ctx, did, time.Now())
}

type AuthorHistoryRunResult struct {
	Claimed   bool `json:"claimed"`
	Completed bool `json:"completed"`
	Retried   bool `json:"retried"`
	Pages     int  `json:"pages"`
	Indexed   int  `json:"indexed"`
}

// RunAuthorHistoryOnce resumes one durable author checkpoint. Work is bounded,
// and only fully indexed pages advance the cursor, so process loss cannot skip data.
func (s *Service) RunAuthorHistoryOnce(ctx context.Context) (AuthorHistoryRunResult, error) {
	result := AuthorHistoryRunResult{}
	if s.AuthorPDS == nil {
		return result, nil
	}
	queue := historyQueue{s.HistoryDB}
	claim, err := queue.claim(ctx, time.Now())
	if err != nil || claim == nil {
		return result, err
	}
	result.Claimed = true
	work, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	base := ""
	if claim.collection == len(documentCollections) {
		result.Completed = true
	} else {
		base, err = s.AuthorPDS.ResolvePDSBaseStrict(work, claim.author, atproto.EndpointPublicHTTPS)
		if err == nil && base == "" {
			err = errors.New("scoped PDS history endpoint unavailable")
		}
	}
	if err == nil && !result.Completed {
		result.Pages, result.Indexed, result.Completed, err = walkHistoryClaim(work, queue, claim,
			func(ctx context.Context, collection, cursor string) ([]authorRecord, string, error) {
				return s.loadAuthorPage(ctx, base, claim.author, collection, cursor)
			},
			func(ctx context.Context, collection string, records []authorRecord) (int, error) {
				return s.indexAuthorPage(ctx, claim.author, collection, records)
			})
	}
	// A cancelled request still records bounded retry evidence without extending
	// indexing past the claim's work deadline. The lease remains authoritative.
	finish, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	retried, finishErr := queue.finish(finish, *claim, result.Completed, err != nil, time.Now())
	result.Retried = retried
	return result, errors.Join(err, finishErr)
}

type historyPageLoader func(context.Context, string, string) ([]authorRecord, string, error)
type historyPageIndexer func(context.Context, string, []authorRecord) (int, error)

func walkHistoryClaim(ctx context.Context, queue historyQueue, claim *historyClaim, load historyPageLoader, index historyPageIndexer) (pages, indexed int, completed bool, err error) {
	budget := 1000
	for claim.collection < len(documentCollections) {
		if err := ctx.Err(); err != nil {
			return pages, indexed, false, err
		}
		collection := documentCollections[claim.collection]
		pageLimit := 50
		if collection == "site.standard.document" {
			pageLimit = 10
		}
		// Never split a page: the PDS cursor addresses the entire returned page.
		if pages >= 25 || budget < pageLimit {
			return pages, indexed, false, nil
		}
		records, next, err := load(ctx, collection, claim.cursor)
		if err != nil {
			return pages, indexed, false, err
		}
		if len(records) > pageLimit || len(next) > 2048 {
			return pages, indexed, false, errors.New("invalid author history page")
		}
		if next != "" && next == claim.cursor {
			return pages, indexed, false, errHistoryCursorLoop
		}
		count, err := index(ctx, collection, records)
		indexed += count
		if err != nil {
			return pages, indexed, false, err
		}
		nextCollection := claim.collection
		if next == "" {
			nextCollection++
		}
		if err := queue.checkpoint(ctx, claim, nextCollection, next, time.Now()); err != nil {
			return pages, indexed, false, err
		}
		pages++
		budget -= len(records)
	}
	return pages, indexed, true, nil
}

// RunAuthorHistory is retained for a long-running server; serverless jobs invoke
// RunAuthorHistoryOnce directly and never depend on this process-local timer.
func (s *Service) RunAuthorHistory(ctx context.Context, logFailure func(error)) {
	var workers sync.WaitGroup
	for range 2 {
		workers.Go(func() {
			for ctx.Err() == nil {
				result, err := s.RunAuthorHistoryOnce(ctx)
				if err != nil && ctx.Err() == nil && logFailure != nil {
					logFailure(err)
				}
				if result.Claimed && err == nil {
					continue
				}
				timer := time.NewTimer(time.Second)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		})
	}
	workers.Wait()
}
