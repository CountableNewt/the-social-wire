package readstate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"
)

// RecordGetter performs com.atproto.repo.getRecord against the repository's
// own resolved PDS and returns the raw response body. Implementations send no
// viewer credentials and return an error for unresolvable repositories and
// non-200 responses. The atproto module can satisfy it later; HTTPRecordGetter
// is the standalone implementation.
type RecordGetter interface {
	GetRecord(ctx context.Context, repoDID, collection, rkey string, cid *string) ([]byte, error)
}

// PDSResolver resolves a DID to its PDS base URL ("" when it has none).
type PDSResolver interface {
	ResolvePDS(ctx context.Context, did string) (string, error)
}

// FetchRecordFunc is the projector's record source.
type FetchRecordFunc func(ctx context.Context, viewerDID, collection, key string, cid *string) (FetchedRecord, error)

// LiveRecordFetcher is LivePDSReadStateRecordFetcher over a RecordGetter.
type LiveRecordFetcher struct {
	getter RecordGetter
}

// NewLiveRecordFetcher wraps getter.
func NewLiveRecordFetcher(getter RecordGetter) *LiveRecordFetcher {
	return &LiveRecordFetcher{getter: getter}
}

// Fetch reads one record and extracts its value for CID verification.
func (f *LiveRecordFetcher) Fetch(ctx context.Context, viewerDID, collection, key string, cid *string) (FetchedRecord, error) {
	body, err := f.getter.GetRecord(ctx, viewerDID, collection, key, cid)
	if err != nil {
		return FetchedRecord{}, err
	}
	return RecordFromEnvelope(body)
}

// RecordFromEnvelope extracts uri, cid and value from a getRecord response,
// re-serializing the value without escaping slashes so it does not grow.
func RecordFromEnvelope(data []byte) (FetchedRecord, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var envelope map[string]any
	if err := decoder.Decode(&envelope); err != nil || envelope == nil {
		return FetchedRecord{}, ErrInvalidRecord
	}
	uri, okURI := envelope["uri"].(string)
	cid, okCID := envelope["cid"].(string)
	record, okValue := envelope["value"].(map[string]any)
	if !okURI || !okCID || !okValue {
		return FetchedRecord{}, ErrInvalidRecord
	}
	encoded, err := marshalNoEscape(record)
	if err != nil {
		return FetchedRecord{}, err
	}
	return FetchedRecord{URI: uri, CID: cid, JSON: encoded}, nil
}

// errRecordTooLarge reports a getRecord body over the collection limit.
var errRecordTooLarge = errors.New("readstate: getRecord response exceeds limit")

// HTTPRecordGetter fetches records over HTTP with redirects disabled, so a
// public PDS response cannot redirect verification requests into the private
// network. Each request has a 15 second deadline.
type HTTPRecordGetter struct {
	client   *http.Client
	resolver PDSResolver
}

// NewHTTPRecordGetter copies client with redirects disallowed.
func NewHTTPRecordGetter(client *http.Client, resolver PDSResolver) *HTTPRecordGetter {
	copied := *client
	copied.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &HTTPRecordGetter{client: &copied, resolver: resolver}
}

// GetRecord implements RecordGetter.
func (g *HTTPRecordGetter) GetRecord(ctx context.Context, repoDID, collection, rkey string, cid *string) ([]byte, error) {
	base, err := g.resolver.ResolvePDS(ctx, repoDID)
	if err != nil {
		return nil, err
	}
	if base == "" {
		return nil, ErrIncompleteGeneration
	}
	endpoint, err := url.Parse(base + "/xrpc/com.atproto.repo.getRecord")
	if err != nil {
		return nil, ErrIncompleteGeneration
	}
	query := url.Values{}
	query.Set("repo", repoDID)
	query.Set("collection", collection)
	query.Set("rkey", rkey)
	if cid != nil {
		query.Set("cid", *cid)
	}
	endpoint.RawQuery = query.Encode()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, ErrInvalidReference
	}
	request.Header.Set("Accept", "application/json")
	response, err := g.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64*1024))
		return nil, ErrIncompleteGeneration
	}
	limit := int64(MaximumRecordBytes + 4096)
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errRecordTooLarge
	}
	return body, nil
}
