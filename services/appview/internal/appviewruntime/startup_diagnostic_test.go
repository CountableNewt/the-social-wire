package appviewruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	turso "turso.tech/database/tursogo-serverless"
)

func TestStartupStagePreservesFailureWithoutLoggingPrivateMessage(t *testing.T) {
	for _, cause := range []error{
		&turso.Error{Code: "SQLITE_BUSY", Message: "private SQL token https://private.example"},
		&turso.Error{Code: "private secret", Message: "private SQL token https://private.example"},
		fmt.Errorf("private SQL token https://private.example: %w", context.DeadlineExceeded),
		errors.New("private SQL token https://private.example"),
		nil,
	} {
		var output bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&output, nil))
		calls := 0
		got := startupStage(logger, Maintenance, "scheduling-open", func() error { calls++; return cause })
		if got != cause || calls != 1 {
			t.Fatal("startup stage must return original error without retry")
		}
		if strings.Contains(output.String(), "private") || strings.Contains(output.String(), "https://") {
			t.Fatal("startup log leaked private failure data")
		}
		var record map[string]any
		if err := json.Unmarshal(output.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		if record["stage"] != "scheduling-open" || record["surface"] != "maintenance" || record["duration_ms"].(float64) < 0 {
			t.Fatal("missing safe startup phase timing")
		}
		if cause == nil && record["error_kind"] != "none" {
			t.Fatal("successful stage classified as failure")
		}
		if errors.Is(cause, context.DeadlineExceeded) && record["error_code"] != "DEADLINE_EXCEEDED" {
			t.Fatal("lost wrapped cancellation classification")
		}
	}
}
