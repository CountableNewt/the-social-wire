package atproto

import (
	"encoding/base64"
	"errors"
	"strings"
)

// ErrInvalidBase64URL is Base64URLDecodingError.invalidAlphabetLength.
var ErrInvalidBase64URL = errors.New("atproto: invalid base64url")

// DecodeBase64URL decodes base64url with or without padding.
func DecodeBase64URL(value string) ([]byte, error) {
	padded := strings.NewReplacer("-", "+", "_", "/").Replace(value)
	if mod := len(padded) % 4; mod != 0 {
		padded += strings.Repeat("=", 4-mod)
	}
	data, err := base64.StdEncoding.DecodeString(padded)
	if err != nil {
		return nil, ErrInvalidBase64URL
	}
	return data, nil
}

// EncodeBase64URL encodes data as unpadded base64url.
func EncodeBase64URL(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}
