package oauthclient

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

const actor = "did:plc:abcdefghijklmnopqrstuvwx"
const legacyKey = `{"rawRepresentation":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAE=","crv":"P-256","kty":"EC"}`
const pdsOrigin = "https://pds.socialwire.dev"
const serverOrigin = "https://auth.socialwire.dev"
const clientOrigin = "https://testing.thesocialwire.app"

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func responseWith(status int, body, nonce string) *http.Response {
	headers := make(http.Header)
	if nonce != "" {
		headers.Set("DPoP-Nonce", nonce)
	}
	return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body))}
}

func proofFromRequest(r *http.Request) (proofHeader, proofPayload, error) {
	var header proofHeader
	var payload proofPayload
	parts := strings.Split(r.Header.Get("DPoP"), ".")
	if len(parts) != 3 {
		return header, payload, errors.New("missing DPoP proof")
	}
	decoded := make([][]byte, len(parts))
	for i, part := range parts {
		value, err := jwtEncoding.DecodeString(part)
		if err != nil {
			return header, payload, err
		}
		decoded[i] = value
	}
	if json.Unmarshal(decoded[0], &header) != nil || json.Unmarshal(decoded[1], &payload) != nil || len(decoded[2]) != 64 {
		return header, payload, errors.New("invalid DPoP proof")
	}
	x, _ := jwtEncoding.DecodeString(header.Key.X)
	y, _ := jwtEncoding.DecodeString(header.Key.Y)
	pub := ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if header.Type != "dpop+jwt" || header.Algorithm != "ES256" || !ecdsa.Verify(&pub, digest[:],
		new(big.Int).SetBytes(decoded[2][:32]), new(big.Int).SetBytes(decoded[2][32:])) {
		return header, payload, errors.New("invalid DPoP proof signature")
	}
	return header, payload, nil
}

func TestPARAndCallbackReuseDPoPKeyAndBindActor(t *testing.T) {
	var parProof proofHeader
	var initialID string
	tokenCalls := 0
	handler := testTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" {
			t.Error("discovery, PAR, or token request forwarded an Authorization header")
		}
		switch r.URL.Host + r.URL.Path {
		case "plc.directory/" + actor:
			return responseWith(200, `{"id":"`+actor+`","service":[{"id":"#atproto_pds","type":"AtprotoPersonalDataServer","serviceEndpoint":"`+pdsOrigin+`"}]}`, ""), nil
		case "pds.socialwire.dev/.well-known/oauth-protected-resource":
			return responseWith(200, `{"authorization_servers":["`+serverOrigin+`"]}`, ""), nil
		case "auth.socialwire.dev/.well-known/oauth-authorization-server":
			return responseWith(200, `{"issuer":"`+serverOrigin+`","authorization_endpoint":"`+serverOrigin+`/authorize?extra=value","token_endpoint":"`+serverOrigin+`/token","pushed_authorization_request_endpoint":"`+serverOrigin+`/par","scopes_supported":["atproto"]}`, ""), nil
		case "auth.socialwire.dev/par":
			if err := r.ParseForm(); err != nil || r.PostForm.Get("code_challenge") != sha256URL("verifier") ||
				r.PostForm.Get("code_challenge_method") != "S256" || r.PostForm.Get("state") != "opaque-state" ||
				r.PostForm.Get("login_hint") != actor || r.PostForm.Get("scope") != Scope {
				t.Errorf("wrong PAR form: %v, %v", r.PostForm, err)
			}
			header, claims, err := proofFromRequest(r)
			if err != nil || claims.Target != serverOrigin+"/par" || claims.Method != http.MethodPost || claims.AccessHash != "" {
				t.Errorf("wrong PAR proof: %+v, %v", claims, err)
			}
			if claims.Nonce == "" {
				initialID = claims.ID
				parProof = header
				return responseWith(400, `{"error":"use_dpop_nonce"}`, "nonce"), nil
			}
			if claims.Nonce != "nonce" || claims.ID == initialID || header != parProof {
				t.Errorf("wrong PAR challenge response: %+v", claims)
			}
			return responseWith(201, `{"request_uri":"urn:par:request","expires_in":90}`, ""), nil
		case "auth.socialwire.dev/token":
			tokenCalls++
			if err := r.ParseForm(); err != nil || r.PostForm.Get("grant_type") != "authorization_code" ||
				r.PostForm.Get("code") != "callback-code" || r.PostForm.Get("code_verifier") != "verifier" ||
				r.PostForm.Get("redirect_uri") != clientOrigin+"/oauth/scheduling/callback" {
				t.Errorf("wrong code form: %v, %v", r.PostForm, err)
			}
			header, claims, err := proofFromRequest(r)
			if err != nil || header != parProof || claims.Target != serverOrigin+"/token" || claims.AccessHash != "" {
				t.Errorf("code exchange did not use the PAR key: %+v, %v", claims, err)
			}
			body, _ := json.Marshal(tokenReply{AccessToken: "access-token", RefreshToken: "refresh-token", Type: "DPoP", Scope: Scope, Subject: actor})
			return responseWith(200, string(body), ""), nil
		}
		return nil, fmt.Errorf("unexpected OAuth endpoint %s", r.URL)
	})
	client := New(clientOrigin, &http.Client{Transport: handler})
	start, err := client.Start(t.Context(), actor, "opaque-state", "verifier")
	if err != nil {
		t.Fatal(err)
	}
	redirect, err := url.Parse(start.RedirectURL)
	if err != nil || redirect.Query().Get("request_uri") != "urn:par:request" || redirect.Query().Get("client_id") != client.ClientID() ||
		redirect.Query().Get("extra") != "value" || start.AuthServer != serverOrigin || start.PDSEndpoint != pdsOrigin {
		t.Fatalf("invalid PAR result: %+v, %v", start, err)
	}
	state := State{Handle: actor, InitiatorDID: actor, PKCEVerifier: "verifier", AuthServer: start.AuthServer,
		TokenEndpoint: start.TokenEndpoint, PDSEndpoint: start.PDSEndpoint, DPoPKeyJSON: start.DPoPKeyJSON}
	session, err := client.Complete(t.Context(), state, "callback-code")
	var stored tokenPayload
	if err != nil || session.DID != actor || session.Handle == nil || *session.Handle != actor ||
		session.DPoPKeyJSON != start.DPoPKeyJSON || json.Unmarshal([]byte(session.TokenJSON), &stored) != nil ||
		stored.Subject != actor || stored.RefreshToken != "refresh-token" || stored.PDSEndpoint != pdsOrigin {
		t.Fatalf("invalid callback session: %+v, %v", session, err)
	}
	state.InitiatorDID = "did:plc:unexpected"
	if _, err := client.Complete(t.Context(), state, "callback-code"); !errors.Is(err, ErrToken) || tokenCalls != 2 {
		t.Fatalf("token subject mismatch was accepted: %v", err)
	}
}

type testStore struct {
	mu           sync.Mutex
	session      Session
	replacements int
	leaseOwner   string
	leaseHash    string
	leaseUntil   int64
	started      map[string]bool
}

func (s *testStore) LoadOAuthSession(ctx context.Context, did string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value := s.session
	return &value, nil
}

func (s *testStore) ClaimOAuthRefresh(ctx context.Context, did, owner, hash string, now, until int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.leaseOwner != "" && s.leaseUntil > now {
		return false, nil
	}
	if s.started[hash] {
		return false, ErrNotConfigured
	}
	s.leaseOwner, s.leaseHash, s.leaseUntil = owner, hash, until
	return true, nil
}
func (s *testStore) BeginOAuthRefresh(ctx context.Context, did, owner, hash string, now int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.leaseOwner != owner || s.leaseHash != hash || s.leaseUntil <= now {
		return ErrNotConfigured
	}
	if s.started == nil {
		s.started = map[string]bool{}
	}
	if s.started[hash] {
		return ErrNotConfigured
	}
	s.started[hash] = true
	return nil
}
func (s *testStore) CompleteOAuthRefresh(ctx context.Context, session Session, previous, owner, hash string, now int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.leaseOwner != owner || s.leaseUntil <= now || s.leaseHash != hash {
		return ErrNotConfigured
	}
	if s.session.TokenJSON == previous {
		s.session = session
		s.replacements++
	}
	s.leaseOwner = ""
	return nil
}
func (s *testStore) ReleaseOAuthRefresh(ctx context.Context, did, owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.leaseOwner == owner {
		s.leaseOwner = ""
	}
	return nil
}

func TestXRPCRefreshesOnceAndPreservesSwiftStoredKey(t *testing.T) {
	record := `{"accessToken":"old-token","tokenType":"DPoP","expiresIn":3600,"refreshToken":"old-refresh","scope":"atproto","sub":"` +
		actor + `","pdsEndpoint":"` + pdsOrigin + `","tokenEndpoint":"` + serverOrigin + `/token"}`
	store := &testStore{session: Session{DID: actor, TokenJSON: record, DPoPKeyJSON: legacyKey}}
	key, err := decodeKey(legacyKey)
	if err != nil || key.D.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("Swift key could not be loaded: %v", err)
	}
	exported, err := encodeKey(key)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := decodeKey(exported)
	if err != nil || roundTrip.D.Cmp(key.D) != 0 {
		t.Fatalf("Swift-compatible round trip failed: %v", err)
	}
	var refreshes atomic.Int32
	handler := testTransport(func(r *http.Request) (*http.Response, error) {
		header, claims, err := proofFromRequest(r)
		if err != nil || header.Key.X != jwtEncoding.EncodeToString(key.X.FillBytes(make([]byte, 32))) {
			t.Errorf("wrong stored key in proof: %v", err)
		}
		switch r.URL.Host + r.URL.Path {
		case "pds.socialwire.dev/xrpc/com.atproto.repo.listRecords":
			if strings.Contains(claims.Target, "?") || claims.Method != http.MethodGet ||
				r.URL.Query().Get("repo") != actor {
				t.Errorf("wrong PDS DPoP target: %+v", claims)
			}
			authorization := r.Header.Get("Authorization")
			if claims.AccessHash != sha256URL(strings.TrimPrefix(authorization, "DPoP ")) {
				t.Error("PDS proof omitted the access-token hash")
			}
			if authorization == "DPoP old-token" {
				if claims.Nonce == "" {
					return responseWith(401, `{"error":"use_dpop_nonce"}`, "pds-nonce"), nil
				}
				return responseWith(401, `{"error":"ExpiredToken"}`, ""), nil
			}
			if authorization != "DPoP new-token" || claims.Nonce != "pds-nonce" {
				t.Errorf("incorrect updated PDS token or nonce: %+v", claims)
			}
			return responseWith(200, `{"records":[]}`, ""), nil
		case "auth.socialwire.dev/token":
			refreshes.Add(1)
			if err := r.ParseForm(); err != nil || r.PostForm.Get("grant_type") != "refresh_token" ||
				r.PostForm.Get("refresh_token") != "old-refresh" || r.Header.Get("Authorization") != "" ||
				claims.Nonce != "" || claims.AccessHash != "" {
				t.Errorf("wrong refresh form or DPoP proof: %v, %+v", err, claims)
			}
			return responseWith(200, `{"access_token":"new-token","refresh_token":"rotated","token_type":"DPoP","sub":"`+actor+`"}`, ""), nil
		}
		return nil, fmt.Errorf("unexpected OAuth endpoint %s", r.URL)
	})
	client := New(clientOrigin, &http.Client{Transport: handler})
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			value, err := client.XRPC(t.Context(), actor, http.MethodGet, "com.atproto.repo.listRecords",
				url.Values{"repo": {actor}, "limit": {"1"}}, nil, "", store)
			if err != nil || string(value) != `{"records":[]}` {
				t.Errorf("PDS request failed: %s, %v", value, err)
			}
		})
	}
	group.Wait()
	var saved tokenPayload
	if refreshes.Load() != 1 || store.replacements != 1 || json.Unmarshal([]byte(store.session.TokenJSON), &saved) != nil ||
		saved.AccessToken != "new-token" || saved.RefreshToken != "rotated" || saved.Scope != "atproto" ||
		store.session.DPoPKeyJSON != legacyKey {
		t.Fatal("the refresh token was not rotated exactly once using the persisted key")
	}
	if _, err := client.XRPC(t.Context(), actor, http.MethodGet, "https://other.social/xrpc/place", nil, nil, "", store); err == nil {
		t.Fatal("non-PDS endpoint accepted")
	}
}
