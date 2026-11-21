package readstate

import "encoding/json"

// Reference is an immutable repository record reference. A loader must verify
// the returned CID.
type Reference struct {
	URI string `json:"uri"`
	CID string `json:"cid"`
}

func decodeReference(raw json.RawMessage) (Reference, error) {
	value, err := parseObject(raw)
	if err != nil {
		return Reference{}, err
	}
	uri, err := value.str("uri")
	if err != nil {
		return Reference{}, err
	}
	cid, err := value.str("cid")
	if err != nil {
		return Reference{}, err
	}
	return Reference{URI: uri, CID: cid}, nil
}

func decodeOptionalReference(value object, key string) (*Reference, error) {
	if !value.present(key) {
		return nil, nil
	}
	reference, err := decodeReference(value[key])
	if err != nil {
		return nil, err
	}
	return &reference, nil
}

// UnmarshalJSON decodes with exact keys.
func (r *Reference) UnmarshalJSON(data []byte) error {
	value, err := decodeReference(data)
	if err != nil {
		return err
	}
	*r = value
	return nil
}
