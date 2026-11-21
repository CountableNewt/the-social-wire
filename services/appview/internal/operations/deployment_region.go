package operations

import (
	"os"
	"strings"
)

// deploymentRegion normalizes RAILWAY_REPLICA_REGION to at most 32
// alphanumeric, "-" or "_" characters, or "unknown".
func deploymentRegion() string {
	return normalizedDeploymentRegion(os.Getenv("RAILWAY_REPLICA_REGION"))
}

func normalizedDeploymentRegion(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var builder strings.Builder
	for _, r := range value {
		if isAlphanumeric(r) || r == '-' || r == '_' {
			builder.WriteRune(r)
		}
	}
	if builder.Len() == 0 {
		return "unknown"
	}
	return prefixRunes(builder.String(), 32)
}
