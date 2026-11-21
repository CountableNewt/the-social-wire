package atproto

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
)

const (
	repoRequestTimeout = 25 * time.Second
	repoBodyLimit      = 1024 * 1024
)

// RepoRecord is one record read from a repo. CID "" means absent. Value is
// decoded with encoding/json defaults (numbers are float64).
type RepoRecord struct {
	URI   string
	CID   string
	Value map[string]any
}

// RepoClient is the XRPC client for public author repos and the
// authenticated viewer's PDS (ATProtoAuthenticatedRepoClient). Reads accept a
// nil auth for public access; with auth they forward the viewer's
// Authorization header and PDS-bound DPoP proof. Writes always target the
// viewer's own repo (auth.DID).
type RepoClient struct {
	http     *http.Client
	resolver *Resolver
	logger   *slog.Logger
}

// NewRepoClient builds a client on the shared HTTP client and PLC base URL,
// using SharedPDSResolutionCache. A nil logger uses slog.Default().
func NewRepoClient(httpClient *http.Client, plcURL string, logger *slog.Logger) *RepoClient {
	return NewRepoClientWithResolver(httpClient, NewResolver(httpClient, plcURL), logger)
}

// NewRepoClientWithResolver builds a client around an existing resolver.
func NewRepoClientWithResolver(httpClient *http.Client, resolver *Resolver, logger *slog.Logger) *RepoClient {
	if logger == nil {
		logger = slog.Default()
	}
	return &RepoClient{http: httpClient, resolver: resolver, logger: logger}
}

// Resolver exposes the client's identity resolver.
func (c *RepoClient) Resolver() *Resolver { return c.resolver }

// HTTPClient exposes the shared HTTP client.
func (c *RepoClient) HTTPClient() *http.Client { return c.http }

// ListRecordsParams selects a listRecords page.
type ListRecordsParams struct {
	// Repo is a DID or handle.
	Repo       string
	Collection string
	// Limit is clamped to 1...100; 0 means 50.
	Limit  int
	Cursor string
	// Forward requests oldest-first; the default (false) is reverse=true.
	Forward bool
	// RequireResolvedRepository turns resolution failures into
	// ErrRepoUnavailable and invalid pages into ErrRepoMalformedResponse
	// instead of an empty page.
	RequireResolvedRepository bool
}

// ListRecords fetches one page, returning the records and next cursor ("" at
// the end). Unresolvable repos and non-200 responses yield an empty page.
// Bridgy relay hosts reject reverse=true, so for them the page is sorted
// newest-first locally.
func (c *RepoClient) ListRecords(ctx context.Context, auth *reqctx.Auth, params ListRecordsParams) ([]RepoRecord, string, error) {
	unavailable := func() ([]RepoRecord, string, error) {
		if params.RequireResolvedRepository {
			return nil, "", ErrRepoUnavailable
		}
		return []RepoRecord{}, "", nil
	}
	repoDID, err := c.resolver.ResolveRepoDID(ctx, params.Repo)
	if err != nil {
		return nil, "", err
	}
	if repoDID == "" {
		return unavailable()
	}
	pdsBase, err := c.resolver.ResolvePDSBase(ctx, repoDID)
	if err != nil {
		return nil, "", err
	}
	if pdsBase == "" {
		return unavailable()
	}

	reverse := !params.Forward
	serverReverse := reverse && !RelayHostOmitsListRecordsReverse(pdsBase)
	sortNewestFirst := reverse && !serverReverse
	limit := params.Limit
	if limit == 0 {
		limit = 50
	}
	query := url.Values{
		"repo":       {repoDID},
		"collection": {params.Collection},
		"limit":      {strconv.Itoa(min(max(limit, 1), 100))},
		"reverse":    {strconv.FormatBool(serverReverse)},
	}
	if params.Cursor != "" {
		query.Set("cursor", params.Cursor)
	}
	target := NormalizePDSBase(pdsBase) + "/xrpc/com.atproto.repo.listRecords?" + query.Encode()
	if _, err := url.Parse(target); err != nil {
		return unavailable()
	}

	payload, err := c.executeJSON(ctx, target, auth)
	if err != nil {
		return nil, "", err
	}
	if params.RequireResolvedRepository {
		if err := ValidatePublicListRecordsResponse(payload); err != nil {
			return nil, "", err
		}
	}
	rows := recordRows(payload["records"])
	if sortNewestFirst {
		sortRecordsNewestFirst(rows)
	}
	records := make([]RepoRecord, 0, len(rows))
	for _, row := range rows {
		uri, uriOK := row["uri"].(string)
		value, valueOK := row["value"].(map[string]any)
		if !uriOK || !valueOK {
			continue
		}
		cid, _ := row["cid"].(string)
		records = append(records, RepoRecord{URI: uri, CID: cid, Value: value})
	}
	if params.RequireResolvedRepository && len(records) != len(rows) {
		return nil, "", ErrRepoMalformedResponse
	}
	cursor, _ := payload["cursor"].(string)
	return records, cursor, nil
}

// recordRows mirrors `json["records"] as? [[String: Any]] ?? []`: any
// non-object element voids the whole list.
func recordRows(raw any) []map[string]any {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	rows := make([]map[string]any, 0, len(list))
	for _, item := range list {
		row, ok := item.(map[string]any)
		if !ok {
			return nil
		}
		rows = append(rows, row)
	}
	return rows
}

// ValidatePublicListRecordsResponse accepts only a records array whose every
// row has a string uri and object value, and an absent/null/string cursor;
// only such confirmed pages may clear a projection.
func ValidatePublicListRecordsResponse(payload map[string]any) error {
	raw, ok := payload["records"].([]any)
	if !ok {
		return ErrRepoMalformedResponse
	}
	for _, item := range raw {
		row, ok := item.(map[string]any)
		if !ok {
			return ErrRepoMalformedResponse
		}
		if _, ok := row["uri"].(string); !ok {
			return ErrRepoMalformedResponse
		}
		if _, ok := row["value"].(map[string]any); !ok {
			return ErrRepoMalformedResponse
		}
	}
	if cursor, present := payload["cursor"]; present && cursor != nil {
		if _, ok := cursor.(string); !ok {
			return ErrRepoMalformedResponse
		}
	}
	return nil
}

// ListAllRecords pages newest-first until the cursor ends or maxPages pages
// (pageLimit 0 → 50, maxPages 0 → 20).
func (c *RepoClient) ListAllRecords(ctx context.Context, auth *reqctx.Auth, repo, collection string, pageLimit, maxPages int) ([]RepoRecord, error) {
	if pageLimit == 0 {
		pageLimit = 50
	}
	if maxPages == 0 {
		maxPages = 20
	}
	all := []RepoRecord{}
	cursor := ""
	for pages := 0; ; {
		records, next, err := c.ListRecords(ctx, auth, ListRecordsParams{Repo: repo, Collection: collection, Limit: pageLimit, Cursor: cursor})
		if err != nil {
			return nil, err
		}
		all = append(all, records...)
		cursor = next
		pages++
		if cursor == "" || pages >= maxPages {
			return all, nil
		}
	}
}

// GetRecord returns the record value, or nil when the repo is unresolvable,
// the record is missing, or the PDS answers non-200.
func (c *RepoClient) GetRecord(ctx context.Context, auth *reqctx.Auth, repo, collection, rkey string) (map[string]any, error) {
	repoDID, payload, err := c.getRecordPayload(ctx, auth, repo, collection, rkey, "")
	if err != nil || repoDID == "" {
		return nil, err
	}
	value, _ := payload["value"].(map[string]any)
	return value, nil
}

// GetRecordWithMetadata reads from the resolved author PDS and keeps the
// repository URI/CID; it returns nil unless the PDS echoes the requested URI
// (and the pinned cid, when one is supplied for immutable chunk reads).
func (c *RepoClient) GetRecordWithMetadata(ctx context.Context, auth *reqctx.Auth, repo, collection, rkey, cid string) (*RepoRecord, error) {
	repoDID, payload, err := c.getRecordPayload(ctx, auth, repo, collection, rkey, cid)
	if err != nil || repoDID == "" {
		return nil, err
	}
	value, valueOK := payload["value"].(map[string]any)
	returnedURI, uriOK := payload["uri"].(string)
	returnedCID, cidOK := payload["cid"].(string)
	if !valueOK || !uriOK || !cidOK || returnedURI != BuildRecordURI(repoDID, collection, rkey) ||
		(cid != "" && cid != returnedCID) {
		return nil, nil
	}
	return &RepoRecord{URI: returnedURI, CID: returnedCID, Value: value}, nil
}

// GetRecordByATURI parses a strict at://did/collection/rkey (trimmed) and
// reads it; malformed URIs return nil.
func (c *RepoClient) GetRecordByATURI(ctx context.Context, auth *reqctx.Auth, atURI string) (map[string]any, error) {
	parsed, ok := ParseRecordURI(NormalizeAtRepoParam(atURI))
	if !ok {
		return nil, nil
	}
	return c.GetRecord(ctx, auth, parsed.Repo, parsed.Collection, parsed.RKey)
}

func (c *RepoClient) getRecordPayload(ctx context.Context, auth *reqctx.Auth, repo, collection, rkey, cid string) (string, map[string]any, error) {
	repoDID, err := c.resolver.ResolveRepoDID(ctx, repo)
	if err != nil || repoDID == "" {
		return "", nil, err
	}
	pdsBase, err := c.resolver.ResolvePDSBase(ctx, repoDID)
	if err != nil || pdsBase == "" {
		return "", nil, err
	}
	query := url.Values{"repo": {repoDID}, "collection": {collection}, "rkey": {rkey}}
	if cid != "" {
		query.Set("cid", cid)
	}
	target := NormalizePDSBase(pdsBase) + "/xrpc/com.atproto.repo.getRecord?" + query.Encode()
	payload, err := c.executeJSON(ctx, target, auth)
	if err != nil {
		return "", nil, err
	}
	return repoDID, payload, nil
}

// CreateRecord creates a record in the viewer's repo and returns its URI
// ("" when the viewer's PDS is unresolvable or the reply has no uri).
func (c *RepoClient) CreateRecord(ctx context.Context, auth *reqctx.Auth, collection string, record map[string]any) (string, error) {
	reply, err := c.mutateRecord(ctx, auth, "com.atproto.repo.createRecord", map[string]any{
		"repo": auth.DID, "collection": collection, "record": record,
	})
	if err != nil {
		return "", err
	}
	uri, _ := reply["uri"].(string)
	return uri, nil
}

// PutRecord upserts a record in the viewer's repo.
func (c *RepoClient) PutRecord(ctx context.Context, auth *reqctx.Auth, collection, rkey string, record map[string]any) error {
	_, err := c.mutateRecord(ctx, auth, "com.atproto.repo.putRecord", map[string]any{
		"repo": auth.DID, "collection": collection, "rkey": rkey, "record": record,
	})
	return err
}

// DeleteRecord deletes a record from the viewer's repo.
func (c *RepoClient) DeleteRecord(ctx context.Context, auth *reqctx.Auth, collection, rkey string) error {
	_, err := c.mutateRecord(ctx, auth, "com.atproto.repo.deleteRecord", map[string]any{
		"repo": auth.DID, "collection": collection, "rkey": rkey,
	})
	return err
}

// mutateRecord POSTs a repo write to the viewer's PDS with forwarded
// credentials. Non-200 replies map to 401 / 403 / 502 *httpx.Error. A
// missing PDS returns nil without error, as Swift did.
func (c *RepoClient) mutateRecord(ctx context.Context, auth *reqctx.Auth, method string, body map[string]any) (map[string]any, error) {
	pdsBase, err := c.resolver.ResolvePDSBase(ctx, auth.DID)
	if err != nil || pdsBase == "" {
		return nil, err
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, repoRequestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(reqCtx, http.MethodPost, NormalizePDSBase(pdsBase)+"/xrpc/"+method, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	ApplyForwardedCredentials(request.Header, auth)
	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	frame, err := readBounded(response.Body, repoBodyLimit)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		text := string(frame)
		if len(text) > 512 {
			text = text[:512]
		}
		c.logger.WarnContext(ctx, "XRPC mutate non-OK", "status", response.StatusCode, "method", method, "body", text)
		switch response.StatusCode {
		case http.StatusUnauthorized:
			return nil, httpx.Errorf(http.StatusUnauthorized, "PDS rejected OAuth credentials for "+method)
		case http.StatusForbidden:
			return nil, httpx.Errorf(http.StatusForbidden, "PDS rejected repo scope for "+method)
		default:
			return nil, httpx.Errorf(http.StatusBadGateway, "PDS "+method+" failed ("+strconv.Itoa(response.StatusCode)+")")
		}
	}
	var payload map[string]any
	if json.Unmarshal(frame, &payload) != nil || payload == nil {
		return map[string]any{}, nil
	}
	return payload, nil
}

// executeJSON GETs an XRPC URL; non-200 and non-object replies yield an
// empty map, transport failures and oversized bodies an error.
func (c *RepoClient) executeJSON(ctx context.Context, target string, auth *reqctx.Auth) (map[string]any, error) {
	reqCtx, cancel := context.WithTimeout(ctx, repoRequestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(reqCtx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	if auth != nil {
		ApplyForwardedCredentials(request.Header, auth)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if response.StatusCode != http.StatusNotFound {
			c.logger.DebugContext(ctx, "XRPC non-OK", "status", response.StatusCode, "url", target)
		}
		return map[string]any{}, nil
	}
	frame, err := readBounded(response.Body, repoBodyLimit)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if json.Unmarshal(frame, &payload) != nil || payload == nil {
		return map[string]any{}, nil
	}
	return payload, nil
}

// ApplyForwardedCredentials sets the viewer's Authorization header and the
// PDS-bound DPoP proof (the X-ATProto-Upstream-DPoP proof, else the
// gateway-bound one) on an outbound PDS request. The gateway cannot mint
// proofs, so an RFC 9449 use_dpop_nonce challenge is surfaced to the caller
// (as 401) for the client to retry with a fresh proof.
func ApplyForwardedCredentials(header http.Header, auth *reqctx.Auth) {
	if auth == nil {
		return
	}
	if authorization := strings.TrimSpace(auth.AuthorizationForwardingValue); authorization != "" {
		header.Set("Authorization", authorization)
	}
	if proof := PDSDPoPProof(auth); proof != "" {
		header.Set("DPoP", proof)
	}
}

// PDSDPoPProof prefers the upstream (PDS-bound) proof over the gateway proof.
func PDSDPoPProof(auth *reqctx.Auth) string {
	if upstream := strings.TrimSpace(auth.UpstreamDPoPProof); upstream != "" {
		return upstream
	}
	return strings.TrimSpace(auth.DPoPProof)
}

func sortRecordsNewestFirst(rows []map[string]any) {
	sort.SliceStable(rows, func(i, j int) bool {
		ti, tj := recordDateString(rows[i]), recordDateString(rows[j])
		if ti != tj {
			return ti > tj
		}
		ui, _ := rows[i]["uri"].(string)
		uj, _ := rows[j]["uri"].(string)
		return ui > uj
	})
}

func recordDateString(row map[string]any) string {
	value, ok := row["value"].(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"publishedAt", "createdAt", "indexedAt"} {
		if text, ok := value[key].(string); ok {
			return text
		}
	}
	return ""
}
