package oauthclient

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
)

var jwtEncoding = base64.RawURLEncoding
var errKey = errors.New("invalid stored scheduling DPoP key")

type storedDPoPKey struct {
	Type              string `json:"kty"`
	Curve             string `json:"crv"`
	RawRepresentation string `json:"rawRepresentation"`
}

type proofKey struct {
	Type  string `json:"kty"`
	Curve string `json:"crv"`
	X     string `json:"x"`
	Y     string `json:"y"`
}

type proofHeader struct {
	Type      string   `json:"typ"`
	Algorithm string   `json:"alg"`
	Key       proofKey `json:"jwk"`
}

type proofPayload struct {
	ID         string `json:"jti"`
	Method     string `json:"htm"`
	Target     string `json:"htu"`
	IssuedAt   int64  `json:"iat"`
	AccessHash string `json:"ath,omitempty"`
	Nonce      string `json:"nonce,omitempty"`
}

func generateKey() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}

func encodeKey(private *ecdsa.PrivateKey) (string, error) {
	raw, err := private.Bytes()
	if err != nil {
		return "", err
	}
	value := storedDPoPKey{"EC", "P-256", base64.StdEncoding.EncodeToString(raw)}
	body, err := json.Marshal(value)
	return string(body), err
}

func decodeKey(raw string) (*ecdsa.PrivateKey, error) {
	var stored storedDPoPKey
	if len(raw) > 1024 || json.Unmarshal([]byte(raw), &stored) != nil || stored.Type != "EC" || stored.Curve != "P-256" {
		return nil, errKey
	}
	scalar, err := base64.StdEncoding.Strict().DecodeString(stored.RawRepresentation)
	if err != nil || len(scalar) != 32 {
		return nil, errKey
	}
	private, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), scalar)
	if err != nil {
		return nil, errKey
	}
	return private, nil
}

// NewVerifier creates a random, URL-safe value for OAuth state and PKCE.
func NewVerifier() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return jwtEncoding.EncodeToString(raw[:]), nil
}

func sha256URL(raw string) string {
	digest := sha256.Sum256([]byte(raw))
	return jwtEncoding.EncodeToString(digest[:])
}

func proof(private *ecdsa.PrivateKey, method string, target *url.URL, token, nonce string) (string, error) {
	id, err := NewVerifier()
	if err != nil {
		return "", err
	}
	htu := *target
	htu.RawQuery, htu.Fragment, htu.RawFragment = "", "", ""
	htu.ForceQuery = false
	public := proofKey{
		Type: "EC", Curve: "P-256",
		X: jwtEncoding.EncodeToString(private.X.FillBytes(make([]byte, 32))),
		Y: jwtEncoding.EncodeToString(private.Y.FillBytes(make([]byte, 32))),
	}
	head, err := json.Marshal(proofHeader{Type: "dpop+jwt", Algorithm: "ES256", Key: public})
	if err != nil {
		return "", err
	}
	claims := proofPayload{ID: id, Method: strings.ToUpper(method), Target: htu.String(), IssuedAt: time.Now().Unix(), Nonce: nonce}
	if token != "" {
		claims.AccessHash = sha256URL(token)
	}
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	input := jwtEncoding.EncodeToString(head) + "." + jwtEncoding.EncodeToString(body)
	digest := sha256.Sum256([]byte(input))
	r, s, err := ecdsa.Sign(rand.Reader, private, digest[:])
	if err != nil {
		return "", err
	}
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	return input + "." + jwtEncoding.EncodeToString(signature), nil
}
