package repo

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
)

// ErrInvalidPublicRecord mirrors PublicContentDiscoveryError.invalidResponse as
// thrown by Swift's PublicContentRecordCID; callers map it to their own error.
var ErrInvalidPublicRecord = errors.New("invalid public record")

const (
	publicRecordMaximumBytes  = 1 << 20
	publicRecordMaximumDepth  = 32
	publicRecordMaximumValues = 100_000
)

// VerifyPublicRecordCID checks that a public record's JSON value re-encodes to
// DAG-CBOR whose CID equals cid. It is bounded separately from the smaller
// read-state codec and does not establish repository authority. A malformed
// cid (or $link) string yields a SnapshotError; everything else that fails
// yields ErrInvalidPublicRecord.
func VerifyPublicRecordCID(recordJSON []byte, cid string) error {
	if len(recordJSON) > publicRecordMaximumBytes {
		return ErrInvalidPublicRecord
	}
	expected, err := ParseCID(cid)
	if err != nil {
		return err
	}
	if !expected.IsDAGCBOR() {
		return ErrInvalidPublicRecord
	}
	decoder := json.NewDecoder(bytes.NewReader(recordJSON))
	decoder.UseNumber()
	var parsed any
	if err := decoder.Decode(&parsed); err != nil {
		return ErrInvalidPublicRecord
	}
	if _, err := decoder.Token(); err != io.EOF {
		return ErrInvalidPublicRecord
	}
	object, ok := parsed.(map[string]any)
	if !ok {
		return ErrInvalidPublicRecord
	}
	remaining := publicRecordMaximumValues
	value, err := convertPublicRecordJSON(object, 0, &remaining)
	if err != nil {
		return err
	}
	encoded := EncodeDAGCBOR(value)
	if len(encoded) > publicRecordMaximumBytes || HashCID(encoded) != expected {
		return ErrInvalidPublicRecord
	}
	return nil
}

func convertPublicRecordJSON(value any, depth int, remaining *int) (Value, error) {
	*remaining--
	if depth > publicRecordMaximumDepth || *remaining < 0 {
		return Value{}, ErrInvalidPublicRecord
	}
	switch typed := value.(type) {
	case string:
		return StringValue(typed), nil
	case bool:
		return BoolValue(typed), nil
	case json.Number:
		number, err := strconv.ParseFloat(typed.String(), 64)
		if err != nil || math.IsInf(number, 0) || math.IsNaN(number) || math.Round(number) != number ||
			math.Abs(number) > 9_007_199_254_740_991 {
			return Value{}, ErrInvalidPublicRecord
		}
		integer := int64(number)
		if integer >= 0 {
			return UnsignedValue(uint64(integer)), nil
		}
		return NegativeValue(uint64(-1 - integer)), nil
	case nil:
		return NullValue(), nil
	case []any:
		items := make([]Value, 0, len(typed))
		for _, item := range typed {
			converted, err := convertPublicRecordJSON(item, depth+1, remaining)
			if err != nil {
				return Value{}, err
			}
			items = append(items, converted)
		}
		return ArrayValue(items), nil
	case map[string]any:
		if len(typed) == 1 {
			if link, ok := typed["$link"].(string); ok {
				cid, err := ParseCID(link)
				if err != nil {
					return Value{}, err
				}
				return LinkValue(cid), nil
			}
			if encoded, ok := typed["$bytes"].(string); ok {
				padding := strings.Repeat("=", (4-len(encoded)%4)%4)
				if strings.ContainsAny(encoded, "\r\n") {
					return Value{}, ErrInvalidPublicRecord
				}
				decoded, err := base64.StdEncoding.DecodeString(encoded + padding)
				if err != nil {
					return Value{}, ErrInvalidPublicRecord
				}
				return BytesValue(decoded), nil
			}
		}
		result := make(map[string]Value, len(typed))
		for key, item := range typed {
			converted, err := convertPublicRecordJSON(item, depth+1, remaining)
			if err != nil {
				return Value{}, err
			}
			result[key] = converted
		}
		return MapValue(result), nil
	}
	return Value{}, ErrInvalidPublicRecord
}
