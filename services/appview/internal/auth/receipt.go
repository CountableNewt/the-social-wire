package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	ReceiptHeader      = "X-ATProto-Session-Attestation-Receipt"
	RequiredHeader     = "X-ATProto-Session-Attestation-Required"
	PreparedHeader     = "X-ATProto-Upstream-DPoP-Prepared"
	SessionProofHeader = "X-ATProto-Session-DPoP"
	SessionNonceHeader = "X-ATProto-Session-DPoP-Nonce"
	UpstreamHeader     = "X-ATProto-Upstream-DPoP"
)

var expiredReceipt = errors.New("attestation expired")

type receipt struct {
	Version    int    `json:"v"`
	KeyID      string `json:"kid"`
	IssuedAt   int64  `json:"iat"`
	Expiry     int64  `json:"exp"`
	TokenHash  string `json:"ath"`
	Subject    string `json:"sub"`
	Thumbprint string `json:"jkt"`
}

func (v *Verifier) mac(payload string) []byte {
	mac := hmac.New(sha256.New, v.secret)
	mac.Write([]byte(payload))
	return mac.Sum(nil)
}

func (v *Verifier) issue(raw string, claims accessClaims, now time.Time, expiry int64) string {
	value := receipt{1, v.keyID, now.Unix(), expiry, digestString(raw), claims.Subject, claims.Confirmation.Thumbprint}
	body, _ := json.Marshal(value)
	payload := encoding.EncodeToString(body)
	return payload + "." + encoding.EncodeToString(v.mac(payload))
}

func (v *Verifier) verifyReceipt(raw, token string, claims accessClaims, now time.Time) (int64, error) {
	if len(raw) > 4096 {
		return 0, invalid
	}
	payload, signature, ok := strings.Cut(raw, ".")
	mac, err := encoding.DecodeString(signature)
	if !ok || err != nil || !hmac.Equal(mac, v.mac(payload)) {
		return 0, invalid
	}
	body, err := encoding.DecodeString(payload)
	var value receipt
	if err != nil || json.Unmarshal(body, &value) != nil || value.Version != 1 || value.KeyID != v.keyID ||
		value.Subject != claims.Subject || value.Thumbprint != claims.Confirmation.Thumbprint ||
		value.TokenHash != digestString(token) || value.IssuedAt > now.Unix()+5 ||
		value.Expiry <= value.IssuedAt || value.Expiry > value.IssuedAt+60 || value.Expiry > claims.Expires {
		return 0, invalid
	}
	if value.Expiry <= now.Unix() {
		return 0, expiredReceipt
	}
	return value.Expiry, nil
}
