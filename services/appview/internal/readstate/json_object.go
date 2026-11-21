package readstate

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// object is a JSON object with exact, case-sensitive keys, the lookup Swift's
// keyed decoding containers perform (encoding/json struct decoding folds case).
type object map[string]json.RawMessage

func parseObject(data []byte) (object, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return nil, decodeErr("expected object")
	}
	var value object
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, decodeErr(err.Error())
	}
	return value, nil
}

func isNull(raw json.RawMessage) bool { return string(bytes.TrimSpace(raw)) == "null" }

// present reports a key that exists with a non-null value (decodeIfPresent).
func (o object) present(key string) bool {
	raw, ok := o[key]
	return ok && !isNull(raw)
}

func (o object) keys() []string {
	keys := make([]string, 0, len(o))
	for key := range o {
		keys = append(keys, key)
	}
	return keys
}

// onlyKeys reports whether every key is in allowed.
func (o object) onlyKeys(allowed ...string) bool {
	for key := range o {
		found := false
		for _, candidate := range allowed {
			if key == candidate {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (o object) required(key string) (json.RawMessage, error) {
	raw, ok := o[key]
	if !ok || isNull(raw) {
		return nil, decodeErr("missing " + key)
	}
	return raw, nil
}

func (o object) str(key string) (string, error) {
	raw, err := o.required(key)
	if err != nil {
		return "", err
	}
	return rawString(raw, key)
}

func (o object) optStr(key string) (*string, error) {
	if !o.present(key) {
		return nil, nil
	}
	value, err := rawString(o[key], key)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func rawString(raw json.RawMessage, key string) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '"' {
		return "", decodeErr(key + " is not a string")
	}
	var value string
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return "", decodeErr(key + ": " + err.Error())
	}
	return value, nil
}

func (o object) int64(key string) (int64, error) {
	raw, err := o.required(key)
	if err != nil {
		return 0, err
	}
	return rawInt64(raw, key)
}

func (o object) optInt64(key string) (*int64, error) {
	if !o.present(key) {
		return nil, nil
	}
	value, err := rawInt64(o[key], key)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func (o object) int(key string) (int, error) {
	value, err := o.int64(key)
	return int(value), err
}

func (o object) optInt(key string) (*int, error) {
	value, err := o.optInt64(key)
	if value == nil || err != nil {
		return nil, err
	}
	converted := int(*value)
	return &converted, nil
}

// rawInt64 accepts integer literals and exactly integral number literals, as
// Swift's JSONDecoder does for integer types.
func rawInt64(raw json.RawMessage, key string) (int64, error) {
	text := strings.TrimSpace(string(raw))
	if text == "" || !(text[0] == '-' || (text[0] >= '0' && text[0] <= '9')) {
		return 0, decodeErr(key + " is not a number")
	}
	if value, err := strconv.ParseInt(text, 10, 64); err == nil {
		return value, nil
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsInf(value, 0) || value != math.Trunc(value) ||
		value < math.MinInt64 || value >= math.MaxInt64 {
		return 0, decodeErr(key + " is not an integer")
	}
	return int64(value), nil
}

func (o object) boolean(key string) (bool, error) {
	raw, err := o.required(key)
	if err != nil {
		return false, err
	}
	switch strings.TrimSpace(string(raw)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, decodeErr(key + " is not a boolean")
}

func (o object) array(key string) ([]json.RawMessage, error) {
	raw, err := o.required(key)
	if err != nil {
		return nil, err
	}
	return rawArray(raw, key)
}

func rawArray(raw json.RawMessage, key string) ([]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, decodeErr(key + " is not an array")
	}
	var values []json.RawMessage
	if err := json.Unmarshal(trimmed, &values); err != nil {
		return nil, decodeErr(key + ": " + err.Error())
	}
	if values == nil {
		values = []json.RawMessage{}
	}
	return values, nil
}

func (o object) stringArray(key string) ([]string, error) {
	values, err := o.array(key)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(values))
	for _, raw := range values {
		value, err := rawString(raw, key)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

// marshalNoEscape encodes like Swift's JSONEncoder with .withoutEscapingSlashes:
// no HTML escaping, slashes left as is.
func marshalNoEscape(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}

// swiftJSON encodes like a default Swift JSONEncoder, which escapes "/" as
// "\/". A slash can only occur inside JSON strings, so replacement is safe.
func swiftJSON(value any) (string, error) {
	data, err := marshalNoEscape(value)
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(string(data), "/", `\/`), nil
}
