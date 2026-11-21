package auth

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type proofClaims struct {
	ID        string `json:"jti"`
	IssuedAt  int64  `json:"iat"`
	Expires   int64  `json:"exp"`
	Method    string `json:"htm"`
	URL       string `json:"htu"`
	TokenHash string `json:"ath"`
}

func requestTarget(r *http.Request) string {
	scheme := "https"
	host := (&url.URL{Host: r.Host}).Hostname()
	if r.TLS == nil && (strings.EqualFold(host, "localhost") || strings.HasPrefix(host, "127.") || host == "::1") {
		scheme = "http"
	}
	if raw, _, _ := strings.Cut(r.Header.Get("X-Forwarded-Proto"), ","); strings.TrimSpace(raw) != "" {
		scheme = strings.ToLower(strings.TrimSpace(raw))
	}
	if scheme != "https" && scheme != "http" {
		return ""
	}
	return (&url.URL{Scheme: scheme, Host: r.Host, Path: r.URL.Path, RawPath: r.URL.RawPath}).String()
}

func verifyProof(raw, method, target, accessToken, thumbprint string, now time.Time) (proofClaims, error) {
	var claims proofClaims
	proof, err := parseJWT(raw)
	if err != nil || !strings.EqualFold(proof.header.Type, "dpop+jwt") || proof.header.Algorithm != "ES256" ||
		!proof.verify(proof.header.Key) || json.Unmarshal(proof.payload, &claims) != nil {
		return claims, invalid
	}
	key := proof.header.Key
	jkt := digestString(`{"crv":"P-256","kty":"EC","x":"` + key.X + `","y":"` + key.Y + `"}`)
	if jkt != thumbprint || target == "" || claims.URL != target || claims.Method != method ||
		claims.TokenHash != digestString(accessToken) || strings.TrimSpace(claims.ID) == "" || len(claims.ID) > 256 ||
		claims.IssuedAt < now.Unix()-120 || claims.IssuedAt > now.Unix()+120 || (claims.Expires != 0 && claims.Expires <= now.Unix()-120) {
		return claims, invalid
	}
	return claims, nil
}
