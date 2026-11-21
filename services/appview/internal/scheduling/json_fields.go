package scheduling

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// rawFields is a decoded JSON object kept as raw members so records can
// distinguish absent, null, and unknown keys the way Swift's Codable did.
type rawFields map[string]json.RawMessage

func decodeFields(data []byte) (rawFields, error) {
	var fields rawFields
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, fmt.Errorf("scheduling: expected a JSON object")
	}
	return fields, nil
}

// present reports whether key holds a non-null value (decodeIfPresent).
func (f rawFields) present(key string) bool {
	raw, ok := f[key]
	return ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// optional decodes key into target when present and non-null.
func (f rawFields) optional(key string, target any) (bool, error) {
	if !f.present(key) {
		return false, nil
	}
	if err := json.Unmarshal(f[key], target); err != nil {
		return false, fmt.Errorf("scheduling: field %s: %w", key, err)
	}
	return true, nil
}

// required decodes key into target, failing when it is absent or null.
func (f rawFields) required(key string, target any) error {
	if !f.present(key) {
		return fmt.Errorf("scheduling: missing field %s", key)
	}
	if err := json.Unmarshal(f[key], target); err != nil {
		return fmt.Errorf("scheduling: field %s: %w", key, err)
	}
	return nil
}

// optionalString decodes an optional string field.
func (f rawFields) optionalString(key string) (*string, error) {
	var value string
	ok, err := f.optional(key, &value)
	if !ok || err != nil {
		return nil, err
	}
	return &value, nil
}

// unknown collects members outside known as JSON values (nulls included).
func (f rawFields) unknown(known map[string]bool) (map[string]any, error) {
	fields := map[string]any{}
	for key, raw := range f {
		if known[key] {
			continue
		}
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		fields[key] = value
	}
	return fields, nil
}

// objectWriter assembles an encoded object: unknown members first, then
// known members, which win on conflict (Swift encoded in that order).
type objectWriter map[string]any

func newObjectWriter(unknown map[string]any) objectWriter {
	writer := objectWriter{}
	for key, value := range unknown {
		writer[key] = value
	}
	return writer
}

func (w objectWriter) set(key string, value any) { w[key] = value }

func (w objectWriter) setString(key string, value *string) {
	if value != nil {
		w[key] = *value
	}
}

func (w objectWriter) marshal() ([]byte, error) { return json.Marshal(map[string]any(w)) }

// jsonValue round-trips v through JSON into plain values (skejJSONValue).
func jsonValue(v any) (any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return value, nil
}

// jsonEqual compares two values by canonical encoding, Swift's JSONValue ==.
func jsonEqual(a, b any) bool {
	left, err1 := json.Marshal(a)
	right, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && bytes.Equal(left, right)
}

func stringPtr(value string) *string { return &value }

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
