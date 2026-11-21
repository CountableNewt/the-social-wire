package thinstore

import (
	"encoding/json"
	"errors"
	"time"
)

// EnrollRequest is the POST /v1/appview/enroll body; missing arrays decode as empty.
type EnrollRequest struct {
	AuthorDIDs []string `json:"authorDids"`
	FeedURLs   []string `json:"feedUrls"`
}

// UnmarshalJSON defaults absent arrays to empty like the Swift decoder.
func (r *EnrollRequest) UnmarshalJSON(data []byte) error {
	var wire struct {
		AuthorDIDs []string `json:"authorDids"`
		FeedURLs   []string `json:"feedUrls"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	r.AuthorDIDs = nonNil(wire.AuthorDIDs)
	r.FeedURLs = nonNil(wire.FeedURLs)
	return nil
}

// ReadMarkRequest is a read-mark body; readAt is an optional ISO 8601 string,
// and unparseable values decode as absent.
type ReadMarkRequest struct {
	SubjectURI string
	ReadAt     *time.Time
}

// UnmarshalJSON requires subjectUri and parses readAt leniently.
func (r *ReadMarkRequest) UnmarshalJSON(data []byte) error {
	var wire struct {
		SubjectURI *string `json:"subjectUri"`
		ReadAt     *string `json:"readAt"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.SubjectURI == nil {
		return errors.New("thinstore: subjectUri is required")
	}
	r.SubjectURI = *wire.SubjectURI
	r.ReadAt = nil
	if wire.ReadAt != nil {
		if parsed, ok := parseISO8601Date(*wire.ReadAt); ok {
			r.ReadAt = &parsed
		}
	}
	return nil
}

// ReadMarkDeleteRequest is a read-mark deletion body.
type ReadMarkDeleteRequest struct {
	SubjectURI string `json:"subjectUri"`
}

// UnmarshalJSON requires subjectUri.
func (r *ReadMarkDeleteRequest) UnmarshalJSON(data []byte) error {
	var wire struct {
		SubjectURI *string `json:"subjectUri"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.SubjectURI == nil {
		return errors.New("thinstore: subjectUri is required")
	}
	r.SubjectURI = *wire.SubjectURI
	return nil
}
