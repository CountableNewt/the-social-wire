package main

import (
	"context"
	"encoding/base64"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

func TestInitializeOnlyIsRepeatable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "volume", "social-wire.sqlite")
	env := config.Env{
		"APP_ENV":                           "dev",
		"BACKEND_MODE":                      "standalone",
		"SQLITE_DB_PATH":                    path,
		"ENABLE_THIN_APPVIEW":               "true",
		"PDS_ATTESTATION_RECEIPT_SECRET":    "startup-fixture-attestation-secret",
		"SOCIAL_DRAFTS_ENABLED":             "true",
		"SOCIAL_DRAFT_STORAGE_KEY":          base64.StdEncoding.EncodeToString(make([]byte, 32)),
		"SOCIAL_DRAFT_S3_ENDPOINT":          "https://example.com",
		"SOCIAL_DRAFT_S3_BUCKET":            "fixture",
		"SOCIAL_DRAFT_S3_ACCESS_KEY_ID":     "fixture",
		"SOCIAL_DRAFT_S3_SECRET_ACCESS_KEY": "fixture",
		"SOCIAL_DRAFT_S3_REGION":            "auto",
	}
	ctx := context.Background()
	for range 2 {
		if err := run(ctx, []string{"--initialize-only"}, env, slog.Default()); err != nil {
			t.Fatal(err)
		}
	}
	db, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, table := range []string{"content_items", "operations_service_state", "private_social_draft_accounts"} {
		var name string
		if err := db.Reader().QueryRowContext(ctx, "SELECT name FROM sqlite_schema WHERE type = 'table' AND name = ?", table).Scan(&name); err != nil {
			t.Errorf("missing %s schema: %v", table, err)
		}
	}
}
