package repo

import "encoding/base64"

// Kind tags the variant held by a Value.
type Kind uint8

// Value kinds, mirroring Swift's RepositoryCBORValue cases.
const (
	KindNull Kind = iota
	KindMap
	KindArray
	KindString
	KindBytes
	KindLink
	KindUnsigned
	KindNegative
	KindDouble
	KindBool
)

// Value is a decoded DAG-CBOR data model value. Only the field matching Kind is
// meaningful. Negative holds the CBOR major-type-1 argument n, encoding -1-n.
type Value struct {
	Kind  Kind
	Map   map[string]Value
	Array []Value
	Str   string
	Bytes []byte
	Link  CID
	Uint  uint64
	Float float64
	Bool  bool
}

// MapValue wraps a map.
func MapValue(m map[string]Value) Value { return Value{Kind: KindMap, Map: m} }

// ArrayValue wraps a list.
func ArrayValue(items []Value) Value { return Value{Kind: KindArray, Array: items} }

// StringValue wraps a text string.
func StringValue(s string) Value { return Value{Kind: KindString, Str: s} }

// BytesValue wraps a byte string.
func BytesValue(b []byte) Value { return Value{Kind: KindBytes, Bytes: b} }

// LinkValue wraps a CID link (tag 42).
func LinkValue(c CID) Value { return Value{Kind: KindLink, Link: c} }

// UnsignedValue wraps a non-negative integer.
func UnsignedValue(n uint64) Value { return Value{Kind: KindUnsigned, Uint: n} }

// NegativeValue wraps the negative integer -1-n.
func NegativeValue(n uint64) Value { return Value{Kind: KindNegative, Uint: n} }

// DoubleValue wraps a 64-bit float.
func DoubleValue(f float64) Value { return Value{Kind: KindDouble, Float: f} }

// BoolValue wraps a boolean.
func BoolValue(b bool) Value { return Value{Kind: KindBool, Bool: b} }

// NullValue is the CBOR null.
func NullValue() Value { return Value{Kind: KindNull} }

// Field returns the map entry for key; ok is false for missing keys or non-maps.
func (v Value) Field(key string) (Value, bool) {
	if v.Kind != KindMap {
		return Value{}, false
	}
	field, ok := v.Map[key]
	return field, ok
}

// Text returns the string payload.
func (v Value) Text() (string, bool) { return v.Str, v.Kind == KindString }

// Data returns the byte-string payload.
func (v Value) Data() ([]byte, bool) { return v.Bytes, v.Kind == KindBytes }

// CID returns the link payload.
func (v Value) CID() (CID, bool) { return v.Link, v.Kind == KindLink }

// Number returns the unsigned integer payload.
func (v Value) Number() (uint64, bool) { return v.Uint, v.Kind == KindUnsigned }

// Values returns the array payload.
func (v Value) Values() ([]Value, bool) { return v.Array, v.Kind == KindArray }

// JSONObject converts the value to the ATProto JSON data model, suitable for
// encoding/json: bytes become {"$bytes": unpadded base64}, links {"$link": cid}.
func (v Value) JSONObject() any {
	switch v.Kind {
	case KindMap:
		out := make(map[string]any, len(v.Map))
		for key, item := range v.Map {
			out[key] = item.JSONObject()
		}
		return out
	case KindArray:
		out := make([]any, len(v.Array))
		for i, item := range v.Array {
			out[i] = item.JSONObject()
		}
		return out
	case KindString:
		return v.Str
	case KindBytes:
		return map[string]any{"$bytes": base64.RawStdEncoding.EncodeToString(v.Bytes)}
	case KindLink:
		return map[string]any{"$link": v.Link.String()}
	case KindUnsigned:
		return v.Uint
	case KindNegative:
		return -1 - int64(v.Uint)
	case KindDouble:
		return v.Float
	case KindBool:
		return v.Bool
	default:
		return nil
	}
}
