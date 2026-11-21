package readstate

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math"
	"strings"
)

// VerifyRecordCID is ReadStateRecordCID.verify: bounded canonical DAG-CBOR
// encoding of the original JSON record, including unknown fields, hashed and
// compared with a CIDv1 dag-cbor sha2-256 base32 CID.
// https://atproto.com/specs/data-model
func VerifyRecordCID(data []byte, cid string) error {
	if len(data) > MaximumRecordBytes {
		return ErrSizeLimit
	}
	expected, err := decodeCID(cid)
	if err != nil {
		return err
	}
	if len(expected) != 36 || !bytes.Equal(expected[:4], []byte{1, 0x71, 0x12, 0x20}) {
		return ErrInvalidReference
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return decodeErr(err.Error())
	}
	if decoder.More() {
		return decodeErr("trailing data")
	}
	if _, ok := value.(map[string]any); !ok {
		return ErrInvalidRecord
	}
	var encoded []byte
	if err := encodeCBOR(value, &encoded, 0); err != nil {
		return err
	}
	sum := sha256.Sum256(encoded)
	if !bytes.Equal(sum[:], expected[4:]) {
		return ErrInvalidReference
	}
	return nil
}

func encodeCBOR(value any, out *[]byte, depth int) error {
	if depth > 32 {
		return ErrSizeLimit
	}
	switch typed := value.(type) {
	case string:
		*out = appendCBORHeader(*out, 3, uint64(len(typed)))
		*out = append(*out, typed...)
	case bool:
		if typed {
			*out = append(*out, 0xf5)
		} else {
			*out = append(*out, 0xf4)
		}
	case json.Number:
		number, err := typed.Float64()
		if err != nil || math.IsInf(number, 0) || math.IsNaN(number) || number != math.Round(number) ||
			math.Abs(number) > 9_007_199_254_740_991 {
			return ErrInvalidRecord
		}
		integer := int64(number)
		if integer >= 0 {
			*out = appendCBORHeader(*out, 0, uint64(integer))
		} else {
			*out = appendCBORHeader(*out, 1, uint64(-1-integer))
		}
	case nil:
		*out = append(*out, 0xf6)
	case []any:
		*out = appendCBORHeader(*out, 4, uint64(len(typed)))
		for _, item := range typed {
			if err := encodeCBOR(item, out, depth+1); err != nil {
				return err
			}
		}
	case map[string]any:
		if link, ok := typed["$link"].(string); ok && len(typed) == 1 {
			cid, err := decodeCID(link)
			if err != nil {
				return err
			}
			payload := append([]byte{0}, cid...)
			*out = appendCBORHeader(*out, 6, 42)
			*out = appendCBORHeader(*out, 2, uint64(len(payload)))
			*out = append(*out, payload...)
		} else if encoded, ok := typed["$bytes"].(string); ok && len(typed) == 1 {
			padding := strings.Repeat("=", (4-len([]rune(encoded))%4)%4)
			decoded, err := base64.StdEncoding.DecodeString(encoded + padding)
			if err != nil {
				return ErrInvalidRecord
			}
			*out = appendCBORHeader(*out, 2, uint64(len(decoded)))
			*out = append(*out, decoded...)
		} else {
			*out = appendCBORHeader(*out, 5, uint64(len(typed)))
			for _, key := range cborSortedKeys(typed) {
				if err := encodeCBOR(key, out, depth+1); err != nil {
					return err
				}
				if err := encodeCBOR(typed[key], out, depth+1); err != nil {
					return err
				}
			}
		}
	default:
		return ErrInvalidRecord
	}
	if len(*out) > MaximumRecordBytes {
		return ErrSizeLimit
	}
	return nil
}

// decodeCID decodes a base32 CIDv1 (raw or dag-cbor, sha2-256).
func decodeCID(value string) ([]byte, error) {
	if len(value) != 59 || value[0] != 'b' {
		return nil, ErrInvalidReference
	}
	const alphabet = "abcdefghijklmnopqrstuvwxyz234567"
	var buffer uint32
	bits := 0
	result := make([]byte, 0, 36)
	for index := 1; index < len(value); index++ {
		position := strings.IndexByte(alphabet, value[index])
		if position < 0 {
			return nil, ErrInvalidReference
		}
		buffer = buffer<<5 | uint32(position)
		bits += 5
		if bits >= 8 {
			bits -= 8
			result = append(result, byte(buffer>>uint(bits)))
			buffer &= (1 << uint(bits)) - 1
		}
	}
	if buffer != 0 || len(result) != 36 || result[0] != 1 || (result[1] != 0x55 && result[1] != 0x71) ||
		result[2] != 0x12 || result[3] != 0x20 {
		return nil, ErrInvalidReference
	}
	return result, nil
}
