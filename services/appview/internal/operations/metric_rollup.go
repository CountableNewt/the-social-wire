package operations

import "time"

// MetricRollup is OperationsMetricRollup: one persisted minute bucket.
type MetricRollup struct {
	Environment string
	BucketStart time.Time
	MetricName  string
	Dimensions  map[string]string
	SampleCount int
	ValueSum    float64
	ValueMin    *float64
	ValueMax    *float64
}
