package operations

// HealthState is OperationsHealthState: one service health dimension.
type HealthState string

// Health states, stored verbatim in operations_service_state.
const (
	Healthy   HealthState = "healthy"
	Degraded  HealthState = "degraded"
	Unhealthy HealthState = "unhealthy"
	Unknown   HealthState = "unknown"
)

func validHealthState(value string) (HealthState, bool) {
	switch HealthState(value) {
	case Healthy, Degraded, Unhealthy, Unknown:
		return HealthState(value), true
	}
	return "", false
}

// downgradeHealthy lowers Healthy to Degraded and keeps every other state.
func downgradeHealthy(state HealthState) HealthState {
	if state == Healthy {
		return Degraded
	}
	return state
}

// unknownIfHealthy lowers Healthy to Unknown and keeps every other state.
func unknownIfHealthy(state HealthState) HealthState {
	if state == Healthy {
		return Unknown
	}
	return state
}
