package oauthclient

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SessionStore coordinates token rotation with a database lease and durable
// per-token intent. Nil from LoadOAuthSession means the account is disconnected.
type SessionStore interface {
	LoadOAuthSession(context.Context, string) (*Session, error)
	ClaimOAuthRefresh(context.Context, string, string, string, int64, int64) (bool, error)
	BeginOAuthRefresh(context.Context, string, string, string, int64) error
	CompleteOAuthRefresh(context.Context, Session, string, string, string, int64) error
	ReleaseOAuthRefresh(context.Context, string, string) error
}

type activeSession struct {
	record   Session
	token    tokenPayload
	key      *ecdsa.PrivateKey
	nonceKey string
}

type nonceEntry struct {
	value   string
	expires time.Time
}

func load(ctx context.Context, did string, store SessionStore) (activeSession, error) {
	var result activeSession
	record, err := store.LoadOAuthSession(ctx, did)
	if err != nil {
		return result, err
	}
	if record == nil || record.DID != did || len(record.TokenJSON) > 128<<10 ||
		json.Unmarshal([]byte(record.TokenJSON), &result.token) != nil ||
		result.token.Subject != did || !strings.EqualFold(result.token.Type, "dpop") || result.token.AccessToken == "" {
		return result, ErrNotConfigured
	}
	if _, ok := publicOrigin(result.token.PDSEndpoint); !ok {
		return result, ErrNotConfigured
	}
	if _, ok := endpoint(result.token.TokenEndpoint); !ok {
		return result, ErrNotConfigured
	}
	result.key, err = decodeKey(record.DPoPKeyJSON)
	if err != nil {
		return result, ErrNotConfigured
	}
	result.record = *record
	result.nonceKey = did + "\x00" + result.token.PDSEndpoint + "\x00" + sha256URL(record.DPoPKeyJSON)
	return result, nil
}

// XRPC calls a repo method at the viewer's persisted, validated PDS origin.
// One nonce challenge and one token refresh can be retried during a request.
func (c *Client) XRPC(ctx context.Context, did, method, nsid string, query url.Values, body []byte, contentType string, store SessionStore) ([]byte, error) {
	if !allowedMethod(method, nsid) {
		return nil, errors.New("unsupported scheduling repository method")
	}
	if repos, present := query["repo"]; present && (len(repos) != 1 || repos[0] != did) {
		return nil, errors.New("scheduler cannot read another account's repository")
	}
	if store == nil {
		return nil, ErrNotConfigured
	}
	if nsid == "com.atproto.repo.uploadBlob" {
		// A nonce challenge before the PDS reads a large upload can delay
		// unwinding the request body. Prime the current nonce with getSession.
		if _, err := c.XRPC(ctx, did, http.MethodGet, "com.atproto.server.getSession", nil, nil, "", store); err != nil {
			return nil, err
		}
	}
	session, err := load(ctx, did, store)
	if err != nil {
		return nil, err
	}
	reply, err := c.xrpc(ctx, session, method, nsid, query, body, contentType)
	var unauthorized *XRPCError
	if !errors.As(err, &unauthorized) || unauthorized.Status != http.StatusUnauthorized {
		return reply, err
	}
	refreshed, err := c.refreshSession(ctx, session, store)
	if err != nil {
		return nil, err
	}
	if refreshed.token.PDSEndpoint != session.token.PDSEndpoint {
		return nil, errors.New("the scheduling account's PDS changed")
	}
	return c.xrpc(ctx, refreshed, method, nsid, query, body, contentType)
}

func allowedMethod(method, nsid string) bool {
	switch nsid {
	case "com.atproto.server.getSession", "com.atproto.repo.getRecord", "com.atproto.repo.listRecords":
		return method == http.MethodGet
	case "com.atproto.repo.putRecord", "com.atproto.repo.createRecord", "com.atproto.repo.deleteRecord", "com.atproto.repo.uploadBlob":
		return method == http.MethodPost
	default:
		return false
	}
}

func (c *Client) xrpc(ctx context.Context, session activeSession, method, nsid string, query url.Values, body []byte, contentType string) ([]byte, error) {
	base, _ := publicOrigin(session.token.PDSEndpoint)
	target, _ := endpoint(base + "/xrpc/" + nsid)
	target.RawQuery = query.Encode()
	if contentType == "" {
		contentType = "application/json"
	}
	reply, err := c.signedWithNonceRetry(ctx, method, target, body, contentType, session.key, session.token.AccessToken, c.cachedNonce(session.nonceKey))
	if reply.nonce != "" {
		c.storeNonce(session.nonceKey, reply.nonce)
	}
	return reply.body, err
}

func (c *Client) cachedNonce(key string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.nonces[key]
	if !time.Now().Before(entry.expires) {
		delete(c.nonces, key)
		return ""
	}
	return entry.value
}

func (c *Client) storeNonce(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if len(c.nonces) >= 10_000 {
		for key, entry := range c.nonces {
			if !now.Before(entry.expires) {
				delete(c.nonces, key)
			}
		}
	}
	if _, exists := c.nonces[key]; exists || len(c.nonces) < 10_000 {
		c.nonces[key] = nonceEntry{value, now.Add(10 * time.Minute)}
	}
}

func (c *Client) refreshSession(ctx context.Context, session activeSession, store SessionStore) (activeSession, error) {
	pending := c.refresh.DoChan(session.record.DID, func() (any, error) {
		requestCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		owner, err := NewVerifier()
		if err != nil {
			return activeSession{}, err
		}
		var current activeSession
		var tokenHash string
		for {
			current, err = load(requestCtx, session.record.DID, store)
			if err != nil || current.record.TokenJSON != session.record.TokenJSON || current.record.DPoPKeyJSON != session.record.DPoPKeyJSON {
				return current, err
			}
			if current.token.RefreshToken == "" {
				return activeSession{}, ErrNotConfigured
			}
			tokenHash = sha256URL(current.token.RefreshToken)
			now := time.Now()
			held, claimErr := store.ClaimOAuthRefresh(requestCtx, current.record.DID, owner, tokenHash, now.UnixMicro(), now.Add(time.Minute).UnixMicro())
			if claimErr != nil {
				fresh, reloadErr := load(requestCtx, session.record.DID, store)
				if reloadErr == nil && (fresh.record.TokenJSON != session.record.TokenJSON || fresh.record.DPoPKeyJSON != session.record.DPoPKeyJSON) {
					return fresh, nil
				}
				return activeSession{}, claimErr
			}
			if held {
				break
			}
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-requestCtx.Done():
				timer.Stop()
				return activeSession{}, requestCtx.Err()
			case <-timer.C:
			}
		}
		defer func() {
			release, stop := context.WithTimeout(context.WithoutCancel(requestCtx), 5*time.Second)
			defer stop()
			_ = store.ReleaseOAuthRefresh(release, session.record.DID, owner)
		}()
		// Re-read after claiming: another instance may have rotated just before
		// our lease transaction. Do not consume a stale token in that case.
		current, err = load(requestCtx, session.record.DID, store)
		if err != nil || current.record.TokenJSON != session.record.TokenJSON || current.record.DPoPKeyJSON != session.record.DPoPKeyJSON {
			return current, err
		}
		if sha256URL(current.token.RefreshToken) != tokenHash {
			return activeSession{}, ErrNotConfigured
		}
		refreshToken := current.token.RefreshToken
		if err := store.BeginOAuthRefresh(requestCtx, current.record.DID, owner, tokenHash, time.Now().UnixMicro()); err != nil {
			return activeSession{}, err
		}

		target, _ := endpoint(current.token.TokenEndpoint)
		form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}, "client_id": {c.ClientID()}}
		reply, err := c.requestToken(requestCtx, target, current.key, form)
		if err != nil {
			return activeSession{}, err
		}
		if !validActorToken(reply, current.record.DID) {
			return activeSession{}, ErrToken
		}
		if reply.Scope == "" {
			reply.Scope = current.token.Scope
		}
		if reply.RefreshToken == "" {
			reply.RefreshToken = refreshToken
		}
		replacement, err := encodeSession(current.record, reply, current.token.PDSEndpoint, current.token.TokenEndpoint)
		if err != nil {
			return activeSession{}, err
		}
		if err := store.CompleteOAuthRefresh(requestCtx, replacement, current.record.TokenJSON, owner, tokenHash, time.Now().UnixMicro()); err != nil {
			return activeSession{}, err
		}
		return load(requestCtx, current.record.DID, store)
	})
	select {
	case result := <-pending:
		return result.Val.(activeSession), result.Err
	case <-ctx.Done():
		return activeSession{}, ctx.Err()
	}
}
