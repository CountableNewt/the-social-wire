// Package auth authenticates standalone AppView requests using AT Protocol OAuth.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
	"golang.org/x/sync/singleflight"
)

type accessClaims struct {
	Subject           string          `json:"sub"`
	Issuer            string          `json:"iss"`
	Expires           int64           `json:"exp"`
	NotBefore         int64           `json:"nbf"`
	ClientID          string          `json:"client_id"`
	AlternateClientID string          `json:"clientId"`
	AuthorizedParty   string          `json:"azp"`
	Audience          json.RawMessage `json:"aud"`
	Confirmation      struct {
		Thumbprint string `json:"jkt"`
	} `json:"cnf"`
}

// Verifier holds process-local replay protection and bounded authority caches.
type Verifier struct {
	fetch          func(context.Context, string, http.Header, any) (int, http.Header, error)
	resolver       *atproto.Resolver
	policy         config.OAuthGatewayPolicy
	supplemental   keySet
	secret         []byte
	keyID          string
	mu             sync.Mutex
	authorities    map[string]authority
	replays        map[string]int64
	attested       map[string]int64
	replayPruneAt  int64
	discovery      chan struct{}
	discoveryGroup singleflight.Group
	sessionOrigins map[string]int
	session        chan struct{}
}

// New configures DPoP-bound access-token verification.
func New(core config.Core, resolver *atproto.Resolver) (*Verifier, error) {
	if resolver == nil {
		return nil, errors.New("OAuth verifier requires an ATProto resolver")
	}
	if len(core.AttestationReceiptSecret) < 32 || len(core.AttestationReceiptSecret) > 4096 {
		return nil, errors.New("PDS_ATTESTATION_RECEIPT_SECRET must contain 32–4096 bytes")
	}
	verifier := &Verifier{
		fetch: fetch, resolver: resolver, policy: core.OAuthGateway, secret: []byte(core.AttestationReceiptSecret),
		keyID: digestString(core.AttestationReceiptSecret)[:16], authorities: make(map[string]authority),
		replays: make(map[string]int64), attested: make(map[string]int64), discovery: make(chan struct{}, 32),
		session: make(chan struct{}, 64), sessionOrigins: make(map[string]int),
	}
	if strings.TrimSpace(core.SupplementalJWKSJSON) != "" &&
		(json.Unmarshal([]byte(core.SupplementalJWKSJSON), &verifier.supplemental) != nil || len(verifier.supplemental.Keys) > 64) {
		return nil, errors.New("invalid OAUTH_ACCESS_TOKEN_SUPPLEMENTAL_JWKS_JSON")
	}
	return verifier, nil
}

func (v *Verifier) enforcePolicy(claims accessClaims) error {
	if !v.policy.RequireKnownClient {
		return nil
	}
	if len(v.policy.AllowedClientIDs) == 0 && len(v.policy.AllowedAudiences) == 0 {
		return httpx.Errorf(http.StatusForbidden, "Gateway client policy is enabled but no allowlists are configured")
	}
	client := strings.TrimSpace(claims.ClientID)
	if client == "" {
		client = strings.TrimSpace(claims.AlternateClientID)
	}
	for _, candidate := range []string{client, strings.TrimSpace(claims.AuthorizedParty)} {
		if _, ok := v.policy.AllowedClientIDs[candidate]; ok {
			return nil
		}
	}
	var audiences []string
	var single string
	if json.Unmarshal(claims.Audience, &single) == nil {
		audiences = []string{single}
	} else if json.Unmarshal(claims.Audience, &audiences) != nil {
		audiences = nil
	}
	for _, audience := range audiences {
		if _, ok := v.policy.AllowedAudiences[audience]; ok {
			return nil
		}
	}
	return httpx.Errorf(http.StatusForbidden, "OAuth client is not authorized for this gateway")
}

func (v *Verifier) consume(thumbprint string, proof proofClaims, now int64) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.replayPruneAt <= now {
		for key, expiry := range v.replays {
			if expiry <= now {
				delete(v.replays, key)
			}
		}
		v.replayPruneAt = now + 1
	}
	key := thumbprint + "\x00" + proof.ID
	if v.replays[key] > now || len(v.replays) >= 100_000 {
		return false
	}
	v.replays[key] = proof.IssuedAt + 121
	return true
}

// Middleware verifies the token and request binding before exposing the viewer.
func (v *Verifier) Middleware(next httpx.Handler) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		authorization := strings.TrimSpace(r.Header.Get("Authorization"))
		if authorization == "" {
			return httpx.Errorf(http.StatusUnauthorized, "Missing Authorization header")
		}
		scheme, rawToken, ok := strings.Cut(authorization, " ")
		if !ok || (!strings.EqualFold(scheme, "DPoP") && !strings.EqualFold(scheme, "Bearer")) {
			return httpx.Errorf(http.StatusUnauthorized, "Authorization header must prefix DPoP or Bearer")
		}
		rawToken = strings.TrimSpace(rawToken)
		rawProof := strings.TrimSpace(r.Header.Get("DPoP"))
		if rawProof == "" {
			return httpx.Errorf(http.StatusUnauthorized, "Missing RFC 9449 DPoP proof header")
		}
		rejected := httpx.Errorf(http.StatusUnauthorized, "Invalid or stale ATProto OAuth access token")
		token, err := parseJWT(rawToken)
		var claims accessClaims
		now := time.Now()
		if err != nil || json.Unmarshal(token.payload, &claims) != nil ||
			claims.Expires <= now.Unix() || claims.NotBefore > now.Unix() ||
			len(claims.Subject) > 2048 || !atproto.IsValidDID(claims.Subject) ||
			strings.ContainsAny(claims.Subject, "/\\%?# \t\r\n") || claims.Issuer == "" {
			return rejected
		}
		proof, err := verifyProof(rawProof, r.Method, requestTarget(r), rawToken, claims.Confirmation.Thumbprint, now)
		if err != nil {
			return rejected
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		var receipt, nonce string
		if !v.supplemental.verifies(token) {
			issuer, ok := atproto.ValidatedPDSBase(claims.Issuer, atproto.EndpointPublicHTTPS)
			if !ok {
				return rejected
			}
			authority, err := v.authority(ctx, claims.Subject, issuer, now, false)
			if errors.Is(err, dependency) {
				return dependencyError()
			}
			if err != nil {
				return rejected
			}
			verified := authority.keys.verifies(token)
			if !verified && len(authority.keys.Keys) > 0 && now.Sub(authority.fetchedAt) >= 30*time.Second {
				refreshed, err := v.authority(ctx, claims.Subject, issuer, now, true)
				if errors.Is(err, dependency) {
					return dependencyError()
				}
				if err != nil {
					return rejected
				}
				// A signature rejection against known keys cannot itself permit
				// active fallback, even if refreshed metadata has no keys.
				if !refreshed.keys.verifies(token) {
					return rejected
				}
				verified = true
			}
			if !verified {
				if len(authority.keys.Keys) != 0 || len(v.supplemental.Keys) != 0 || !authority.attestable ||
					!strings.HasPrefix(claims.Subject, "did:plc:") {
					return rejected
				}
				receipt, nonce, err = v.attest(ctx, r, authority, rawToken, authorization, claims, now)
				switch {
				case errors.Is(err, expiredReceipt):
					w.Header().Set(RequiredHeader, "true")
					return httpx.JSON(w, http.StatusPreconditionRequired, map[string]string{"error": "ATProtoSessionAttestationRequired"})
				case errors.Is(err, dependency):
					return dependencyError()
				case err != nil && nonce != "":
					w.Header().Set(SessionNonceHeader, nonce)
					w.WriteHeader(http.StatusUnauthorized)
					return nil
				case err != nil:
					return rejected
				}
			}
		}
		if err := v.enforcePolicy(claims); err != nil {
			return err
		}
		verifiedAt := time.Now().Unix()
		if claims.Expires <= verifiedAt || proof.IssuedAt+120 < verifiedAt {
			return rejected
		}
		if !v.consume(claims.Confirmation.Thumbprint, proof, verifiedAt) {
			return httpx.Errorf(http.StatusUnauthorized, "Invalid DPoP proof for this request")
		}
		if receipt != "" {
			w.Header().Set(ReceiptHeader, receipt)
		}
		if nonce != "" {
			w.Header().Set(SessionNonceHeader, nonce)
		}
		viewer := &reqctx.Auth{DID: claims.Subject, AuthorizationForwardingValue: authorization, DPoPProof: rawProof,
			UpstreamDPoPProof: strings.TrimSpace(r.Header.Get(UpstreamHeader))}
		return next(w, r.WithContext(reqctx.WithAuth(r.Context(), viewer)))
	}
}

func dependencyError() error {
	return httpx.Errorf(http.StatusServiceUnavailable, "Authentication service is temporarily unavailable.")
}

func (v *Verifier) attest(ctx context.Context, r *http.Request, authority authority, rawToken, authorization string, claims accessClaims, now time.Time) (string, string, error) {
	if strings.TrimSpace(r.Header.Get(UpstreamHeader)) != "" || strings.EqualFold(strings.TrimSpace(r.Header.Get(PreparedHeader)), "true") {
		prior := strings.TrimSpace(r.Header.Get(ReceiptHeader))
		if prior == "" {
			return "", "", expiredReceipt
		}
		expiry, err := v.verifyReceipt(prior, rawToken, claims, now)
		if err != nil {
			return "", "", err
		}
		return v.issue(rawToken, claims, now, expiry), "", nil
	}
	key := digestString(rawToken)
	v.mu.Lock()
	expiry := v.attested[key]
	v.mu.Unlock()
	if expiry > now.Unix() {
		return v.issue(rawToken, claims, now, expiry), "", nil
	}
	session := strings.TrimSpace(r.Header.Get(SessionProofHeader))
	target := authority.pds + "/xrpc/com.atproto.server.getSession"
	if session == "" || len(session) > 8192 {
		return "", "", invalid
	}
	if _, err := verifyProof(session, http.MethodGet, target, rawToken, claims.Confirmation.Thumbprint, now); err != nil {
		return "", "", err
	}
	select {
	case v.session <- struct{}{}:
		defer func() { <-v.session }()
	default:
		return "", "", dependency
	}
	v.mu.Lock()
	if v.sessionOrigins[authority.pds] >= 8 {
		v.mu.Unlock()
		return "", "", dependency
	}
	v.sessionOrigins[authority.pds]++
	v.mu.Unlock()
	defer func() {
		v.mu.Lock()
		defer v.mu.Unlock()
		v.sessionOrigins[authority.pds]--
		if v.sessionOrigins[authority.pds] == 0 {
			delete(v.sessionOrigins, authority.pds)
		}
	}()
	headers := http.Header{}
	headers.Set("Authorization", authorization)
	headers.Set("DPoP", session)
	var response struct {
		DID    string `json:"did"`
		Active *bool  `json:"active"`
	}
	status, headers, err := v.fetch(ctx, target, headers, &response)
	if err != nil {
		return "", "", err
	}
	nonce := strings.TrimSpace(headers.Get("DPoP-Nonce"))
	if len(nonce) > 1024 || strings.ContainsAny(nonce, ",\r\n") {
		nonce = ""
	}
	if slices.Contains([]int{http.StatusBadRequest, http.StatusUnauthorized}, status) && nonce != "" {
		return "", nonce, invalid
	}
	if status != http.StatusOK || response.DID != claims.Subject || (response.Active != nil && !*response.Active) {
		return "", "", invalid
	}
	expiry = min(claims.Expires, now.Unix()+60)
	v.mu.Lock()
	if len(v.attested) >= 10_000 {
		for key, value := range v.attested {
			if value <= now.Unix() {
				delete(v.attested, key)
			}
		}
	}
	if len(v.attested) < 10_000 {
		v.attested[key] = expiry
	}
	v.mu.Unlock()
	return v.issue(rawToken, claims, now, expiry), nonce, nil
}
