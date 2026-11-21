package scheduling

import (
	"encoding/json"
	"fmt"
)

// ScheduleErrorCode classifies a publish failure.
type ScheduleErrorCode string

// Error codes.
const (
	CodeTransientNetwork  ScheduleErrorCode = "transient_network"
	CodeRateLimited       ScheduleErrorCode = "rate_limited"
	CodeAuthInvalid       ScheduleErrorCode = "auth_invalid"
	CodeRecordInvalid     ScheduleErrorCode = "record_invalid"
	CodeParentMissing     ScheduleErrorCode = "parent_missing"
	CodeParentUnavailable ScheduleErrorCode = "parent_unavailable"
	CodeUnknown           ScheduleErrorCode = "unknown"
)

// UnmarshalJSON rejects unknown codes.
func (c *ScheduleErrorCode) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	switch code := ScheduleErrorCode(value); code {
	case CodeTransientNetwork, CodeRateLimited, CodeAuthInvalid, CodeRecordInvalid,
		CodeParentMissing, CodeParentUnavailable, CodeUnknown:
		*c = code
		return nil
	}
	return fmt.Errorf("scheduling: unknown schedule error code %q", value)
}

// ScheduleError is the error recorded on a schedule; it is also a Go error so
// the worker can raise it directly.
type ScheduleError struct {
	Code           ScheduleErrorCode `json:"code"`
	Message        string            `json:"message"`
	Classification ScheduleErrorCode `json:"classification"`
	RetryAfter     *string           `json:"retryAfter,omitempty"`
}

func newScheduleError(code ScheduleErrorCode, message string) *ScheduleError {
	return &ScheduleError{Code: code, Message: message, Classification: code}
}

func (e *ScheduleError) Error() string { return e.Message }

// UnmarshalJSON accepts a bare legacy string or a partial object.
func (e *ScheduleError) UnmarshalJSON(data []byte) error {
	var legacy string
	if err := json.Unmarshal(data, &legacy); err == nil {
		*e = *newScheduleError(CodeUnknown, legacy)
		return nil
	}
	fields, err := decodeFields(data)
	if err != nil {
		return err
	}
	code := CodeUnknown
	if _, err := fields.optional("code", &code); err != nil {
		return err
	}
	message := "Unknown schedule error"
	if _, err := fields.optional("message", &message); err != nil {
		return err
	}
	classification := code
	if _, err := fields.optional("classification", &classification); err != nil {
		return err
	}
	retryAfter, err := fields.optionalString("retryAfter")
	if err != nil {
		return err
	}
	*e = ScheduleError{Code: code, Message: message, Classification: classification, RetryAfter: retryAfter}
	return nil
}
