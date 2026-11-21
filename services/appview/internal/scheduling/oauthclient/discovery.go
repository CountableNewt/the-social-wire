package oauthclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

func endpoint(raw string) (*url.URL, bool) {
	if len(raw) > 4096 || strings.ContainsAny(raw, " \r\n\t#") {
		return nil, false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Opaque != "" || parsed.Fragment != "" {
		return nil, false
	}
	_, ok := publicOrigin("https://" + parsed.Host)
	return parsed, ok
}

func didEndpoint(did string) (string, error) {
	if len(did) > 512 {
		return "", ErrIdentity
	}
	if plcDID.MatchString(did) {
		return "https://plc.directory/" + url.PathEscape(did), nil
	}
	raw, found := strings.CutPrefix(did, "did:web:")
	if !found {
		return "", ErrIdentity
	}
	parts := strings.Split(raw, ":")
	authority, err := url.PathUnescape(parts[0])
	if err != nil || strings.ContainsAny(authority, "/?#@\\") {
		return "", ErrIdentity
	}
	origin, ok := publicOrigin("https://" + authority)
	if !ok {
		return "", ErrIdentity
	}
	if len(parts) == 1 {
		return origin + "/.well-known/did.json", nil
	}
	for _, part := range parts[1:] {
		if !webDIDPart.MatchString(part) || part == "." || part == ".." {
			return "", ErrIdentity
		}
	}
	return origin + "/" + strings.Join(parts[1:], "/") + "/did.json", nil
}

func (c *Client) resolvePDS(ctx context.Context, did string) (string, error) {
	raw, err := didEndpoint(did)
	if err != nil {
		return "", err
	}
	target, ok := endpoint(raw)
	if !ok {
		return "", ErrIdentity
	}
	var doc struct {
		ID       string `json:"id"`
		Services []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Endpoint string `json:"serviceEndpoint"`
		} `json:"service"`
	}
	res, err := c.do(ctx, http.MethodGet, target, nil, "", nil, "", "")
	if err != nil {
		return "", err
	}
	if json.Unmarshal(res.body, &doc) != nil || doc.ID != did {
		return "", ErrIdentity
	}
	for _, service := range doc.Services {
		if service.ID != "#atproto_pds" && service.ID != did+"#atproto_pds" && service.Type != "AtprotoPersonalDataServer" {
			continue
		}
		if pds, ok := publicOrigin(service.Endpoint); ok {
			return pds, nil
		}
		return "", ErrIdentity
	}
	return "", ErrIdentity
}

func (c *Client) discover(ctx context.Context, pds string) (string, authorizationMetadata, error) {
	var metadata authorizationMetadata
	var resource struct {
		Servers []string `json:"authorization_servers"`
	}
	target, _ := endpoint(pds + "/.well-known/oauth-protected-resource")
	response, err := c.do(ctx, http.MethodGet, target, nil, "", nil, "", "")
	if err != nil {
		return "", metadata, err
	}
	if json.Unmarshal(response.body, &resource) != nil || resource.Servers == nil {
		return "", metadata, ErrAuthorizationServer
	}
	issuer := pds
	if len(resource.Servers) > 0 {
		var ok bool
		if issuer, ok = publicOrigin(resource.Servers[0]); !ok {
			return "", metadata, ErrAuthorizationServer
		}
	}
	target, _ = endpoint(issuer + "/.well-known/oauth-authorization-server")
	response, err = c.do(ctx, http.MethodGet, target, nil, "", nil, "", "")
	if err != nil {
		return "", metadata, err
	}
	if json.Unmarshal(response.body, &metadata) != nil || metadata.Issuer != issuer || !slices.Contains(metadata.SupportedScopes, "atproto") {
		return "", metadata, ErrAuthorizationServer
	}
	for _, raw := range []string{metadata.AuthorizationEndpoint, metadata.TokenEndpoint, metadata.PAREndpoint} {
		if _, ok := endpoint(raw); !ok {
			return "", metadata, ErrAuthorizationServer
		}
	}
	return issuer, metadata, nil
}
