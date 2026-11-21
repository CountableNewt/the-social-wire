package readstate

import "encoding/json"

// V2Fragment is one operation fragment in a v2 state chunk, carrying the
// whole-intent hash and, for device writes, the device counter.
type V2Fragment struct {
	Operation     Operation
	IntentHash    string
	DeviceID      *string
	DeviceCounter *int64
}

// NewV2Fragment builds and validates a fragment.
func NewV2Fragment(operation Operation, intentHash string, deviceID *string, deviceCounter *int64) (V2Fragment, error) {
	fragment := V2Fragment{Operation: operation, IntentHash: intentHash, DeviceID: deviceID, DeviceCounter: deviceCounter}
	if err := fragment.Validate(); err != nil {
		return V2Fragment{}, err
	}
	return fragment, nil
}

// Validate checks the operation and fragment metadata.
func (f V2Fragment) Validate() error {
	if err := ValidateOperation(f.Operation); err != nil {
		return err
	}
	if !isLowerHexHash(f.IntentHash) || (f.DeviceID == nil) != (f.DeviceCounter == nil) {
		return ErrInvalidRecord
	}
	if f.DeviceID != nil && f.DeviceCounter != nil {
		if !isDeviceID(*f.DeviceID) || *f.DeviceCounter < 1 || *f.DeviceCounter > MaximumSequence {
			return ErrInvalidRecord
		}
	}
	return nil
}

// MarshalJSON flattens the operation fields followed by fragment metadata.
func (f V2Fragment) MarshalJSON() ([]byte, error) {
	type wire struct {
		operationWire
		Fragment      bool    `json:"fragment"`
		IntentHash    string  `json:"intentHash"`
		DeviceID      *string `json:"deviceId,omitempty"`
		DeviceCounter *int64  `json:"deviceCounter,omitempty"`
	}
	return marshalNoEscape(wire{operationWire: f.Operation.wire(), Fragment: true, IntentHash: f.IntentHash,
		DeviceID: f.DeviceID, DeviceCounter: f.DeviceCounter})
}

// UnmarshalJSON decodes a closed-shape fragment and validates it.
func (f *V2Fragment) UnmarshalJSON(data []byte) error {
	value, err := decodeV2Fragment(data)
	if err != nil {
		return err
	}
	*f = value
	return nil
}

func decodeV2Fragment(raw json.RawMessage) (V2Fragment, error) {
	if err := v2FragmentShape(raw); err != nil {
		return V2Fragment{}, err
	}
	value, err := parseObject(raw)
	if err != nil {
		return V2Fragment{}, err
	}
	operation, err := decodeOperation(value)
	if err != nil {
		return V2Fragment{}, err
	}
	flag, err := value.boolean("fragment")
	if err != nil {
		return V2Fragment{}, err
	}
	if !flag {
		return V2Fragment{}, ErrInvalidRecord
	}
	result := V2Fragment{Operation: operation}
	if result.IntentHash, err = value.str("intentHash"); err != nil {
		return V2Fragment{}, err
	}
	if result.DeviceID, err = value.optStr("deviceId"); err != nil {
		return V2Fragment{}, err
	}
	if result.DeviceCounter, err = value.optInt64("deviceCounter"); err != nil {
		return V2Fragment{}, err
	}
	if err := result.Validate(); err != nil {
		return V2Fragment{}, err
	}
	return result, nil
}
