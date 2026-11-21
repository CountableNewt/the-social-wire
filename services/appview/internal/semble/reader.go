package semble

import (
	"context"
	"encoding/json"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
	"sort"
	"strings"
	"sync"
)

type Record struct {
	Collection, URI string
	CID             *string
	Value           json.RawMessage
}
type RecordRead struct {
	Records  []Record
	Complete bool
}
type Reader interface {
	Read(context.Context, string, []string) (RecordRead, error)
}
type Repository interface {
	ListRecords(context.Context, *reqctx.Auth, atproto.ListRecordsParams) ([]atproto.RepoRecord, string, error)
}
type PublicReader struct{ Repo Repository }

func (r PublicReader) Read(ctx context.Context, did string, collections []string) (RecordRead, error) {
	out := RecordRead{[]Record{}, true}
	for _, collection := range collections {
		cursor := ""
		seen := map[string]bool{}
		for i := 0; i < 20; i++ {
			page, next, e := r.Repo.ListRecords(ctx, nil, atproto.ListRecordsParams{Repo: did, Collection: collection, Cursor: cursor, Limit: 100, RequireResolvedRepository: true})
			if e != nil {
				return RecordRead{}, e
			}
			if len(page) > 100 {
				out.Complete = false
				break
			}
			for _, record := range page {
				owner, recordCollection, _, ok := parseURI(record.URI)
				if !ok || owner != did || recordCollection != collection {
					out.Complete = false
					continue
				}
				b, e := json.Marshal(record.Value)
				if e != nil {
					continue
				}
				cid := record.CID
				var cp *string
				if cid != "" {
					cp = &cid
				}
				out.Records = append(out.Records, Record{collection, record.URI, cp, b})
			}
			if next == "" {
				break
			}
			if seen[next] || i == 19 {
				out.Complete = false
				break
			}
			seen[next] = true
			cursor = next
		}
	}
	return out, nil
}
func (s Service) records(ctx context.Context, owners map[string]bool, collections []string) RecordRead {
	authors := []string{}
	for did := range owners {
		if strings.HasPrefix(did, "did:") {
			authors = append(authors, did)
		}
	}
	sort.Strings(authors)
	out := RecordRead{[]Record{}, true}
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, 8)
	for _, did := range authors {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			mu.Lock()
			out.Complete = false
			mu.Unlock()
			wg.Wait()
			return out
		}
		wg.Add(1)
		go func(did string) {
			defer wg.Done()
			defer func() { <-slots }()
			read, e := s.Reader.Read(ctx, did, collections)
			mu.Lock()
			defer mu.Unlock()
			if e != nil {
				out.Complete = false
				return
			}
			out.Records = append(out.Records, read.Records...)
			out.Complete = out.Complete && read.Complete
		}(did)
	}
	wg.Wait()
	return out
}
