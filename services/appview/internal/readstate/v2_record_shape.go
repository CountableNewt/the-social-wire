package readstate

import (
	"encoding/json"
	"strings"
)

// v2 rewrites are closed to unknown nested semantics and reference roots
// (ReadStateV2RecordShape).

func shapeObject(raw json.RawMessage, keys ...string) (object, error) {
	value, err := parseObject(raw)
	if err != nil {
		return nil, ErrInvalidRecord
	}
	if !value.onlyKeys(keys...) {
		return nil, ErrInvalidRecord
	}
	return value, nil
}

func shapeReference(value object, key string) error {
	if value.present(key) {
		_, err := shapeObject(value[key], "uri", "cid")
		return err
	}
	return nil
}

func v2ManifestShape(data []byte, _ object) error {
	// The whole value must also be representable as a ReadStateJSONValue.
	var whole JSONValue
	if err := json.Unmarshal(data, &whole); err != nil {
		return err
	}
	value, err := shapeObject(data, "$type", "version", "generation", "revision", "lastSequence",
		"stateHead", "devicesHead", "legacyReceiptsHead", "compactionVersion")
	if err != nil {
		return err
	}
	for _, key := range []string{"stateHead", "devicesHead", "legacyReceiptsHead"} {
		if err := shapeReference(value, key); err != nil {
			return err
		}
	}
	return nil
}

func v2FragmentShape(raw json.RawMessage) error {
	var whole JSONValue
	if err := json.Unmarshal(raw, &whole); err != nil {
		return err
	}
	value, err := shapeObject(raw, "actionId", "sequence", "state", "actedAt", "selection", "subjectUris",
		"boundaries", "calendar", "fragment", "intentHash", "deviceId", "deviceCounter")
	if err != nil {
		return err
	}
	if value.present("calendar") {
		if _, err := shapeObject(value["calendar"], "cutoff", "timeZone", "referenceDate"); err != nil {
			return err
		}
	}
	if raw, ok := value["boundaries"]; ok && strings.HasPrefix(strings.TrimSpace(string(raw)), "[") {
		boundaries, err := rawArray(raw, "boundaries")
		if err != nil {
			return err
		}
		for _, item := range boundaries {
			boundary, err := shapeObject(item, "scope", "createdAt", "entryId")
			if err != nil {
				return err
			}
			scope, ok := boundary["scope"]
			if !ok {
				scope = json.RawMessage("null")
			}
			if _, err := shapeObject(scope, "publicationId", "authorDid", "publicationSiteKeys"); err != nil {
				return err
			}
		}
	}
	return nil
}

func v2ChunkShape(raw json.RawMessage, kind V2ChunkKind) error {
	var whole JSONValue
	if err := json.Unmarshal(raw, &whole); err != nil {
		return err
	}
	payload := "receipts"
	if kind == V2KindState {
		payload = "fragments"
	}
	value, err := shapeObject(raw, "$type", "version", "kind", payload, "previous")
	if err != nil {
		return err
	}
	if err := shapeReference(value, "previous"); err != nil {
		return err
	}
	if receipts, ok := value["receipts"]; ok && strings.HasPrefix(strings.TrimSpace(string(receipts)), "[") {
		items, err := rawArray(receipts, "receipts")
		if err != nil {
			return err
		}
		for _, item := range items {
			if kind == V2KindDevices {
				_, err = shapeObject(item, "deviceId", "committedCounter", "prefixHash")
			} else {
				_, err = shapeObject(item, "actionId", "originalSequence", "originalIntentHash")
			}
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func isLowerHexHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	for index := 0; index < len(value); index++ {
		c := value[index]
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// isDeviceID reports a lowercase canonical UUID, as
// UUID(uuidString:)?.uuidString.lowercased() == value.
func isDeviceID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index := 0; index < len(value); index++ {
		c := value[index]
		switch index {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
				return false
			}
		}
	}
	return true
}
