package readstate

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
)

// canonicalBytes is ReadStateV2CanonicalHash.bytes: restricted DAG-CBOR for v2
// intent/receipt values, without links, floats or lossy JSON bridges.
func canonicalBytes(value JSONValue) ([]byte, error) {
	var data []byte
	header := func(major byte, value uint64) {
		data = appendCBORHeader(data, major, value)
	}
	var appendValue func(JSONValue, int) error
	appendValue = func(value JSONValue, depth int) error {
		if depth > 32 {
			return ErrSizeLimit
		}
		switch value.Kind {
		case JSONNull:
			data = append(data, 0xf6)
		case JSONBool:
			if value.Bool {
				data = append(data, 0xf5)
			} else {
				data = append(data, 0xf4)
			}
		case JSONInteger:
			if value.Integer < -MaximumSequence || value.Integer > MaximumSequence {
				return ErrInvalidRecord
			}
			if value.Integer < 0 {
				header(1, uint64(-1-value.Integer))
			} else {
				header(0, uint64(value.Integer))
			}
		case JSONString:
			header(3, uint64(len(value.String)))
			data = append(data, value.String...)
		case JSONArray:
			header(4, uint64(len(value.Array)))
			for _, item := range value.Array {
				if err := appendValue(item, depth+1); err != nil {
					return err
				}
			}
		case JSONObject:
			header(5, uint64(len(value.Object)))
			for _, key := range cborSortedKeys(value.Object) {
				if err := appendValue(jsonString(key), depth+1); err != nil {
					return err
				}
				if err := appendValue(value.Object[key], depth+1); err != nil {
					return err
				}
			}
		}
		if len(data) > 16*1024*1024 {
			return ErrSizeLimit
		}
		return nil
	}
	if err := appendValue(value, 0); err != nil {
		return nil, err
	}
	return data, nil
}

// canonicalHash is the lowercase hex SHA-256 of canonicalBytes.
func canonicalHash(value JSONValue) (string, error) {
	data, err := canonicalBytes(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// appendCBORHeader writes a minimal-length CBOR major type header.
func appendCBORHeader(data []byte, major byte, value uint64) []byte {
	if value < 24 {
		return append(data, major<<5|byte(value))
	}
	count, additional := 8, byte(27)
	switch {
	case value <= 0xff:
		count, additional = 1, 24
	case value <= 0xffff:
		count, additional = 2, 25
	case value <= 0xffffffff:
		count, additional = 4, 26
	}
	data = append(data, major<<5|additional)
	for shift := (count - 1) * 8; shift >= 0; shift -= 8 {
		data = append(data, byte(value>>uint(shift)))
	}
	return data
}

// cborSortedKeys orders keys by UTF-8 length, then bytewise (DAG-CBOR).
func cborSortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) < len(keys[j])
		}
		return keys[i] < keys[j]
	})
	return keys
}
