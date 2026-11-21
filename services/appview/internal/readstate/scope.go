package readstate

import "encoding/json"

// Scope is a frozen publication identity. Empty site keys mean every item by
// this author.
type Scope struct {
	PublicationID       string   `json:"publicationId"`
	AuthorDID           string   `json:"authorDid"`
	PublicationSiteKeys []string `json:"publicationSiteKeys"`
}

// MarshalJSON always encodes publicationSiteKeys as an array.
func (s Scope) MarshalJSON() ([]byte, error) {
	keys := s.PublicationSiteKeys
	if keys == nil {
		keys = []string{}
	}
	return marshalNoEscape(struct {
		PublicationID       string   `json:"publicationId"`
		AuthorDID           string   `json:"authorDid"`
		PublicationSiteKeys []string `json:"publicationSiteKeys"`
	}{s.PublicationID, s.AuthorDID, keys})
}

// UnmarshalJSON decodes with exact keys.
func (s *Scope) UnmarshalJSON(data []byte) error {
	value, err := decodeScope(data)
	if err != nil {
		return err
	}
	*s = value
	return nil
}

func decodeScope(raw json.RawMessage) (Scope, error) {
	value, err := parseObject(raw)
	if err != nil {
		return Scope{}, err
	}
	publicationID, err := value.str("publicationId")
	if err != nil {
		return Scope{}, err
	}
	authorDID, err := value.str("authorDid")
	if err != nil {
		return Scope{}, err
	}
	keys, err := value.stringArray("publicationSiteKeys")
	if err != nil {
		return Scope{}, err
	}
	return Scope{PublicationID: publicationID, AuthorDID: authorDID, PublicationSiteKeys: keys}, nil
}
