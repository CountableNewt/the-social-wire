package operations

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	turso "turso.tech/database/tursogo-serverless"
)

func TestTelemetryDiagnosticsNeverLogPrivateProviderDetails(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	logTelemetryFailure(logger, "Telemetry export stopped without a safe retry", &turso.HTTPError{StatusCode: 404, Message: "SELECT private_secret https://private.example/token"}, 3)
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["error_kind"] != "http" || record["error_code"] != "404" || record["batch_size"] != "3" {
		t.Fatal("safe telemetry diagnostic missing")
	}
	if strings.Contains(output.String(), "private") || strings.Contains(output.String(), "SELECT") {
		t.Fatal("provider details leaked")
	}
	output.Reset()
	logTelemetryFailure(logger, "Telemetry export exhausted bounded retries", &turso.Error{Code: "SELECT private_secret", Message: "private URL"}, 1)
	if strings.Contains(output.String(), "private") || !strings.Contains(output.String(), "UNKNOWN") {
		t.Fatal("invalid provider machine code leaked")
	}
}
