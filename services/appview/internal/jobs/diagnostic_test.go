package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	turso "turso.tech/database/tursogo-serverless"
)

func TestMaintenanceFailureLogContainsOnlySafeDiagnostics(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	logFailure("scheduling", "action", &turso.Error{Code: "SQLITE_BUSY", Message: "private SQL secret-key"})
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	for key, expected := range map[string]string{"job": "scheduling", "phase": "action", "error_kind": "turso", "error_code": "SQLITE_BUSY"} {
		if record[key] != expected {
			t.Fatalf("missing safe %s", key)
		}
	}
	output.Reset()
	logFailure("scheduling", "claim", &turso.Error{Code: "SELECT private SQL", Message: "secret-key"})
	if strings.Contains(output.String(), "private") || strings.Contains(output.String(), "secret-key") || !strings.Contains(output.String(), "UNKNOWN") {
		t.Fatal("unsafe provider details logged")
	}
	output.Reset()
	logFailure("scheduling", "context", context.DeadlineExceeded)
	if !strings.Contains(output.String(), "DEADLINE_EXCEEDED") {
		t.Fatal("context failure missing")
	}
	output.Reset()
	logFailure("scheduling", "claim", nil)
	if output.Len() != 0 {
		t.Fatal("successful phase logged as failure")
	}
}
