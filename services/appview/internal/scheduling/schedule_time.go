package scheduling

import "time"

// IsQuarterHour constrains newly selected publish times, not existing PDS records
// consumed by the worker. Historical off-grid schedules remain publishable.
func IsQuarterHour(date time.Time) bool {
	utc := date.UTC()
	return utc.Minute()%15 == 0 && utc.Second() == 0 && utc.Nanosecond() == 0
}
