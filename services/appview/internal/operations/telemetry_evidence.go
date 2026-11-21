package operations

import (
	"strconv"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/isotime"
)

// TelemetryHeartbeatEvidence is OperationsTelemetryHeartbeatEvidence.
type TelemetryHeartbeatEvidence struct {
	DependencyState       map[string]string
	ExportFailureObserved bool
	DropObserved          bool
	FreshnessUncertain    bool
	CompletenessUncertain bool
}

// TelemetryEvidence turns a buffer snapshot into heartbeat dependency evidence.
// Invalid snapshots can never support Healthy freshness or completeness.
func TelemetryEvidence(snapshot TelemetrySnapshot, observedAt time.Time) TelemetryHeartbeatEvidence {
	formatOr := func(t *time.Time, fallback string) string {
		if t == nil {
			return fallback
		}
		return isotime.Format(*t)
	}
	dependencies := map[string]string{
		"telemetry_queue_depth":               strconv.Itoa(snapshot.QueueDepth),
		"telemetry_in_flight":                 strconv.Itoa(snapshot.InFlightCount),
		"telemetry_queue_capacity":            strconv.Itoa(snapshot.Capacity),
		"telemetry_dropped_total":             strconv.Itoa(snapshot.DroppedCount),
		"telemetry_consecutive_failures":      strconv.Itoa(snapshot.ConsecutiveFailures),
		"telemetry_last_successful_export_at": formatOr(snapshot.LastSuccessfulExportAt, "none"),
		"telemetry_last_drop_at":              formatOr(snapshot.LastDropAt, "unknown"),
		"telemetry_last_drop_recovered_at":    formatOr(snapshot.LastDropRecoveredAt, "none"),
		"telemetry_snapshot_observed_at":      isotime.Format(observedAt),
	}

	occupied := snapshot.QueueDepth + snapshot.InFlightCount
	structurallyValid := snapshot.QueueDepth >= 0 &&
		snapshot.InFlightCount >= 0 &&
		snapshot.Capacity > 0 &&
		snapshot.DroppedCount >= 0 &&
		snapshot.ConsecutiveFailures >= 0 &&
		(snapshot.DroppedCount > 0 || snapshot.LastDropAt == nil) &&
		occupied >= 0 &&
		occupied <= snapshot.Capacity

	lastExportTimeValid := true
	if snapshot.LastSuccessfulExportAt != nil {
		age := observedAt.Sub(*snapshot.LastSuccessfulExportAt).Seconds()
		if age >= 0 {
			dependencies["telemetry_last_export_age_seconds"] = strconv.FormatFloat(age, 'f', 3, 64)
		} else {
			dependencies["telemetry_last_export_age_seconds"] = "invalid_future"
			lastExportTimeValid = false
		}
	} else {
		dependencies["telemetry_last_export_age_seconds"] = "unknown"
	}

	dropTimesValid := true
	for _, t := range []*time.Time{snapshot.LastDropAt, snapshot.LastDropRecoveredAt} {
		if t != nil && t.After(observedAt) {
			dropTimesValid = false
		}
	}
	recoveryValid := true
	if recovered := snapshot.LastDropRecoveredAt; recovered != nil {
		recoveryValid = snapshot.DroppedCount > 0 &&
			snapshot.LastDropAt != nil && recovered.After(*snapshot.LastDropAt) &&
			snapshot.LastSuccessfulExportAt != nil && !recovered.After(*snapshot.LastSuccessfulExportAt)
	}
	if !structurallyValid || !lastExportTimeValid || !dropTimesValid || !recoveryValid {
		dependencies["telemetry_exporter"] = "unknown_invalid_snapshot"
		return TelemetryHeartbeatEvidence{
			DependencyState:       dependencies,
			FreshnessUncertain:    true,
			CompletenessUncertain: true,
		}
	}

	exportFailureObserved := snapshot.ConsecutiveFailures > 0
	// Cumulative loss remains diagnostic history; never infer recovery from an empty queue.
	dropObserved := snapshot.DroppedCount > 0 && snapshot.LastDropRecoveredAt == nil
	switch {
	case snapshot.DroppedCount == 0:
		dependencies["telemetry_loss_state"] = "none"
	case dropObserved:
		dependencies["telemetry_loss_state"] = "unrecovered"
	default:
		dependencies["telemetry_loss_state"] = "recovered"
	}
	switch {
	case exportFailureObserved || dropObserved:
		dependencies["telemetry_exporter"] = "degraded"
	case snapshot.InFlightCount > 0:
		dependencies["telemetry_exporter"] = "exporting"
	case snapshot.QueueDepth > 0:
		dependencies["telemetry_exporter"] = "queued"
	case snapshot.LastSuccessfulExportAt == nil:
		dependencies["telemetry_exporter"] = "idle_no_export_yet"
	default:
		dependencies["telemetry_exporter"] = "idle"
	}
	return TelemetryHeartbeatEvidence{
		DependencyState:       dependencies,
		ExportFailureObserved: exportFailureObserved,
		DropObserved:          dropObserved,
	}
}
