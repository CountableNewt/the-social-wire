package readstate

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
)

// JSONKind tags a JSONValue case.
type JSONKind int

// ReadStateJSONValue cases.
const (
	JSONNull JSONKind = iota
	JSONBool
	JSONInteger
	JSONString
	JSONArray
	JSONObject
)

// JSONValue mirrors ReadStateJSONValue: it preserves manifest extension fields
// and feeds the restricted canonical hash. Numbers must be integers.
type JSONValue struct {
	Kind    JSONKind
	Bool    bool
	Integer int64
	String  string
	Array   []JSONValue
	Object  map[string]JSONValue
}

// Convenience constructors.
func jsonString(value string) JSONValue { return JSONValue{Kind: JSONString, String: value} }
func jsonInteger(value int64) JSONValue { return JSONValue{Kind: JSONInteger, Integer: value} }
func jsonArray(values ...JSONValue) JSONValue {
	if values == nil {
		values = []JSONValue{}
	}
	return JSONValue{Kind: JSONArray, Array: values}
}

// Equal compares two values structurally.
func (v JSONValue) Equal(other JSONValue) bool {
	if v.Kind != other.Kind {
		return false
	}
	switch v.Kind {
	case JSONNull:
		return true
	case JSONBool:
		return v.Bool == other.Bool
	case JSONInteger:
		return v.Integer == other.Integer
	case JSONString:
		return v.String == other.String
	case JSONArray:
		if len(v.Array) != len(other.Array) {
			return false
		}
		for index := range v.Array {
			if !v.Array[index].Equal(other.Array[index]) {
				return false
			}
		}
		return true
	default:
		if len(v.Object) != len(other.Object) {
			return false
		}
		for key, value := range v.Object {
			candidate, ok := other.Object[key]
			if !ok || !value.Equal(candidate) {
				return false
			}
		}
		return true
	}
}

// UnmarshalJSON decodes in Swift's order: null, bool, integer, string, array, object.
func (v *JSONValue) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	text := string(trimmed)
	switch {
	case text == "null":
		*v = JSONValue{Kind: JSONNull}
	case text == "true" || text == "false":
		*v = JSONValue{Kind: JSONBool, Bool: text == "true"}
	case len(text) > 0 && (text[0] == '-' || (text[0] >= '0' && text[0] <= '9')):
		value, err := rawInt64(trimmed, "value")
		if err != nil {
			return err
		}
		*v = jsonInteger(value)
	case strings.HasPrefix(text, `"`):
		value, err := rawString(trimmed, "value")
		if err != nil {
			return err
		}
		*v = jsonString(value)
	case strings.HasPrefix(text, "["):
		var values []JSONValue
		if err := json.Unmarshal(trimmed, &values); err != nil {
			return err
		}
		*v = jsonArray(values...)
	case strings.HasPrefix(text, "{"):
		var values map[string]JSONValue
		if err := json.Unmarshal(trimmed, &values); err != nil {
			return err
		}
		if values == nil {
			values = map[string]JSONValue{}
		}
		*v = JSONValue{Kind: JSONObject, Object: values}
	default:
		return decodeErr("unsupported JSON value")
	}
	return nil
}

// MarshalJSON encodes the value; object keys are emitted sorted.
func (v JSONValue) MarshalJSON() ([]byte, error) {
	switch v.Kind {
	case JSONNull:
		return []byte("null"), nil
	case JSONBool:
		return json.Marshal(v.Bool)
	case JSONInteger:
		return json.Marshal(v.Integer)
	case JSONString:
		return marshalNoEscape(v.String)
	case JSONArray:
		if v.Array == nil {
			return []byte("[]"), nil
		}
		return marshalNoEscape(v.Array)
	default:
		var buffer bytes.Buffer
		buffer.WriteByte('{')
		keys := make([]string, 0, len(v.Object))
		for key := range v.Object {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for index, key := range keys {
			if index > 0 {
				buffer.WriteByte(',')
			}
			name, _ := marshalNoEscape(key)
			buffer.Write(name)
			buffer.WriteByte(':')
			value, err := v.Object[key].MarshalJSON()
			if err != nil {
				return nil, err
			}
			buffer.Write(value)
		}
		buffer.WriteByte('}')
		return buffer.Bytes(), nil
	}
}

// toJSONValue round-trips an encodable value, ReadStateV2CanonicalHash.value.
func toJSONValue(value any) (JSONValue, error) {
	data, err := marshalNoEscape(value)
	if err != nil {
		return JSONValue{}, err
	}
	var result JSONValue
	if err := json.Unmarshal(data, &result); err != nil {
		return JSONValue{}, err
	}
	return result, nil
}
