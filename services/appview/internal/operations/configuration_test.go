package operations

import (
	"errors"
	"testing"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
)

func TestRequireEnvironment(t *testing.T) {
	for value, want := range map[string]string{"dev": "dev", " PROD ": "prod"} {
		got, err := RequireEnvironment(config.Env{"APP_ENV": value})
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v", value, got, err)
		}
	}
	for _, env := range []config.Env{{}, {"APP_ENV": ""}, {"APP_ENV": "local"}, {"APP_ENV": "production"}} {
		if _, err := RequireEnvironment(env); !errors.Is(err, ErrInvalidOrMissingEnvironment) {
			t.Errorf("%v: err %v", env, err)
		}
	}
}

func TestLoadConfigurationDefaults(t *testing.T) {
	configuration := LoadConfiguration(config.Env{"RAILWAY_REPLICA_ID": "replica-1"})
	if !configuration.Enabled || configuration.Environment != "__missing__" || configuration.InstanceID != "replica-1" {
		t.Fatalf("%+v", configuration)
	}
	disabled := LoadConfiguration(config.Env{"OPERATIONS_TELEMETRY_ENABLED": "off", "APP_ENV": " dev "})
	if disabled.Enabled || disabled.Environment != "dev" {
		t.Fatalf("%+v", disabled)
	}
	if LoadConfiguration(config.Env{"OPERATIONS_INDEX_FAILURE_RATIO": "2"}).IndexFailureRatio != 0.01 {
		t.Fatal("out-of-range ratio accepted")
	}
}

func TestRetiredGatewayOperationsSecretIsIgnored(t *testing.T) {
	configuration := LoadConfiguration(config.Env{"GATEWAY_OPERATIONS_INTERNAL_SECRET": "retired-secret"})
	if configuration.BackfillFingerprintSecret != "" {
		t.Fatal("retired Operations gateway secret was retained")
	}
	configuration = LoadConfiguration(config.Env{"OPERATIONS_BACKFILL_FINGERPRINT_SECRET": "fingerprint-secret"})
	if configuration.BackfillFingerprintSecret != "fingerprint-secret" {
		t.Fatal("dedicated fingerprint secret was not retained")
	}
}
