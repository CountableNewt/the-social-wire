package operations

import "time"

// ServiceState is OperationsServiceState: one instance heartbeat row.
type ServiceState struct {
	Service         string
	Environment     string
	InstanceID      string
	Liveness        HealthState
	Readiness       HealthState
	Freshness       HealthState
	Completeness    HealthState
	DependencyState map[string]string
	Version         *string
	StartedAt       time.Time
	HeartbeatAt     time.Time
}
