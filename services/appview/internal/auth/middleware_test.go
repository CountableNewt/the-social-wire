package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
)

func key(t *testing.T) (*ecdsa.PrivateKey, jwk, string) {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public := jwk{Type: "EC", Curve: "P-256", X: encoding.EncodeToString(private.X.FillBytes(make([]byte, 32))),
		Y: encoding.EncodeToString(private.Y.FillBytes(make([]byte, 32)))}
	thumbprint := digestString(`{"crv":"P-256","kty":"EC","x":"` + public.X + `","y":"` + public.Y + `"}`)
	return private, public, thumbprint
}

func signed(t *testing.T, private *ecdsa.PrivateKey, header any, claims any) string {
	t.Helper()
	headerJSON, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	input := encoding.EncodeToString(headerJSON) + "." + encoding.EncodeToString(body)
	digest := sha256.Sum256([]byte(input))
	r, s, err := ecdsa.Sign(rand.Reader, private, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	return input + "." + encoding.EncodeToString(signature)
}

func TestMiddlewareRequiresSignatureAndBoundProof(t *testing.T) {
	issuerPrivate, issuerKey, _ := key(t)
	viewerPrivate, viewerKey, thumbprint := key(t)
	otherPrivate, otherKey, _ := key(t)
	issuerKey.ID = "publisher"
	jwks, err := json.Marshal(keySet{Keys: []jwk{issuerKey}})
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := New(config.Core{AttestationReceiptSecret: strings.Repeat("x", 32), SupplementalJWKSJSON: string(jwks),
		OAuthGateway: config.OAuthGatewayPolicy{RequireKnownClient: true, AllowedClientIDs: map[string]struct{}{"first-party": {}}}},
		atproto.NewResolver(http.DefaultClient, "https://plc.directory"))
	if err != nil {
		t.Fatal(err)
	}
	router := httpx.NewRouter()
	router.Group(verifier.Middleware).Get("/v1/protected", func(w http.ResponseWriter, r *http.Request) error {
		viewer := reqctx.AuthFrom(r.Context())
		if viewer.DID != "did:plc:viewer" || viewer.UpstreamDPoPProof != "upstream-proof" {
			t.Errorf("unexpected auth: %+v", viewer)
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	})
	now := time.Now().Unix()
	tokenHeader := map[string]string{"alg": "ES256", "kid": "publisher"}
	tokenClaims := map[string]any{"sub": "did:plc:viewer", "exp": now + 300, "iss": "did:example:issuer",
		"cnf": map[string]string{"jkt": thumbprint}, "client_id": "first-party"}
	token := signed(t, issuerPrivate, tokenHeader, tokenClaims)
	send := func(token string, private *ecdsa.PrivateKey, proofKey jwk, target, id string, clock int64) int {
		t.Helper()
		proof := signed(t, private, map[string]any{"typ": "dpop+jwt", "alg": "ES256", "jwk": proofKey},
			proofClaims{ID: id, IssuedAt: clock, Method: http.MethodGet, URL: target, TokenHash: digestString(token)})
		req := httptest.NewRequest(http.MethodGet, "http://gateway.social/v1/protected?cursor=opaque", nil)
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Header.Set("Authorization", "DPoP "+token)
		req.Header.Set("DPoP", proof)
		req.Header.Set(UpstreamHeader, "upstream-proof")
		reply := httptest.NewRecorder()
		router.ServeHTTP(reply, req)
		return reply.Code
	}
	const target = "https://gateway.social/v1/protected"
	cases := []struct {
		name, token, target, id string
		private                 *ecdsa.PrivateKey
		proofKey                jwk
		clock                   int64
		want                    int
	}{
		{"valid", token, target, "replay", viewerPrivate, viewerKey, now, 204},
		{"replay", token, target, "replay", viewerPrivate, viewerKey, now, 401},
		{"wrong key", token, target, "key", otherPrivate, otherKey, now, 401},
		{"query in proof", token, target + "?cursor=opaque", "query", viewerPrivate, viewerKey, now, 401},
		{"stale", token, target, "stale", viewerPrivate, viewerKey, now - 180, 401},
		{"unsigned by trusted issuer", signed(t, otherPrivate, tokenHeader, tokenClaims), target, "bad-sig", viewerPrivate, viewerKey, now, 401},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if status := send(tc.token, tc.private, tc.proofKey, tc.target, tc.id, tc.clock); status != tc.want {
				t.Errorf("status %d, want %d", status, tc.want)
			}
		})
	}
	tokenClaims["client_id"] = "third-party"
	if status := send(signed(t, issuerPrivate, tokenHeader, tokenClaims), viewerPrivate, viewerKey, target, "policy", now); status != 403 {
		t.Errorf("unauthorized OAuth client got HTTP %d", status)
	}
}

func TestReceiptBindsTokenAndNeverExtendsAuthority(t *testing.T) {
	now := time.Unix(10000, 0)
	verifier, err := New(config.Core{AttestationReceiptSecret: strings.Repeat("s", 32)}, atproto.NewResolver(nil, "https://plc.directory"))
	if err != nil {
		t.Fatal(err)
	}
	claims := accessClaims{Subject: "did:plc:viewer", Expires: now.Unix() + 300}
	claims.Confirmation.Thumbprint = "thumbprint"
	raw := verifier.issue("token", claims, now, now.Unix()+45)
	until, err := verifier.verifyReceipt(raw, "token", claims, now.Add(10*time.Second))
	if err != nil || until != now.Unix()+45 {
		t.Fatalf("receipt validity: %d, %v", until, err)
	}
	refreshed := verifier.issue("token", claims, now.Add(10*time.Second), until)
	if _, err := verifier.verifyReceipt(refreshed, "token", claims, now.Add(45*time.Second)); err != expiredReceipt {
		t.Fatalf("renewed receipt must retain original expiry, got %v", err)
	}
	if _, err := verifier.verifyReceipt(raw, "different", claims, now); err != invalid {
		t.Fatalf("receipt accepted for different token: %v", err)
	}
}

func TestActivePDSFallbackRequiresAuthorityAndServerConfirmation(t *testing.T) {
	pds := "https://pds.socialwire.dev"
	issuer := "https://issuer.socialwire.dev"
	resolver := atproto.NewResolver(testHTTPDoer(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{
			"service":[{"id":"#atproto_pds","type":"AtprotoPersonalDataServer","serviceEndpoint":"` + pds + `"}]
		}`))}, nil
	}), "https://plc.directory").WithCache(atproto.NewPDSResolutionCache())
	newVerifier := func(namedIssuer bool, responseStatus int) (*Verifier, *int) {
		t.Helper()
		verifier, err := New(config.Core{AttestationReceiptSecret: strings.Repeat("x", 32)}, resolver)
		if err != nil {
			t.Fatal(err)
		}
		probes := 0
		verifier.fetch = func(ctx context.Context, target string, headers http.Header, out any) (int, http.Header, error) {
			var body string
			switch target {
			case pds + "/.well-known/oauth-protected-resource":
				body = `{"authorization_servers":[]}`
				if namedIssuer {
					body = `{"authorization_servers":["` + issuer + `"]}`
				}
			case issuer + "/.well-known/oauth-authorization-server":
				body = `{"issuer":"` + issuer + `","jwks_uri":"/jwks"}`
			case issuer + "/jwks":
				body = `{"keys":[]}`
			case pds + "/xrpc/com.atproto.server.getSession":
				probes++
				if headers.Get("Authorization") == "" || headers.Get("DPoP") == "" {
					t.Error("PDS probe omitted credentials")
				}
				if responseStatus != http.StatusOK {
					nonce := http.Header{}
					nonce.Set("DPoP-Nonce", "retry-nonce")
					return responseStatus, nonce, nil
				}
				body = `{"did":"did:plc:active-viewer","active":true}`
			default:
				t.Errorf("unexpected OAuth lookup: %s", target)
				return 404, nil, nil
			}
			return 200, nil, json.Unmarshal([]byte(body), out)
		}
		return verifier, &probes
	}
	signer, _, _ := key(t)
	private, public, thumbprint := key(t)
	now := time.Now().Unix()
	token := signed(t, signer, map[string]string{"alg": "ES256"}, map[string]any{
		"sub": "did:plc:active-viewer", "iss": issuer, "exp": now + 300, "cnf": map[string]string{"jkt": thumbprint},
	})
	header := map[string]any{"typ": "dpop+jwt", "alg": "ES256", "jwk": public}
	makeProof := func(id, target string) string {
		return signed(t, private, header, proofClaims{ID: id, IssuedAt: now, Method: http.MethodGet,
			URL: target, TokenHash: digestString(token)})
	}
	send := func(verifier *Verifier, proofID, receipt string, prepared bool) *httptest.ResponseRecorder {
		t.Helper()
		router := httpx.NewRouter()
		router.Group(verifier.Middleware).Get("/read", func(w http.ResponseWriter, r *http.Request) error {
			w.WriteHeader(http.StatusNoContent)
			return nil
		})
		req := httptest.NewRequest(http.MethodGet, "https://gateway.social/read", nil)
		req.Header.Set("Authorization", "DPoP "+token)
		req.Header.Set("DPoP", makeProof(proofID, "https://gateway.social/read"))
		req.Header.Set(SessionProofHeader, makeProof("session-"+proofID, pds+"/xrpc/com.atproto.server.getSession"))
		if prepared {
			req.Header.Set(PreparedHeader, "true")
			req.Header.Set(ReceiptHeader, receipt)
		}
		reply := httptest.NewRecorder()
		router.ServeHTTP(reply, req)
		return reply
	}
	verifier, probes := newVerifier(false, http.StatusOK)
	if result := send(verifier, "unbound", "", false); result.Code != 401 || *probes != 0 {
		t.Fatalf("unbound issuer: %d, %d PDS session calls", result.Code, *probes)
	}
	verifier, probes = newVerifier(true, http.StatusUnauthorized)
	if result := send(verifier, "nonce", "", false); result.Code != 401 || *probes != 1 || result.Header().Get(SessionNonceHeader) != "retry-nonce" {
		t.Fatalf("PDS challenge: %d, %d PDS session calls, headers %+v", result.Code, *probes, result.Header())
	}
	verifier, probes = newVerifier(true, http.StatusOK)
	if result := send(verifier, "prepared", "", true); result.Code != 428 || *probes != 0 {
		t.Fatalf("prepared proof should require receipt: %d, %d PDS session calls", result.Code, *probes)
	}
	result := send(verifier, "active", "", false)
	receipt := result.Header().Get(ReceiptHeader)
	if result.Code != 204 || *probes != 1 || receipt == "" {
		t.Fatalf("attested session: %d, %d PDS session calls, headers %+v", result.Code, *probes, result.Header())
	}
	if result := send(verifier, "receipt", receipt, true); result.Code != 204 || *probes != 1 {
		t.Fatalf("receipt should avoid PDS session call: %d, %d PDS session calls", result.Code, *probes)
	}
}

type testHTTPDoer func(*http.Request) (*http.Response, error)

func (do testHTTPDoer) Do(r *http.Request) (*http.Response, error) { return do(r) }
