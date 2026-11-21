package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
)

var dependency = errors.New("OAuth dependency unavailable")

type authority struct {
	pds        string
	attestable bool
	keys       keySet
	expires    time.Time
	fetchedAt  time.Time
}

func fetch(ctx context.Context, target string, headers http.Header, out any) (int, http.Header, error) {
	address, err := atproto.PublicDNS.ValidatedAddress(ctx, target, time.Now().Add(5*time.Second))
	if err != nil {
		if errors.Is(err, atproto.ErrPublicTargetInvalid) {
			return 0, nil, invalid
		}
		return 0, nil, dependency
	}
	client := atproto.NewPinnedHTTPClient(address, 5*time.Second, 5*time.Second)
	defer client.CloseIdleConnections()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, nil, invalid
	}
	if headers != nil {
		request.Header = headers.Clone()
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return 0, nil, dependency
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return 0, nil, dependency
	}
	if response.StatusCode == http.StatusOK {
		const limit = 64 << 10
		data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
		if err != nil {
			return 0, nil, dependency
		}
		if len(data) > limit || json.Unmarshal(data, out) != nil {
			return 0, nil, invalid
		}
	}
	return response.StatusCode, response.Header, nil
}

func (v *Verifier) discover(ctx context.Context, subject, issuer string, now time.Time) (authority, error) {
	var result authority
	pds, err := v.resolver.ResolvePDSBaseStrict(ctx, subject, atproto.EndpointPublicHTTPS)
	if err != nil {
		if errors.Is(err, atproto.ErrUnsafeServiceEndpoint) {
			return result, invalid
		}
		return result, dependency
	}
	if pds == "" {
		return result, invalid
	}
	result.pds = pds
	var resource struct {
		Servers []string `json:"authorization_servers"`
	}
	// The PDS can be its own authorization server. Active attestation still
	// requires the PDS to name the authorization server explicitly.
	if _, _, err = v.fetch(ctx, pds+"/.well-known/oauth-protected-resource", nil, &resource); err != nil {
		return result, err
	}
	for _, raw := range resource.Servers {
		server, ok := atproto.ValidatedPDSBase(raw, atproto.EndpointPublicHTTPS)
		if ok && server == issuer {
			result.attestable = true
			break
		}
	}
	if issuer != pds && !result.attestable {
		return result, invalid
	}
	for _, endpoint := range []string{"/.well-known/oauth-authorization-server", "/.well-known/openid-configuration"} {
		var metadata struct {
			Issuer  string `json:"issuer"`
			JWKSURI string `json:"jwks_uri"`
			Keys    keySet `json:"jwks"`
		}
		status, _, err := v.fetch(ctx, issuer+endpoint, nil, &metadata)
		if err != nil {
			return result, err
		}
		if status != http.StatusOK {
			continue
		}
		metadataIssuer, ok := atproto.ValidatedPDSBase(metadata.Issuer, atproto.EndpointPublicHTTPS)
		if !ok || metadataIssuer != issuer {
			return result, invalid
		}
		result.keys.Keys = append(result.keys.Keys, metadata.Keys.Keys...)
		if metadata.JWKSURI == "" {
			continue
		}
		base, _ := url.Parse(issuer + "/")
		target, err := base.Parse(metadata.JWKSURI)
		if err != nil || target.User != nil || target.Fragment != "" || target.Scheme != base.Scheme ||
			!strings.EqualFold(target.Hostname(), base.Hostname()) || port(target) != port(base) {
			return result, invalid
		}
		var remote keySet
		status, _, err = v.fetch(ctx, target.String(), nil, &remote)
		if err != nil {
			return result, err
		}
		if status != http.StatusOK && status != http.StatusNotFound {
			return result, invalid
		}
		result.keys.Keys = append(result.keys.Keys, remote.Keys...)
		break
	}
	if len(result.keys.Keys) > 64 {
		return authority{}, invalid
	}
	result.fetchedAt = now
	result.expires = now.Add(5 * time.Minute)
	if len(result.keys.Keys) == 0 {
		result.expires = now.Add(30 * time.Second)
	}
	return result, nil
}

func port(target *url.URL) string {
	if port := target.Port(); port != "" {
		return port
	}
	return "443"
}

func (v *Verifier) authority(ctx context.Context, subject, issuer string, now time.Time, force bool) (authority, error) {
	cacheKey := subject + "\x00" + issuer
	v.mu.Lock()
	cached, ok := v.authorities[cacheKey]
	v.mu.Unlock()
	if ok && now.Before(cached.expires) && (!force || now.Sub(cached.fetchedAt) < 30*time.Second) {
		return cached, nil
	}
	result := v.discoveryGroup.DoChan(cacheKey, func() (any, error) {
		v.mu.Lock()
		cached, ok := v.authorities[cacheKey]
		v.mu.Unlock()
		if ok && now.Before(cached.expires) && (!force || now.Sub(cached.fetchedAt) < 30*time.Second) {
			return cached, nil
		}
		select {
		case v.discovery <- struct{}{}:
			defer func() { <-v.discovery }()
		default:
			return authority{}, dependency
		}
		// A disconnected first caller must not cancel verification shared by
		// other requests. The per-flight timeout still bounds all upstream work.
		requestCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 12*time.Second)
		defer cancel()
		discovered, err := v.discover(requestCtx, subject, issuer, now)
		if err != nil {
			return authority{}, err
		}
		v.mu.Lock()
		defer v.mu.Unlock()
		if len(v.authorities) >= 1024 {
			for key, value := range v.authorities {
				if !now.Before(value.expires) {
					delete(v.authorities, key)
				}
			}
		}
		if len(v.authorities) < 1024 {
			v.authorities[cacheKey] = discovered
		}
		return discovered, nil
	})
	select {
	case outcome := <-result:
		return outcome.Val.(authority), outcome.Err
	case <-ctx.Done():
		return authority{}, dependency
	}
}
