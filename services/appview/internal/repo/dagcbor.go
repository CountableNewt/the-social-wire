package repo

import (
	"bytes"
	"math"
	"slices"
	"unicode/utf8"
)

const dagCBORMaximumDepth = 128

// DecodeDAGCBOR decodes one strictly canonical DAG-CBOR value. Trailing bytes or
// any encoding that does not round-trip byte-for-byte are rejected.
func DecodeDAGCBOR(data []byte) (Value, error) {
	reader := newBinaryCursor(data)
	value, err := decodeDAGCBOR(reader, 0)
	if err != nil {
		return Value{}, err
	}
	if reader.remaining() != 0 || !bytes.Equal(EncodeDAGCBOR(value), data) {
		return Value{}, Invalid("noncanonical DAG-CBOR")
	}
	return value, nil
}

// EncodeDAGCBOR encodes value canonically: minimal integer heads, map keys
// sorted by UTF-8 length then bytewise, floats always as 64-bit.
func EncodeDAGCBOR(value Value) []byte {
	return appendDAGCBOR(nil, value)
}

func appendDAGCBOR(out []byte, value Value) []byte {
	switch value.Kind {
	case KindMap:
		out = appendCBORHeader(out, 5, uint64(len(value.Map)))
		keys := make([]string, 0, len(value.Map))
		for key := range value.Map {
			keys = append(keys, key)
		}
		slices.SortFunc(keys, func(a, b string) int {
			if len(a) != len(b) {
				return len(a) - len(b)
			}
			if a < b {
				return -1
			}
			if a > b {
				return 1
			}
			return 0
		})
		for _, key := range keys {
			out = appendCBORHeader(out, 3, uint64(len(key)))
			out = append(out, key...)
			out = appendDAGCBOR(out, value.Map[key])
		}
	case KindArray:
		out = appendCBORHeader(out, 4, uint64(len(value.Array)))
		for _, item := range value.Array {
			out = appendDAGCBOR(out, item)
		}
	case KindString:
		out = appendCBORHeader(out, 3, uint64(len(value.Str)))
		out = append(out, value.Str...)
	case KindBytes:
		out = appendCBORHeader(out, 2, uint64(len(value.Bytes)))
		out = append(out, value.Bytes...)
	case KindLink:
		out = appendCBORHeader(out, 6, 42)
		out = appendCBORHeader(out, 2, 37)
		out = append(out, 0)
		out = append(out, value.Link[:]...)
	case KindUnsigned:
		out = appendCBORHeader(out, 0, value.Uint)
	case KindNegative:
		out = appendCBORHeader(out, 1, value.Uint)
	case KindDouble:
		out = append(out, 7<<5|27)
		out = appendBigEndian(out, math.Float64bits(value.Float), 8)
	case KindBool:
		if value.Bool {
			out = append(out, 0xf5)
		} else {
			out = append(out, 0xf4)
		}
	default:
		out = append(out, 0xf6)
	}
	return out
}

func appendCBORHeader(out []byte, major byte, value uint64) []byte {
	if value < 24 {
		return append(out, major<<5|byte(value))
	}
	switch {
	case value <= 0xff:
		return appendBigEndian(append(out, major<<5|24), value, 1)
	case value <= 0xffff:
		return appendBigEndian(append(out, major<<5|25), value, 2)
	case value <= 0xffffffff:
		return appendBigEndian(append(out, major<<5|26), value, 4)
	default:
		return appendBigEndian(append(out, major<<5|27), value, 8)
	}
}

func appendBigEndian(out []byte, value uint64, width int) []byte {
	for shift := (width - 1) * 8; shift >= 0; shift -= 8 {
		out = append(out, byte(value>>shift))
	}
	return out
}

func decodeDAGCBOR(reader *binaryCursor, depth int) (Value, error) {
	if depth > dagCBORMaximumDepth {
		return Value{}, Invalid("DAG-CBOR nesting exceeded")
	}
	head, err := reader.integer(1)
	if err != nil {
		return Value{}, err
	}
	major := head >> 5
	additional := head & 31
	if major == 7 {
		switch additional {
		case 20:
			return BoolValue(false), nil
		case 21:
			return BoolValue(true), nil
		case 22:
			return NullValue(), nil
		case 27:
			bits, err := reader.integer(8)
			if err != nil {
				return Value{}, err
			}
			value := math.Float64frombits(bits)
			if math.IsInf(value, 0) || math.IsNaN(value) {
				return Value{}, Invalid("nonfinite number")
			}
			return DoubleValue(value), nil
		default:
			return Value{}, Invalid("invalid CBOR simple value")
		}
	}
	var length uint64
	switch {
	case additional <= 23:
		length = additional
	case additional == 24:
		length, err = reader.integer(1)
	case additional == 25:
		length, err = reader.integer(2)
	case additional == 26:
		length, err = reader.integer(4)
	case additional == 27:
		length, err = reader.integer(8)
	default:
		return Value{}, Invalid("indefinite CBOR")
	}
	if err != nil {
		return Value{}, err
	}
	switch major {
	case 0:
		return UnsignedValue(length), nil
	case 1:
		if length > math.MaxInt64 {
			return Value{}, Invalid("negative integer overflow")
		}
		return NegativeValue(length), nil
	case 2, 3:
		if length > uint64(reader.remaining()) {
			return Value{}, Invalid("truncated CBOR")
		}
		raw, err := reader.read(int(length))
		if err != nil {
			return Value{}, err
		}
		if major == 2 {
			return BytesValue(append([]byte(nil), raw...)), nil
		}
		if !utf8.Valid(raw) {
			return Value{}, Invalid("invalid UTF8")
		}
		return StringValue(string(raw)), nil
	case 4:
		if length > uint64(reader.remaining()) {
			return Value{}, Invalid("array length exceeded")
		}
		values := make([]Value, 0, int(length))
		for range int(length) {
			item, err := decodeDAGCBOR(reader, depth+1)
			if err != nil {
				return Value{}, err
			}
			values = append(values, item)
		}
		return ArrayValue(values), nil
	case 5:
		if length > uint64(reader.remaining()/2) {
			return Value{}, Invalid("map length exceeded")
		}
		values := make(map[string]Value, int(length))
		for range int(length) {
			key, err := decodeDAGCBOR(reader, depth+1)
			if err != nil {
				return Value{}, err
			}
			if key.Kind != KindString {
				return Value{}, Invalid("duplicate or invalid map key")
			}
			if _, exists := values[key.Str]; exists {
				return Value{}, Invalid("duplicate or invalid map key")
			}
			item, err := decodeDAGCBOR(reader, depth+1)
			if err != nil {
				return Value{}, err
			}
			values[key.Str] = item
		}
		return MapValue(values), nil
	case 6:
		if length != 42 {
			return Value{}, Invalid("invalid CBOR link")
		}
		inner, err := decodeDAGCBOR(reader, depth+1)
		if err != nil {
			return Value{}, err
		}
		if inner.Kind != KindBytes || len(inner.Bytes) == 0 || inner.Bytes[0] != 0 {
			return Value{}, Invalid("invalid CBOR link")
		}
		cid, err := ParseCIDBytes(inner.Bytes[1:])
		if err != nil {
			return Value{}, err
		}
		return LinkValue(cid), nil
	default:
		return Value{}, Invalid("unsupported DAG-CBOR value")
	}
}
