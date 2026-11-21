package operations

import (
	"context"
	"time"
)

// ProbeResult is OperationsServiceProbeResult: service-specific evidence for
// one heartbeat. A service probes its own required dependencies and freshness
// source; the heartbeat adds the operations database probe and telemetry evidence.
type ProbeResult struct {
	Liveness        HealthState
	Readiness       HealthState
	Freshness       HealthState
	Completeness    HealthState
	DependencyState map[string]string
	// RequiredDependencyKeys defaults to every DependencyState key when nil;
	// an empty non-nil set means no dependencies were declared.
	RequiredDependencyKeys map[string]struct{}
	ObservedAt             time.Time
	ValidUntil             time.Time
}

// DependencyProbe is OperationsServiceDependencyProbe.
type DependencyProbe func(ctx context.Context) (ProbeResult, error)

func (p ProbeResult) requiredKeys() map[string]struct{} {
	if p.RequiredDependencyKeys != nil {
		return p.RequiredDependencyKeys
	}
	keys := make(map[string]struct{}, len(p.DependencyState))
	for key := range p.DependencyState {
		keys[key] = struct{}{}
	}
	return keys
}
