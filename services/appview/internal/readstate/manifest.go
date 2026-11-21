package readstate

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
)

// ManifestCollection is the singleton manifest collection, record key "self",
// replaced with compare-and-swap.
const ManifestCollection = "app.thesocialwire.readState"

var manifestKnownKeys = []string{"$type", "version", "generation", "lastSequence", "head", "revision",
	"stateHead", "devicesHead", "legacyReceiptsHead", "compactionVersion"}

func isManifestKnownKey(key string) bool {
	for _, known := range manifestKnownKeys {
		if key == known {
			return true
		}
	}
	return false
}

// Manifest is ReadStateManifest (v1 head chain or v2 rooted chains).
type Manifest struct {
	Type               string
	Version            int
	Generation         string
	LastSequence       int64
	Head               *Reference
	Revision           *int64
	StateHead          *Reference
	DevicesHead        *Reference
	LegacyReceiptsHead *Reference
	CompactionVersion  *int
	// Extensions preserves unknown top-level fields.
	Extensions map[string]JSONValue
}

// NewManifestV1 builds a v1 manifest; known keys are dropped from extensions.
func NewManifestV1(generation string, lastSequence int64, head *Reference, extensions map[string]JSONValue) Manifest {
	filtered := map[string]JSONValue{}
	for key, value := range extensions {
		if !isManifestKnownKey(key) {
			filtered[key] = value
		}
	}
	return Manifest{Type: ManifestCollection, Version: 1, Generation: generation, LastSequence: lastSequence,
		Head: head, Extensions: filtered}
}

// NewManifestV2 builds a v2 manifest.
func NewManifestV2(generation string, revision, lastSequence int64, stateHead, devicesHead, legacyReceiptsHead *Reference) Manifest {
	compaction := 1
	return Manifest{Type: ManifestCollection, Version: 2, Generation: generation, Revision: &revision,
		LastSequence: lastSequence, StateHead: stateHead, DevicesHead: devicesHead,
		LegacyReceiptsHead: legacyReceiptsHead, CompactionVersion: &compaction, Extensions: map[string]JSONValue{}}
}

// EffectiveStateHead is the v2 state root or the v1 head.
func (m Manifest) EffectiveStateHead() *Reference {
	if m.Version == 2 {
		return m.StateHead
	}
	return m.Head
}

// EffectiveRevision is the revision, defaulting to lastSequence.
func (m Manifest) EffectiveRevision() int64 {
	if m.Revision != nil {
		return *m.Revision
	}
	return m.LastSequence
}

// Equal compares every stored field, like Swift's synthesized Equatable.
func (m Manifest) Equal(other Manifest) bool {
	if m.Type != other.Type || m.Version != other.Version || m.Generation != other.Generation ||
		m.LastSequence != other.LastSequence || !reflect.DeepEqual(m.Head, other.Head) ||
		!reflect.DeepEqual(m.Revision, other.Revision) || !reflect.DeepEqual(m.StateHead, other.StateHead) ||
		!reflect.DeepEqual(m.DevicesHead, other.DevicesHead) ||
		!reflect.DeepEqual(m.LegacyReceiptsHead, other.LegacyReceiptsHead) ||
		!reflect.DeepEqual(m.CompactionVersion, other.CompactionVersion) ||
		len(m.Extensions) != len(other.Extensions) {
		return false
	}
	for key, value := range m.Extensions {
		candidate, ok := other.Extensions[key]
		if !ok || !value.Equal(candidate) {
			return false
		}
	}
	return true
}

// MarshalJSON emits known keys in Swift's order, then extensions sorted.
func (m Manifest) MarshalJSON() ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteByte('{')
	first := true
	field := func(key string, value any) error {
		data, err := marshalNoEscape(value)
		if err != nil {
			return err
		}
		if !first {
			buffer.WriteByte(',')
		}
		first = false
		name, _ := marshalNoEscape(key)
		buffer.Write(name)
		buffer.WriteByte(':')
		buffer.Write(data)
		return nil
	}
	steps := []struct {
		key     string
		value   any
		present bool
	}{
		{"$type", m.Type, true},
		{"version", m.Version, true},
		{"generation", m.Generation, true},
		{"lastSequence", m.LastSequence, true},
		{"head", m.Head, m.Head != nil},
		{"revision", m.Revision, m.Revision != nil},
		{"stateHead", m.StateHead, m.StateHead != nil},
		{"devicesHead", m.DevicesHead, m.DevicesHead != nil},
		{"legacyReceiptsHead", m.LegacyReceiptsHead, m.LegacyReceiptsHead != nil},
		{"compactionVersion", m.CompactionVersion, m.CompactionVersion != nil},
	}
	for _, step := range steps {
		if step.present {
			if err := field(step.key, step.value); err != nil {
				return nil, err
			}
		}
	}
	keys := make([]string, 0, len(m.Extensions))
	for key := range m.Extensions {
		if !isManifestKnownKey(key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := field(key, m.Extensions[key]); err != nil {
			return nil, err
		}
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}

// UnmarshalJSON decodes with exact keys.
func (m *Manifest) UnmarshalJSON(data []byte) error {
	value, err := DecodeManifest(data)
	if err != nil {
		return err
	}
	*m = value
	return nil
}

// DecodeManifest decodes a manifest record. A v2 manifest is closed to
// unknown fields (ErrInvalidRecord).
func DecodeManifest(data []byte) (Manifest, error) {
	value, err := parseObject(data)
	if err != nil {
		return Manifest{}, err
	}
	var result Manifest
	if result.Type, err = value.str("$type"); err != nil {
		return Manifest{}, err
	}
	if result.Version, err = value.int("version"); err != nil {
		return Manifest{}, err
	}
	if result.Generation, err = value.str("generation"); err != nil {
		return Manifest{}, err
	}
	if result.LastSequence, err = value.int64("lastSequence"); err != nil {
		return Manifest{}, err
	}
	if result.Head, err = decodeOptionalReference(value, "head"); err != nil {
		return Manifest{}, err
	}
	if result.Revision, err = value.optInt64("revision"); err != nil {
		return Manifest{}, err
	}
	if result.StateHead, err = decodeOptionalReference(value, "stateHead"); err != nil {
		return Manifest{}, err
	}
	if result.DevicesHead, err = decodeOptionalReference(value, "devicesHead"); err != nil {
		return Manifest{}, err
	}
	if result.LegacyReceiptsHead, err = decodeOptionalReference(value, "legacyReceiptsHead"); err != nil {
		return Manifest{}, err
	}
	if result.CompactionVersion, err = value.optInt("compactionVersion"); err != nil {
		return Manifest{}, err
	}
	if result.Version == 2 {
		if err := v2ManifestShape(data, value); err != nil {
			return Manifest{}, err
		}
	}
	result.Extensions = map[string]JSONValue{}
	for key, raw := range value {
		if isManifestKnownKey(key) {
			continue
		}
		var extension JSONValue
		if err := json.Unmarshal(raw, &extension); err != nil {
			return Manifest{}, decodeErr("extension " + key)
		}
		result.Extensions[key] = extension
	}
	return result, nil
}

// ManifestRecord pairs a manifest with its record CID.
type ManifestRecord struct {
	Manifest Manifest `json:"manifest"`
	CID      string   `json:"cid"`
}
