package operations

import (
	"context"
	"log/slog"
	"maps"
	"os"
	"time"
)

// HeartbeatJob is OperationsHeartbeatJob: every five seconds it probes the
// operations database and the service, then upserts the service heartbeat.
type HeartbeatJob struct {
	store       *Store
	service     string
	environment string
	instanceID  string
	probe       DependencyProbe
	telemetry   *TelemetryBuffer
	logger      *slog.Logger
}

// NewHeartbeatJob builds a heartbeat. probe and telemetry may be nil; a nil
// probe publishes Unknown, never Healthy.
func NewHeartbeatJob(store *Store, service, environment, instanceID string, probe DependencyProbe, telemetry *TelemetryBuffer, logger *slog.Logger) *HeartbeatJob {
	if logger == nil {
		logger = slog.Default()
	}
	return &HeartbeatJob{store: store, service: service, environment: environment,
		instanceID: instanceID, probe: probe, telemetry: telemetry, logger: logger}
}

// Run publishes heartbeats until ctx is cancelled.
func (j *HeartbeatJob) Run(ctx context.Context) {
	startedAt := time.Now()
	for ctx.Err() == nil {
		if err := j.runOnce(ctx, startedAt, time.Now); err != nil {
			// A failed operations database probe cannot safely publish a green
			// replacement heartbeat; the last evidence expires to Unknown.
			j.logger.Warn("Operations heartbeat failed", "error_type", ErrorCategory(err))
		}
		sleep(ctx, 5*time.Second)
	}
}

func (j *HeartbeatJob) runOnce(ctx context.Context, startedAt time.Time, now func() time.Time) error {
	if err := j.store.Ping(ctx); err != nil {
		return err
	}
	evaluated := j.evaluateProbe(ctx, now)
	dependencies := maps.Clone(evaluated.DependencyState)
	if dependencies == nil {
		dependencies = map[string]string{}
	}
	dependencies["operations_database"] = "ready"
	freshness := evaluated.Freshness
	completeness := evaluated.Completeness
	if j.telemetry != nil {
		evidence := TelemetryEvidence(j.telemetry.Snapshot(), now())
		maps.Copy(dependencies, evidence.DependencyState)
		if evidence.FreshnessUncertain {
			freshness = unknownIfHealthy(freshness)
		} else if evidence.ExportFailureObserved {
			freshness = downgradeHealthy(freshness)
		}
		if evidence.CompletenessUncertain {
			completeness = unknownIfHealthy(completeness)
		} else if evidence.DropObserved {
			completeness = downgradeHealthy(completeness)
		}
	}
	var version *string
	if value, ok := os.LookupEnv("RAILWAY_DEPLOYMENT_ID"); ok {
		version = &value
	}
	state := ServiceState{
		Service:         j.service,
		Environment:     j.environment,
		InstanceID:      j.instanceID,
		Liveness:        evaluated.Liveness,
		Readiness:       evaluated.Readiness,
		Freshness:       freshness,
		Completeness:    completeness,
		DependencyState: dependencies,
		Version:         version,
		StartedAt:       startedAt,
		HeartbeatAt:     now(),
	}
	if err := j.store.UpsertServiceState(ctx, state); err != nil {
		return err
	}
	if j.service != "coordinator-appview" {
		j.enqueueHealthSamples(state)
		return nil
	}
	// Publish historical active-owner samples only after the store revalidates
	// role ownership; a fresh standby heartbeat is insufficient.
	states, err := j.store.ListServiceStates(ctx)
	if err != nil {
		return err
	}
	for _, published := range states {
		if published.Service == j.service && published.InstanceID == j.instanceID {
			if published.DependencyState["coordinator_authority"] == "active" &&
				published.DependencyState["coordinator_role"] == "indexing.appview-coordinator" {
				j.enqueueHealthSamples(published)
			}
			return nil
		}
	}
	return nil
}

func (j *HeartbeatJob) enqueueHealthSamples(state ServiceState) {
	if j.telemetry == nil {
		return
	}
	service := prefixRunes(state.Service, 64)
	ownership := map[string]string{}
	if state.Service == "coordinator-appview" &&
		state.DependencyState["coordinator_authority"] == "active" &&
		state.DependencyState["coordinator_role"] == "indexing.appview-coordinator" {
		ownership = map[string]string{"coordinator_authority": "active", "coordinator_role": "indexing.appview-coordinator"}
	}
	for _, sample := range []struct {
		dimension string
		state     HealthState
	}{
		{"liveness", state.Liveness},
		{"readiness", state.Readiness},
		{"freshness", state.Freshness},
		{"completeness", state.Completeness},
	} {
		dimensions := map[string]string{"service": service, "dimension": sample.dimension, "state": string(sample.state)}
		maps.Copy(dimensions, ownership)
		j.telemetry.Enqueue(Metric("socialwire.service.health.samples_total", 1, dimensions))
	}
}

func (j *HeartbeatJob) evaluateProbe(ctx context.Context, now func() time.Time) ProbeResult {
	if j.probe == nil {
		return unknownProbeResult("missing", now())
	}
	result, err := j.probe(ctx)
	if err != nil {
		evaluatedAt := now()
		return ProbeResult{
			Liveness: Degraded, Readiness: Degraded, Freshness: Unknown, Completeness: Unknown,
			DependencyState:        map[string]string{"service_probe": "failed:" + ErrorCategory(err)},
			RequiredDependencyKeys: map[string]struct{}{"service_probe": {}},
			ObservedAt:             evaluatedAt, ValidUntil: evaluatedAt,
		}
	}
	required := result.requiredKeys()
	evaluatedAt := now()
	dependencies := maps.Clone(result.DependencyState)
	if dependencies == nil {
		dependencies = map[string]string{}
	}
	if result.ObservedAt.After(evaluatedAt) || !result.ValidUntil.After(evaluatedAt) {
		dependencies["service_probe"] = "expired"
		return ProbeResult{
			Liveness: Unknown, Readiness: Unknown, Freshness: Unknown, Completeness: Unknown,
			DependencyState: dependencies, RequiredDependencyKeys: required,
			ObservedAt: result.ObservedAt, ValidUntil: result.ValidUntil,
		}
	}
	if len(required) == 0 {
		return unknownProbeResult("missing_dependencies", evaluatedAt)
	}
	ready := true
	for key := range required {
		if value, ok := result.DependencyState[key]; !ok || (value != "ready" && value != "healthy") {
			ready = false
			break
		}
	}
	if !ready {
		dependencies["service_probe"] = "degraded"
		return ProbeResult{
			Liveness:        downgradeHealthy(result.Liveness),
			Readiness:       downgradeHealthy(result.Readiness),
			Freshness:       downgradeHealthy(result.Freshness),
			Completeness:    downgradeHealthy(result.Completeness),
			DependencyState: dependencies, RequiredDependencyKeys: required,
			ObservedAt: result.ObservedAt, ValidUntil: result.ValidUntil,
		}
	}
	dependencies["service_probe"] = "ready"
	return ProbeResult{
		Liveness: result.Liveness, Readiness: result.Readiness,
		Freshness: result.Freshness, Completeness: result.Completeness,
		DependencyState: dependencies, RequiredDependencyKeys: required,
		ObservedAt: result.ObservedAt, ValidUntil: result.ValidUntil,
	}
}

func unknownProbeResult(reason string, at time.Time) ProbeResult {
	return ProbeResult{
		Liveness: Unknown, Readiness: Unknown, Freshness: Unknown, Completeness: Unknown,
		DependencyState:        map[string]string{"service_probe": reason},
		RequiredDependencyKeys: map[string]struct{}{"service_probe": {}},
		ObservedAt:             at, ValidUntil: at,
	}
}
