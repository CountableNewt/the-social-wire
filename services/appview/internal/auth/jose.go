package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	_ "crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
)

var invalid = errors.New("invalid OAuth credentials")
var encoding = base64.RawURLEncoding.Strict()

type jwk struct {
	Type      string `json:"kty"`
	Curve     string `json:"crv"`
	X         string `json:"x"`
	Y         string `json:"y"`
	N         string `json:"n"`
	E         string `json:"e"`
	ID        string `json:"kid"`
	Algorithm string `json:"alg"`
	Use       string `json:"use"`
	D         string `json:"d"`
}

type keySet struct {
	Keys []jwk `json:"keys"`
}

type joseHeader struct {
	Type      string   `json:"typ"`
	Algorithm string   `json:"alg"`
	ID        string   `json:"kid"`
	Key       jwk      `json:"jwk"`
	Critical  []string `json:"crit"`
}

type jwt struct {
	header    joseHeader
	payload   []byte
	signed    []byte
	signature []byte
}

func parseJWT(raw string) (jwt, error) {
	var result jwt
	parts := strings.Split(raw, ".")
	if len(raw) > 32768 || len(parts) != 3 {
		return result, invalid
	}
	header, err := encoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(header, &result.header) != nil || len(result.header.Critical) != 0 {
		return result, invalid
	}
	result.payload, err = encoding.DecodeString(parts[1])
	if err != nil {
		return result, invalid
	}
	result.signature, err = encoding.DecodeString(parts[2])
	if err != nil || len(result.signature) == 0 {
		return result, invalid
	}
	result.signed = []byte(parts[0] + "." + parts[1])
	return result, nil
}

func (token jwt) verify(key jwk) bool {
	alg := token.header.Algorithm
	if key.D != "" || (key.Use != "" && key.Use != "sig") || (key.Algorithm != "" && key.Algorithm != alg) {
		return false
	}
	x, xErr := encoding.DecodeString(key.X)
	if alg == "EdDSA" {
		return key.Type == "OKP" && key.Curve == "Ed25519" && xErr == nil && len(x) == ed25519.PublicKeySize &&
			ed25519.Verify(ed25519.PublicKey(x), token.signed, token.signature)
	}
	var hash crypto.Hash
	switch alg {
	case "ES256", "RS256", "PS256":
		hash = crypto.SHA256
	case "ES384", "RS384", "PS384":
		hash = crypto.SHA384
	case "ES512", "RS512", "PS512":
		hash = crypto.SHA512
	default:
		return false
	}
	hasher := hash.New()
	hasher.Write(token.signed)
	digest := hasher.Sum(nil)
	if strings.HasPrefix(alg, "ES") {
		curve := map[string]elliptic.Curve{"ES256": elliptic.P256(), "ES384": elliptic.P384(), "ES512": elliptic.P521()}[alg]
		curveName := map[string]string{"ES256": "P-256", "ES384": "P-384", "ES512": "P-521"}[alg]
		size := (curve.Params().BitSize + 7) / 8
		y, yErr := encoding.DecodeString(key.Y)
		if key.Type != "EC" || key.Curve != curveName || xErr != nil || yErr != nil ||
			len(x) != size || len(y) != size || len(token.signature) != size*2 {
			return false
		}
		public := ecdsa.PublicKey{Curve: curve, X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
		return curve.IsOnCurve(public.X, public.Y) && ecdsa.Verify(&public, digest,
			new(big.Int).SetBytes(token.signature[:size]), new(big.Int).SetBytes(token.signature[size:]))
	}
	n, nErr := encoding.DecodeString(key.N)
	e, eErr := encoding.DecodeString(key.E)
	if key.Type != "RSA" || nErr != nil || eErr != nil || len(n) < 256 || len(n) > 1024 || len(e) == 0 || len(e) > 4 {
		return false
	}
	public := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	if strings.HasPrefix(alg, "PS") {
		return rsa.VerifyPSS(public, hash, digest, token.signature, &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash}) == nil
	}
	return rsa.VerifyPKCS1v15(public, hash, digest, token.signature) == nil
}

func (keys keySet) verifies(token jwt) bool {
	for _, key := range keys.Keys {
		if (token.header.ID == "" || token.header.ID == key.ID) && token.verify(key) {
			return true
		}
	}
	return false
}

func digestString(input string) string {
	digest := sha256.Sum256([]byte(input))
	return encoding.EncodeToString(digest[:])
}
