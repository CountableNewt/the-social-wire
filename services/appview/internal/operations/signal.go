package operations

import "time"

// MetricSample is OperationsMetricSample: one additive sample rolled into a
// one-minute bucket.
type MetricSample struct {
	Name       string
	Value      float64
	Dimensions map[string]string
	// RecordedAt defaults to the time of persistence when zero.
	RecordedAt time.Time
}

// Event is OperationsEvent.
type Event struct {
	// ID defaults to a random lowercased UUID when empty.
	ID          string
	Service     string
	Environment string
	InstanceID  string
	Name        string
	// OccurredAt defaults to the time of persistence when zero.
	OccurredAt time.Time
	RequestID  *string
	TraceID    *string
	Attributes map[string]string
}

// TraceSpan is TraceSpan: one sampled or failed request span.
type TraceSpan struct {
	// ID defaults to a random lowercased UUID when empty.
	ID           string
	Environment  string
	TraceID      string
	ParentSpanID *string
	Service      string
	Name         string
	StartedAt    time.Time
	DurationMs   float64
	Status       string
	Attributes   map[string]string
	ExpiresAt    time.Time
}

// Signal is OperationsTelemetrySignal: exactly one of Metric, Event or Span is set.
type Signal struct {
	Metric *MetricSample
	Event  *Event
	Span   *TraceSpan
}

// Metric builds a metric signal recorded now.
func Metric(name string, value float64, dimensions map[string]string) Signal {
	return Signal{Metric: &MetricSample{Name: name, Value: value, Dimensions: dimensions, RecordedAt: time.Now()}}
}

// MetricSignal wraps sample.
func MetricSignal(sample MetricSample) Signal { return Signal{Metric: &sample} }

// EventSignal wraps event.
func EventSignal(event Event) Signal { return Signal{Event: &event} }

// SpanSignal wraps span.
func SpanSignal(span TraceSpan) Signal { return Signal{Span: &span} }
