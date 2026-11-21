package readstate

import (
	"encoding/json"
	"reflect"
)

// LegacyActionReceipt retains whole-intent retry evidence for a compacted v1 action.
type LegacyActionReceipt struct {
	ActionID           string `json:"actionId"`
	OriginalSequence   int64  `json:"originalSequence"`
	OriginalIntentHash string `json:"originalIntentHash"`
}

// UnmarshalJSON decodes with exact keys.
func (r *LegacyActionReceipt) UnmarshalJSON(data []byte) error {
	value, err := parseObject(data)
	if err != nil {
		return err
	}
	var result LegacyActionReceipt
	if result.ActionID, err = value.str("actionId"); err != nil {
		return err
	}
	if result.OriginalSequence, err = value.int64("originalSequence"); err != nil {
		return err
	}
	if result.OriginalIntentHash, err = value.str("originalIntentHash"); err != nil {
		return err
	}
	*r = result
	return nil
}

// DeviceReceipt is a durable contiguous prefix, not a counter-only
// authorization to discard pending work.
type DeviceReceipt struct {
	DeviceID         string `json:"deviceId"`
	CommittedCounter int64  `json:"committedCounter"`
	PrefixHash       string `json:"prefixHash"`
}

// UnmarshalJSON decodes with exact keys.
func (r *DeviceReceipt) UnmarshalJSON(data []byte) error {
	value, err := parseObject(data)
	if err != nil {
		return err
	}
	var result DeviceReceipt
	if result.DeviceID, err = value.str("deviceId"); err != nil {
		return err
	}
	if result.CommittedCounter, err = value.int64("committedCounter"); err != nil {
		return err
	}
	if result.PrefixHash, err = value.str("prefixHash"); err != nil {
		return err
	}
	*r = result
	return nil
}

// NewDeviceReceipt is the empty receipt for a device of viewerDID.
func NewDeviceReceipt(viewerDID, deviceID string) (DeviceReceipt, error) {
	if len(viewerDID) < 4 || viewerDID[:4] != "did:" || len(viewerDID) > 2048 {
		return DeviceReceipt{}, ErrInvalidRecord
	}
	hash, err := canonicalHash(jsonArray(jsonString("app.thesocialwire.read-state/device/v2"),
		jsonString(viewerDID), jsonString(deviceID)))
	if err != nil {
		return DeviceReceipt{}, err
	}
	receipt := DeviceReceipt{DeviceID: deviceID, PrefixHash: hash}
	if err := receipt.validate(); err != nil {
		return DeviceReceipt{}, err
	}
	return receipt, nil
}

func (r DeviceReceipt) validate() error {
	if !isDeviceID(r.DeviceID) || r.CommittedCounter < 0 || r.CommittedCounter > MaximumSequence ||
		!isLowerHexHash(r.PrefixHash) {
		return ErrInvalidRecord
	}
	return nil
}

// Advancing extends the proven prefix by intentHashes starting at firstCounter.
func (r DeviceReceipt) Advancing(firstCounter int64, intentHashes []string) (DeviceReceipt, error) {
	if err := r.validate(); err != nil {
		return DeviceReceipt{}, err
	}
	if len(intentHashes) == 0 || firstCounter != r.CommittedCounter+1 ||
		int64(len(intentHashes)) > MaximumSequence-r.CommittedCounter {
		return DeviceReceipt{}, ErrConflictingSequence
	}
	hash := r.PrefixHash
	for offset, intent := range intentHashes {
		if !isLowerHexHash(intent) {
			return DeviceReceipt{}, ErrInvalidRecord
		}
		next, err := canonicalHash(jsonArray(jsonString("app.thesocialwire.read-state/prefix/v2"),
			jsonString(hash), jsonInteger(firstCounter+int64(offset)), jsonString(intent)))
		if err != nil {
			return DeviceReceipt{}, err
		}
		hash = next
	}
	return DeviceReceipt{DeviceID: r.DeviceID, CommittedCounter: r.CommittedCounter + int64(len(intentHashes)),
		PrefixHash: hash}, nil
}

// VerifiedAcknowledgement returns only the pending prefix proven by its
// original intent hashes.
func (r DeviceReceipt) VerifiedAcknowledgement(remote DeviceReceipt, pendingIntentHashes []string) (int, error) {
	if err := r.validate(); err != nil {
		return 0, err
	}
	if err := remote.validate(); err != nil {
		return 0, err
	}
	count := remote.CommittedCounter - r.CommittedCounter
	if r.DeviceID != remote.DeviceID || count < 0 || count > int64(len(pendingIntentHashes)) {
		return 0, ErrConflictingSequence
	}
	expected := r
	if count > 0 {
		var err error
		if expected, err = r.Advancing(r.CommittedCounter+1, pendingIntentHashes[:count]); err != nil {
			return 0, err
		}
	}
	if !reflect.DeepEqual(expected, remote) {
		return 0, ErrConflictingSequence
	}
	return int(count), nil
}

var _ json.Unmarshaler = (*DeviceReceipt)(nil)
