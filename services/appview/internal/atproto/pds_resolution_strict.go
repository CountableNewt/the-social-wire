package atproto

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// ResolvePDSBaseStrict is ThinAppViewPdsResolution.resolvePdsBase: endpoints
// (cached or fresh) must pass ValidatedPDSBase under policy or the call fails
// with ErrUnsafeServiceEndpoint; 429/5xx fail with *PLCUnavailableError and
// are not cached; other non-200 statuses negative-cache and return "".
// An unparseable document returns "" without caching.
func (r *Resolver) ResolvePDSBaseStrict(ctx context.Context, repoDID string, policy EndpointPolicy) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !strings.HasPrefix(repoDID, "did:") {
		return "", nil
	}
	hit, endpoint, lease, err := r.consultCache(ctx, repoDID)
	if err != nil {
		return "", err
	}
	if hit {
		if endpoint == "" {
			return "", nil
		}
		validated, ok := ValidatedPDSBase(endpoint, policy)
		if !ok {
			return "", ErrUnsafeServiceEndpoint
		}
		return validated, nil
	}
	if lease != nil {
		defer r.cache.ReleaseLease(*lease)
	}

	response, err := r.fetchPLC(ctx, repoDID)
	if err != nil {
		return "", err
	}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		if err := DrainAndClose(ctx, response.Body, MaximumDiscardBytes); err != nil {
			return "", err
		}
		return "", &PLCUnavailableError{Status: response.StatusCode}
	}
	if response.StatusCode != http.StatusOK {
		if err := DrainAndClose(ctx, response.Body, MaximumDiscardBytes); err != nil {
			return "", err
		}
		r.cache.StoreUnresolved(repoDID)
		return "", nil
	}
	body, err := readBounded(response.Body, plcBodyLimit)
	response.Body.Close()
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var doc map[string]any
	if json.Unmarshal(body, &doc) != nil {
		return "", nil
	}
	services, ok := serviceList(doc)
	if !ok {
		return "", nil
	}
	for _, service := range services {
		endpoint, matched := pdsServiceEndpoint(service)
		if !matched {
			continue
		}
		base, ok := ValidatedPDSBase(endpoint, policy)
		if !ok {
			return "", ErrUnsafeServiceEndpoint
		}
		r.cache.StoreResolved(repoDID, base)
		return base, nil
	}
	r.cache.StoreUnresolved(repoDID)
	return "", nil
}
