package scheduling

import (
	"encoding/json"
	"fmt"
)

// ScheduleStatus is an at.skej.schedule lifecycle state.
type ScheduleStatus string

// Schedule states.
const (
	StatusDraft      ScheduleStatus = "draft"
	StatusScheduled  ScheduleStatus = "scheduled"
	StatusBlocked    ScheduleStatus = "blocked"
	StatusPublishing ScheduleStatus = "publishing"
	StatusPublished  ScheduleStatus = "published"
	StatusFailed     ScheduleStatus = "failed"
	StatusCanceled   ScheduleStatus = "canceled"
)

func knownStatus(value string) (ScheduleStatus, bool) {
	switch status := ScheduleStatus(value); status {
	case StatusDraft, StatusScheduled, StatusBlocked, StatusPublishing, StatusPublished, StatusFailed, StatusCanceled:
		return status, true
	}
	if value == "cancelled" {
		return StatusCanceled, true
	}
	return "", false
}

// UnmarshalJSON accepts the British spelling and maps unknown states to failed.
func (s *ScheduleStatus) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if status, ok := knownStatus(value); ok {
		*s = status
	} else {
		*s = StatusFailed
	}
	return nil
}

// TimezonePolicy says how a schedule's time was chosen.
type TimezonePolicy string

// Timezone policies.
const (
	TimezoneAbsoluteUTC  TimezonePolicy = "absolute_utc"
	TimezoneAccountLocal TimezonePolicy = "account_local"
	TimezoneUserLocal    TimezonePolicy = "user_local"
)

// UnmarshalJSON rejects unknown policies, like Swift's synthesized enum decode.
func (p *TimezonePolicy) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	switch policy := TimezonePolicy(value); policy {
	case TimezoneAbsoluteUTC, TimezoneAccountLocal, TimezoneUserLocal:
		*p = policy
		return nil
	}
	return fmt.Errorf("scheduling: unknown timezone policy %q", value)
}

// DependencyRelationship ties a schedule to its parent.
type DependencyRelationship string

// Dependency relationships.
const (
	RelationshipAfter DependencyRelationship = "after"
	RelationshipReply DependencyRelationship = "reply"
	RelationshipQuote DependencyRelationship = "quote"
)

// UnmarshalJSON rejects unknown relationships.
func (r *DependencyRelationship) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	switch relationship := DependencyRelationship(value); relationship {
	case RelationshipAfter, RelationshipReply, RelationshipQuote:
		*r = relationship
		return nil
	}
	return fmt.Errorf("scheduling: unknown dependency relationship %q", value)
}
