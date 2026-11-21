package thinstore

// RSSSourceFailure identifies a viewer's unavailable source for the enrollment
// response. Error descriptions omit source identities because logs are shared.
type RSSSourceFailure struct {
	PublicationID string
	Host          string
	Category      string
}

func (e *RSSSourceFailure) Error() string { return "RSS source unavailable" }

type EnrollmentPartialFailure struct {
	Failures []*RSSSourceFailure
}

func (e *EnrollmentPartialFailure) Error() string { return "Some RSS sources could not be refreshed" }
