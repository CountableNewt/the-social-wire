package operations

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
)

// ErrInvalidOrMissingEnvironment is OperationsConfigurationError.invalidOrMissingEnvironment:
// startup fails unless APP_ENV is dev or prod.
var ErrInvalidOrMissingEnvironment = errors.New("operations: APP_ENV must be dev or prod")

// Configuration is OperationsConfiguration. Only Enabled, Environment and
// InstanceID drive the standalone AppView; the remaining thresholds are loaded
// for parity with the Swift loader.
type Configuration struct {
	Enabled                   bool
	RecoveryEnabled           bool
	AlertDeliveryEnabled      bool
	Environment               string
	InstanceID                string
	OperatorDIDs              map[string]struct{}
	WebhookURL                string
	WebhookSecret             string
	BackfillFingerprintSecret string
	ReplayRewindMicroseconds  int64
	DisconnectAlertSeconds    float64
	IdleAlertSeconds          float64
	CommitStaleSeconds        float64
	BacklogAlertMicroseconds  int64
	BackfillStallSeconds      float64
	IndexFailureRatio         float64
	IndexFailureMinimum       int
	AppView5xxRatio           float64
	AppView5xxMinimumRequests int
	BootstrapP95Seconds       float64
	EntriesP95Seconds         float64
	UnreadCountsP95Seconds    float64
	SidebarP95Seconds         float64
	DatabaseQueryP95Seconds   float64
	ResponseFreshnessSeconds  float64
	ResponseFreshnessRatio    float64
}

// LoadConfiguration mirrors OperationsConfiguration.fromEnvironment. Environment
// is the trimmed APP_ENV (not lowercased), or "__missing__".
func LoadConfiguration(env config.Env) Configuration {
	operatorDIDs := map[string]struct{}{}
	for part := range strings.SplitSeq(env["OPERATIONS_OPERATOR_DIDS"], ",") {
		if part = strings.TrimSpace(part); part != "" {
			operatorDIDs[part] = struct{}{}
		}
	}
	environment := nonEmpty(env, "APP_ENV")
	if environment == "" {
		environment = "__missing__"
	}
	instanceID, ok := env["RAILWAY_REPLICA_ID"]
	if !ok {
		instanceID, _ = os.Hostname()
	}
	fingerprint := nonEmpty(env, "OPERATIONS_BACKFILL_FINGERPRINT_SECRET")
	if fingerprint == "" {
		fingerprint = nonEmpty(env, "GATEWAY_OPERATIONS_INTERNAL_SECRET")
	}
	return Configuration{
		Enabled:                   truthy(env, "OPERATIONS_TELEMETRY_ENABLED", true),
		RecoveryEnabled:           truthy(env, "OPERATIONS_RECOVERY_ENABLED", false),
		AlertDeliveryEnabled:      truthy(env, "OPERATIONS_ALERT_DELIVERY_ENABLED", false),
		Environment:               environment,
		InstanceID:                instanceID,
		OperatorDIDs:              operatorDIDs,
		WebhookURL:                nonEmpty(env, "OPERATIONS_ALERT_WEBHOOK_URL"),
		WebhookSecret:             nonEmpty(env, "OPERATIONS_ALERT_WEBHOOK_SECRET"),
		BackfillFingerprintSecret: fingerprint,
		ReplayRewindMicroseconds:  positiveInt64(env, "OPERATIONS_REPLAY_REWIND_MICROSECONDS", 5_000_000),
		DisconnectAlertSeconds:    positiveSeconds(env, "OPERATIONS_DISCONNECT_ALERT_SECONDS", 120),
		IdleAlertSeconds:          positiveSeconds(env, "OPERATIONS_IDLE_ALERT_SECONDS", 300),
		CommitStaleSeconds:        positiveSeconds(env, "OPERATIONS_COMMIT_STALE_SECONDS", 300),
		BacklogAlertMicroseconds:  positiveInt64(env, "OPERATIONS_BACKLOG_ALERT_MICROSECONDS", 60_000_000),
		BackfillStallSeconds:      positiveSeconds(env, "OPERATIONS_BACKFILL_STALL_SECONDS", 600),
		IndexFailureRatio:         ratio(env, "OPERATIONS_INDEX_FAILURE_RATIO", 0.01),
		IndexFailureMinimum:       int(positiveInt64(env, "OPERATIONS_INDEX_FAILURE_MINIMUM", 10)),
		AppView5xxRatio:           ratio(env, "OPERATIONS_APPVIEW_5XX_RATIO", 0.02),
		AppView5xxMinimumRequests: int(positiveInt64(env, "OPERATIONS_APPVIEW_5XX_MINIMUM_REQUESTS", 20)),
		BootstrapP95Seconds:       positiveSeconds(env, "OPERATIONS_BOOTSTRAP_P95_SECONDS", 5),
		EntriesP95Seconds:         positiveSeconds(env, "OPERATIONS_ENTRIES_P95_SECONDS", 2),
		UnreadCountsP95Seconds:    positiveSeconds(env, "OPERATIONS_UNREAD_COUNTS_P95_SECONDS", 1.5),
		SidebarP95Seconds:         positiveSeconds(env, "OPERATIONS_SIDEBAR_P95_SECONDS", 3),
		DatabaseQueryP95Seconds:   positiveSeconds(env, "OPERATIONS_DATABASE_QUERY_P95_SECONDS", 1),
		ResponseFreshnessSeconds:  positiveSeconds(env, "OPERATIONS_RESPONSE_FRESHNESS_SECONDS", 300),
		ResponseFreshnessRatio:    ratio(env, "OPERATIONS_RESPONSE_FRESHNESS_RATIO", 0.05),
	}
}

// RequireEnvironment mirrors OperationsConfiguration.requireEnvironment: the
// trimmed, lowercased APP_ENV when it is dev or prod, else
// ErrInvalidOrMissingEnvironment. Startup must fail on that error.
func RequireEnvironment(env config.Env) (string, error) {
	return RequireEnvironmentValue(env["APP_ENV"])
}

// RequireEnvironmentValue validates one APP_ENV value.
func RequireEnvironmentValue(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value != "dev" && value != "prod" {
		return "", ErrInvalidOrMissingEnvironment
	}
	return value, nil
}

func truthy(env config.Env, key string, fallback bool) bool {
	value, ok := env[key]
	if !ok {
		return fallback
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func nonEmpty(env config.Env, key string) string { return strings.TrimSpace(env[key]) }

func positiveSeconds(env config.Env, key string, fallback float64) float64 {
	parsed, err := strconv.ParseFloat(env[key], 64)
	if err != nil || !(parsed > 0) {
		return fallback
	}
	return parsed
}

func positiveInt64(env config.Env, key string, fallback int64) int64 {
	parsed, err := strconv.ParseInt(env[key], 10, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func ratio(env config.Env, key string, fallback float64) float64 {
	parsed, err := strconv.ParseFloat(env[key], 64)
	if err != nil || !(parsed > 0) || parsed > 1 {
		return fallback
	}
	return parsed
}
