package thinappviewcore

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type PublicGetter interface {
	Get(context.Context, string, http.Header, int, int) (int, http.Header, []byte, error)
}
type PDSClient struct {
	HTTP      PublicGetter
	PLCBase   string
	mu        sync.Mutex
	endpoints map[string]string
}

func (p *PDSClient) Invalidate(did string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.endpoints, did)
}
func (p *PDSClient) Resolve(ctx context.Context, did string) (string, error) {
	if !strings.HasPrefix(did, "did:") {
		return "", nil
	}
	p.mu.Lock()
	endpoint := p.endpoints[did]
	p.mu.Unlock()
	if endpoint != "" {
		return endpoint, nil
	}
	httpClient := p.HTTP
	if httpClient == nil {
		httpClient = PublicHTTP{}
	}
	root := p.PLCBase
	if root == "" {
		root = "https://plc.directory"
	}
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	status, _, body, err := httpClient.Get(requestCtx, strings.TrimRight(root, "/")+"/"+url.PathEscape(did), http.Header{"Accept": []string{"application/json"}}, 64*1024, 0)
	if err != nil {
		return "", err
	}
	if status == 429 || status >= 500 {
		return "", errors.New("PDS resolution transient status")
	}
	if status != 200 {
		return "", nil
	}
	var document struct {
		Services []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Endpoint string `json:"serviceEndpoint"`
		} `json:"service"`
	}
	if err := json.Unmarshal(body, &document); err != nil {
		return "", nil
	}
	for _, service := range document.Services {
		if service.ID == "#atproto_pds" || service.Type == "AtprotoPersonalDataServer" {
			endpoint := ValidatePDSBase(service.Endpoint)
			if endpoint == "" {
				return "", errors.New("unsafe PDS service endpoint")
			}
			p.mu.Lock()
			if p.endpoints == nil {
				p.endpoints = map[string]string{}
			}
			p.endpoints[did] = endpoint
			p.mu.Unlock()
			return endpoint, nil
		}
	}
	return "", nil
}
func (p *PDSClient) FetchRecord(ctx context.Context, did, collection, key string, cid *string) (string, string, []byte, error) {
	base, err := p.Resolve(ctx, did)
	if err != nil {
		return "", "", nil, err
	}
	if base == "" {
		return "", "", nil, errors.New("PDS unavailable")
	}
	params := url.Values{"repo": []string{did}, "collection": []string{collection}, "rkey": []string{key}}
	if cid != nil {
		params.Set("cid", *cid)
	}
	httpClient := p.HTTP
	if httpClient == nil {
		httpClient = PublicHTTP{}
	}
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	status, _, body, err := httpClient.Get(requestCtx, base+"/xrpc/com.atproto.repo.getRecord?"+params.Encode(), http.Header{"Accept": []string{"application/json"}}, 65536+4096, 0)
	if err != nil {
		return "", "", nil, err
	}
	if status != 200 {
		return "", "", nil, errors.New("PDS record unavailable")
	}
	var record struct {
		URI   string          `json:"uri"`
		CID   string          `json:"cid"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(body, &record); err != nil {
		return "", "", nil, err
	}
	if record.URI != "at://"+did+"/"+collection+"/"+key || record.CID == "" || len(record.Value) == 0 || record.Value[0] != '{' {
		return "", "", nil, errors.New("PDS record envelope mismatch")
	}
	if cid != nil && record.CID != *cid {
		return "", "", nil, errors.New("PDS record CID mismatch")
	}
	return record.URI, record.CID, record.Value, nil
}
