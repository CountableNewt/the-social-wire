package atproto

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// BskyAppViewPublic is the public Bluesky App View used for identity, graph,
// and profile reads. Never use it for com.atproto.repo.* on third-party repos.
const BskyAppViewPublic = "https://public.api.bsky.app"

const (
	resolutionTimeout = 15 * time.Second
	plcBodyLimit      = 64 * 1024
	handleBodyLimit   = 16 * 1024
	resolutionLease   = 15 * time.Second
)

// Resolver resolves handles to DIDs (public App View) and DIDs to PDS base
// URLs (PLC `GET {plc}/{did}`), sharing a DID → PDS cache.
type Resolver struct {
	doer       HTTPDoer
	plcURL     string
	appViewURL string
	cache      *PDSResolutionCache
}

// NewResolver returns a resolver using doer (normally the shared *http.Client),
// the PLC directory base URL, the public App View, and SharedPDSResolutionCache.
func NewResolver(doer HTTPDoer, plcURL string) *Resolver {
	return &Resolver{doer: doer, plcURL: plcURL, appViewURL: BskyAppViewPublic, cache: SharedPDSResolutionCache}
}

// WithCache returns a copy using cache instead of the shared cache.
func (r *Resolver) WithCache(cache *PDSResolutionCache) *Resolver {
	copied := *r
	copied.cache = cache
	return &copied
}

// WithAppViewURL returns a copy resolving handles against base.
func (r *Resolver) WithAppViewURL(base string) *Resolver {
	copied := *r
	copied.appViewURL = base
	return &copied
}

// WithDoer returns a copy sending requests through doer (e.g. a RateLimitedDoer).
func (r *Resolver) WithDoer(doer HTTPDoer) *Resolver {
	copied := *r
	copied.doer = doer
	return &copied
}

// PLCURL returns the configured PLC directory base.
func (r *Resolver) PLCURL() string { return r.plcURL }

// NormalizePDSBase trims whitespace and trailing slashes.
func NormalizePDSBase(endpoint string) string {
	return strings.TrimRight(strings.TrimSpace(endpoint), "/")
}

// RelayHostOmitsListRecordsReverse reports PDS hosts (Bridgy Fed relay) that
// reject reverse=true on listRecords.
func RelayHostOmitsListRecordsReverse(pdsBase string) bool {
	parsed, err := url.Parse(pdsBase)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "atproto.brid.gy" || strings.HasSuffix(host, ".brid.gy")
}

// ParsePDSEndpointFromPLCDoc returns the first #atproto_pds /
// AtprotoPersonalDataServer serviceEndpoint, normalized, or "".
func ParsePDSEndpointFromPLCDoc(doc map[string]any) string {
	services, ok := serviceList(doc)
	if !ok {
		return ""
	}
	for _, service := range services {
		if endpoint, matched := pdsServiceEndpoint(service); matched {
			return NormalizePDSBase(endpoint)
		}
	}
	return ""
}

func serviceList(doc map[string]any) ([]map[string]any, bool) {
	raw, ok := doc["service"].([]any)
	if !ok {
		return nil, false
	}
	services := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		service, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		services = append(services, service)
	}
	return services, true
}

func pdsServiceEndpoint(service map[string]any) (string, bool) {
	endpoint, ok := service["serviceEndpoint"].(string)
	if !ok {
		return "", false
	}
	id, _ := service["id"].(string)
	kind, _ := service["type"].(string)
	return endpoint, id == "#atproto_pds" || kind == "AtprotoPersonalDataServer"
}

// validatedCachedEndpoint is the gateway's lenient check: an absolute HTTPS
// URL with a host and no credentials, query, or fragment.
func validatedCachedEndpoint(endpoint string) string {
	if endpoint == "" {
		return ""
	}
	normalized := NormalizePDSBase(endpoint)
	if strings.Contains(normalized, "#") {
		return ""
	}
	parsed, err := url.Parse(normalized)
	if err != nil || strings.ToLower(parsed.Scheme) != "https" || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return ""
	}
	return normalized
}

// ResolvePDSBase resolves the HTTPS PDS base for repoDID through the cache and
// PLC (ATProtoPdsResolution.resolvePdsBase). It returns "" with a nil error
// when the DID has no usable PDS (negative-cached), and *PLCUnavailableError
// when PLC answers 429 or 5xx (never cached).
func (r *Resolver) ResolvePDSBase(ctx context.Context, repoDID string) (string, error) {
	if !strings.HasPrefix(repoDID, "did:") {
		return "", nil
	}
	hit, endpoint, lease, err := r.consultCache(ctx, repoDID)
	if err != nil {
		return "", err
	}
	if hit {
		return validatedCachedEndpoint(endpoint), nil
	}
	if lease != nil {
		defer r.cache.ReleaseLease(*lease)
	}

	response, err := r.fetchPLC(ctx, repoDID)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return "", &PLCUnavailableError{Status: response.StatusCode}
	}
	if response.StatusCode != http.StatusOK {
		r.cache.StoreUnresolved(repoDID)
		return "", nil
	}
	body, err := readBounded(response.Body, plcBodyLimit)
	if err != nil {
		return "", err
	}
	var doc map[string]any
	if json.Unmarshal(body, &doc) != nil || doc == nil {
		r.cache.StoreUnresolved(repoDID)
		return "", nil
	}
	base := validatedCachedEndpoint(ParsePDSEndpointFromPLCDoc(doc))
	if base == "" {
		r.cache.StoreUnresolved(repoDID)
		return "", nil
	}
	r.cache.StoreResolved(repoDID, base)
	return base, nil
}

// consultCache serves a fresh (or unleased stale) entry, waits briefly for a
// concurrent resolver on a miss, or grants this caller the refresh lease.
func (r *Resolver) consultCache(ctx context.Context, did string) (hit bool, endpoint string, lease *PDSLease, err error) {
	state, cached := r.cache.Lookup(did)
	switch state {
	case PDSCacheFresh:
		return true, cached, nil, nil
	case PDSCacheStale:
		held, ok := r.cache.AcquireLease(did, resolutionLease)
		if !ok {
			return true, cached, nil, nil
		}
		return false, "", &held, nil
	default:
		if held, ok := r.cache.AcquireLease(did, resolutionLease); ok {
			return false, "", &held, nil
		}
		for range 5 {
			timer := time.NewTimer(50 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return false, "", nil, ctx.Err()
			case <-timer.C:
			}
			if state, cached := r.cache.Lookup(did); state != PDSCacheMiss {
				return true, cached, nil, nil
			}
		}
		return false, "", nil, nil
	}
}

func (r *Resolver) fetchPLC(ctx context.Context, did string) (*http.Response, error) {
	target := NormalizePDSBase(r.plcURL) + "/" + escapeURLPath(did)
	return r.get(ctx, target, resolutionTimeout)
}

// get issues a JSON GET whose context (and so body reads) end after timeout.
func (r *Resolver) get(ctx context.Context, target string, timeout time.Duration) (*http.Response, error) {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	request, err := http.NewRequestWithContext(reqCtx, http.MethodGet, target, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := r.doer.Do(request)
	if err != nil {
		cancel()
		return nil, err
	}
	response.Body = &cancelOnClose{ReadCloser: response.Body, cancel: cancel}
	return response, nil
}

// ResolveRepoDID returns handleOrDID when it is already a DID, else resolves
// the handle (leading "@" stripped) via the public App View. It returns ""
// with a nil error when the handle does not resolve.
func (r *Resolver) ResolveRepoDID(ctx context.Context, handleOrDID string) (string, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(handleOrDID), "@")
	if trimmed == "" {
		return "", nil
	}
	if strings.HasPrefix(trimmed, "did:") {
		return trimmed, nil
	}
	target := NormalizePDSBase(r.appViewURL) + "/xrpc/com.atproto.identity.resolveHandle?" +
		url.Values{"handle": {trimmed}}.Encode()
	response, err := r.get(ctx, target, resolutionTimeout)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", nil
	}
	body, err := readBounded(response.Body, handleBodyLimit)
	if err != nil {
		return "", err
	}
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		return "", nil
	}
	did, _ := payload["did"].(string)
	return did, nil
}

// escapeURLPath percent-encodes like Foundation's urlPathAllowed set.
func escapeURLPath(value string) string {
	const allowed = "!$&'()*+,-./:;=@_~"
	var builder strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.IndexByte(allowed, c) >= 0 {
			builder.WriteByte(c)
			continue
		}
		fmt.Fprintf(&builder, "%%%02X", c)
	}
	return builder.String()
}
