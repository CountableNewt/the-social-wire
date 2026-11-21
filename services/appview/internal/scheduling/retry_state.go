package scheduling

import (
	"encoding/json"
	"math"
)

// RetryState is a schedule's publish attempt bookkeeping.
type RetryState struct {
	AttemptCount  int     `json:"attemptCount"`
	LastAttemptAt *string `json:"lastAttemptAt,omitempty"`
	NextAttemptAt *string `json:"nextAttemptAt,omitempty"`
	MaxAttempts   int     `json:"maxAttempts"`
}

func newRetryState() RetryState { return RetryState{MaxAttempts: 8} }

// UnmarshalJSON tolerates legacy booleans and defaults missing counters.
func (r *RetryState) UnmarshalJSON(data []byte) error {
	fields, err := decodeFields(data)
	if err != nil {
		return err
	}
	lastAttemptAt, err := fields.optionalString("lastAttemptAt")
	if err != nil {
		return err
	}
	nextAttemptAt, err := fields.optionalString("nextAttemptAt")
	if err != nil {
		return err
	}
	*r = RetryState{
		AttemptCount:  lenientInteger(fields["attemptCount"], 0),
		LastAttemptAt: lastAttemptAt,
		NextAttemptAt: nextAttemptAt,
		MaxAttempts:   lenientInteger(fields["maxAttempts"], 8),
	}
	return nil
}

func lenientInteger(raw json.RawMessage, fallback int) int {
	if raw == nil {
		return fallback
	}
	var number float64
	if err := json.Unmarshal(raw, &number); err == nil {
		if math.Trunc(number) == number && math.Abs(number) < 1<<62 {
			return int(number)
		}
		return fallback
	}
	var legacy bool
	if err := json.Unmarshal(raw, &legacy); err == nil {
		if legacy {
			return 1
		}
		return 0
	}
	return fallback
}
