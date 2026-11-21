package readstate

import "encoding/json"

// V2ChunkKind is ReadStateV2Chunk.Kind.
type V2ChunkKind string

// v2 chunk kinds.
const (
	V2KindState          V2ChunkKind = "state"
	V2KindDevices        V2ChunkKind = "devices"
	V2KindLegacyReceipts V2ChunkKind = "legacyReceipts"
)

// V2Chunk is a v2 chunk in one of the three manifest chains.
type V2Chunk struct {
	Kind           V2ChunkKind
	Fragments      []V2Fragment
	DeviceReceipts []DeviceReceipt
	LegacyReceipts []LegacyActionReceipt
	Previous       *Reference
}

// NewV2StateChunk builds a state chunk.
func NewV2StateChunk(fragments []V2Fragment, previous *Reference) V2Chunk {
	return V2Chunk{Kind: V2KindState, Fragments: fragments, Previous: previous}
}

// NewV2DevicesChunk builds a devices chunk.
func NewV2DevicesChunk(devices []DeviceReceipt, previous *Reference) V2Chunk {
	return V2Chunk{Kind: V2KindDevices, DeviceReceipts: devices, Previous: previous}
}

// NewV2LegacyReceiptsChunk builds a legacy receipts chunk.
func NewV2LegacyReceiptsChunk(receipts []LegacyActionReceipt, previous *Reference) V2Chunk {
	return V2Chunk{Kind: V2KindLegacyReceipts, LegacyReceipts: receipts, Previous: previous}
}

// MarshalJSON uses Swift's key order: $type, version, kind, previous, payload.
func (c V2Chunk) MarshalJSON() ([]byte, error) {
	type wire struct {
		Type      string      `json:"$type"`
		Version   int         `json:"version"`
		Kind      V2ChunkKind `json:"kind"`
		Previous  *Reference  `json:"previous,omitempty"`
		Fragments any         `json:"fragments,omitempty"`
		Receipts  any         `json:"receipts,omitempty"`
	}
	out := wire{Type: ChunkCollection, Version: 2, Kind: c.Kind, Previous: c.Previous}
	switch c.Kind {
	case V2KindState:
		out.Fragments = nonNil(c.Fragments)
	case V2KindDevices:
		out.Receipts = nonNil(c.DeviceReceipts)
	default:
		out.Receipts = nonNil(c.LegacyReceipts)
	}
	return marshalNoEscape(out)
}

func nonNil[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}

// UnmarshalJSON decodes a closed-shape v2 chunk.
func (c *V2Chunk) UnmarshalJSON(data []byte) error {
	value, err := DecodeV2Chunk(data)
	if err != nil {
		return err
	}
	*c = value
	return nil
}

// DecodeV2Chunk decodes a v2 chunk record.
func DecodeV2Chunk(data []byte) (V2Chunk, error) {
	value, err := parseObject(data)
	if err != nil {
		return V2Chunk{}, err
	}
	recordType, err := value.str("$type")
	if err != nil {
		return V2Chunk{}, err
	}
	version, err := value.int("version")
	if err != nil {
		return V2Chunk{}, err
	}
	if recordType != ChunkCollection || version != 2 {
		return V2Chunk{}, ErrInvalidRecord
	}
	kind, err := value.str("kind")
	if err != nil {
		return V2Chunk{}, err
	}
	result := V2Chunk{Kind: V2ChunkKind(kind)}
	switch result.Kind {
	case V2KindState, V2KindDevices, V2KindLegacyReceipts:
	default:
		return V2Chunk{}, decodeErr("unknown chunk kind")
	}
	if err := v2ChunkShape(data, result.Kind); err != nil {
		return V2Chunk{}, err
	}
	if result.Previous, err = decodeOptionalReference(value, "previous"); err != nil {
		return V2Chunk{}, err
	}
	result.Fragments = []V2Fragment{}
	result.DeviceReceipts = []DeviceReceipt{}
	result.LegacyReceipts = []LegacyActionReceipt{}
	switch result.Kind {
	case V2KindState:
		items, err := value.array("fragments")
		if err != nil {
			return V2Chunk{}, err
		}
		for _, item := range items {
			fragment, err := decodeV2Fragment(item)
			if err != nil {
				return V2Chunk{}, err
			}
			result.Fragments = append(result.Fragments, fragment)
		}
	case V2KindDevices:
		raw, err := value.required("receipts")
		if err != nil {
			return V2Chunk{}, err
		}
		if err := json.Unmarshal(raw, &result.DeviceReceipts); err != nil {
			return V2Chunk{}, decodeErr(err.Error())
		}
	default:
		raw, err := value.required("receipts")
		if err != nil {
			return V2Chunk{}, err
		}
		if err := json.Unmarshal(raw, &result.LegacyReceipts); err != nil {
			return V2Chunk{}, decodeErr(err.Error())
		}
	}
	return result, nil
}

// Validate checks a decoded chunk against the viewer.
func (c V2Chunk) Validate(viewerDID string) error {
	if c.Previous != nil {
		if err := ValidateReference(*c.Previous, viewerDID); err != nil {
			return err
		}
	}
	count := len(c.Fragments) + len(c.DeviceReceipts) + len(c.LegacyReceipts)
	if count < 1 || count > 128 {
		return ErrInvalidRecord
	}
	for _, fragment := range c.Fragments {
		if err := fragment.Validate(); err != nil {
			return err
		}
	}
	for _, receipt := range c.DeviceReceipts {
		if !isDeviceID(receipt.DeviceID) || receipt.CommittedCounter < 0 ||
			receipt.CommittedCounter > MaximumSequence || !isLowerHexHash(receipt.PrefixHash) {
			return ErrInvalidRecord
		}
	}
	for _, receipt := range c.LegacyReceipts {
		if receipt.ActionID == "" || len(receipt.ActionID) > 128 || receipt.OriginalSequence < 1 ||
			receipt.OriginalSequence > MaximumSequence || !isLowerHexHash(receipt.OriginalIntentHash) {
			return ErrInvalidRecord
		}
	}
	return ValidateSize(c)
}
