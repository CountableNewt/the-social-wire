package operations

import (
	"context"
	"database/sql"
	"math"
	"slices"
	"strings"
	"time"
)

// RecordTelemetryBatch persists signals in one write transaction; any
// cross-environment signal rejects the whole batch.
func (s *Store) RecordTelemetryBatch(ctx context.Context, signals []Signal) error {
	if len(signals) == 0 {
		return nil
	}
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		for _, signal := range signals {
			if err := s.insertTelemetry(ctx, tx, signal); err != nil {
				return err
			}
		}
		return nil
	})
}

// RecordMetric persists one metric sample.
func (s *Store) RecordMetric(ctx context.Context, sample MetricSample) error {
	return s.RecordTelemetryBatch(ctx, []Signal{MetricSignal(sample)})
}

// RecordEvent persists one event.
func (s *Store) RecordEvent(ctx context.Context, event Event) error {
	return s.RecordTelemetryBatch(ctx, []Signal{EventSignal(event)})
}

// RecordTraceSpan persists one span.
func (s *Store) RecordTraceSpan(ctx context.Context, span TraceSpan) error {
	return s.RecordTelemetryBatch(ctx, []Signal{SpanSignal(span)})
}

func (s *Store) insertTelemetry(ctx context.Context, tx *sql.Tx, signal Signal) error {
	switch {
	case signal.Metric != nil:
		sample := *signal.Metric
		if environment, ok := sample.Dimensions["environment"]; ok && environment != s.environment {
			return &EnvironmentMismatchError{Expected: s.environment, Actual: environment}
		}
		recordedAt := sample.RecordedAt
		if recordedAt.IsZero() {
			recordedAt = time.Now()
		}
		dimensions := BoundedAttributes(sample.Dimensions)
		bucket := metricBucket(recordedAt)
		_, err := tx.ExecContext(ctx, `
INSERT INTO operations_metric_rollups
  (environment, bucket_start, metric_name, dimensions_hash, dimensions, sample_count,
   value_sum, value_min, value_max, histogram_buckets, expires_at)
VALUES (?, ?, ?, ?, ?, 1, ?, ?, ?, '{}', ?)
ON CONFLICT (environment, bucket_start, metric_name, dimensions_hash) DO UPDATE SET
  sample_count = sample_count + 1, value_sum = value_sum + excluded.value_sum,
  value_min = MIN(value_min, excluded.value_min), value_max = MAX(value_max, excluded.value_max)`,
			s.environment, storeTime(bucket), prefixRunes(sample.Name, 160), dimensionsHash(dimensions),
			swiftJSON(dimensions), sample.Value, sample.Value, sample.Value,
			storeTime(bucket.Add(90*24*time.Hour)))
		return err
	case signal.Event != nil:
		event := *signal.Event
		if event.Environment != s.environment {
			return &EnvironmentMismatchError{Expected: s.environment, Actual: event.Environment}
		}
		if event.ID == "" {
			event.ID = newUUID()
		}
		if event.OccurredAt.IsZero() {
			event.OccurredAt = time.Now()
		}
		_, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO operations_events
  (id, service, environment, instance_id, event_name, occurred_at, request_id,
   trace_id, attributes, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			event.ID, event.Service, event.Environment, event.InstanceID,
			prefixRunes(event.Name, 160), storeTime(event.OccurredAt), event.RequestID, event.TraceID,
			swiftJSON(BoundedAttributes(event.Attributes)),
			storeTime(event.OccurredAt.Add(30*24*time.Hour)))
		return err
	case signal.Span != nil:
		span := *signal.Span
		if span.Environment != s.environment {
			return &EnvironmentMismatchError{Expected: s.environment, Actual: span.Environment}
		}
		if span.ID == "" {
			span.ID = newUUID()
		}
		_, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO operations_trace_spans
  (environment, id, trace_id, parent_span_id, service, name, started_at, duration_ms,
   status, attributes, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			s.environment, span.ID, span.TraceID, span.ParentSpanID, span.Service, span.Name,
			storeTime(span.StartedAt), span.DurationMs, span.Status,
			swiftJSON(BoundedAttributes(span.Attributes)), storeTime(span.ExpiresAt))
		return err
	}
	return nil
}

// metricBucket floors t to its minute.
func metricBucket(t time.Time) time.Time {
	seconds := float64(t.UnixNano()) / 1e9
	return time.Unix(int64(math.Floor(seconds/60)*60), 0).UTC()
}

// dimensionsHash hashes the sorted "key=value&..." identity of dimensions.
func dimensionsHash(dimensions map[string]string) string {
	keys := make([]string, 0, len(dimensions))
	for key := range dimensions {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	parts := make([]string, len(keys))
	for i, key := range keys {
		parts[i] = key + "=" + dimensions[key]
	}
	return HashIdentity(strings.Join(parts, "&"))
}

// ListMetricRollups returns minute buckets in [startAt, endAt], at most limit (1...10000).
func (s *Store) ListMetricRollups(ctx context.Context, startAt, endAt time.Time, limit int) ([]MetricRollup, error) {
	limit = max(1, min(limit, 10_000))
	var rollups []MetricRollup
	err := s.db.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
SELECT environment, bucket_start, metric_name, dimensions, sample_count, value_sum, value_min, value_max
FROM operations_metric_rollups
WHERE environment = ? AND bucket_start >= ? AND bucket_start <= ?
ORDER BY bucket_start ASC, metric_name ASC, dimensions_hash ASC
LIMIT ?`, s.environment, storeTime(startAt), storeTime(endAt), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				rollup             MetricRollup
				bucket, dimensions string
				minimum, maximum   sql.NullFloat64
			)
			if err := rows.Scan(&rollup.Environment, &bucket, &rollup.MetricName, &dimensions,
				&rollup.SampleCount, &rollup.ValueSum, &minimum, &maximum); err != nil {
				return err
			}
			var ok bool
			if rollup.BucketStart, ok = parseStoreTime(bucket); !ok {
				continue
			}
			rollup.Dimensions = decodeStringMap(dimensions)
			if minimum.Valid {
				rollup.ValueMin = &minimum.Float64
			}
			if maximum.Valid {
				rollup.ValueMax = &maximum.Float64
			}
			rollups = append(rollups, rollup)
		}
		return rows.Err()
	})
	return rollups, err
}
