package podcasts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
)

var ErrInvalidRequest = errors.New("invalid podcast request")

func decodeRequired(data []byte, target any, required ...string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, key := range required {
		value, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return ErrInvalidRequest
		}
	}
	return json.Unmarshal(data, target)
}

func PrivateURLAllowed(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() != "" && parsed.User == nil
}

func Identity(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func pointer[T any](value T) *T { return &value }

func extractBlobLink(raw any) string {
	record, _ := raw.(map[string]any)
	ref, _ := record["ref"].(map[string]any)
	if value, _ := ref["$link"].(string); value != "" {
		return value
	}
	value, _ := record["cid"].(string)
	return value
}

func buildSyncGetBlobURL(base, did, cid string) string {
	if !PrivateURLAllowed(base) || did == "" || cid == "" {
		return ""
	}
	return base + "/xrpc/com.atproto.sync.getBlob?" + url.Values{"did": {did}, "cid": {cid}}.Encode()
}
