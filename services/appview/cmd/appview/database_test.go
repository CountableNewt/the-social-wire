package main

import (
	"context"
	"testing"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

func TestRuntimeReadinessRequiresCompletedInitializationAndMatchingFeatures(t *testing.T) {
	db, err := sqlitedb.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := verifyRuntimeSchema(ctx, db, false); err == nil {
		t.Fatal("empty database was ready")
	}
	var count int
	if err := db.Reader().QueryRow("SELECT count(*) FROM sqlite_schema WHERE name='appview_schema_readiness'").Scan(&count); err != nil || count != 0 {
		t.Fatal("readiness verification created schema")
	}
	if err := markRuntimeSchema(ctx, db, false); err != nil {
		t.Fatal(err)
	}
	if err := verifyRuntimeSchema(ctx, db, false); err != nil {
		t.Fatal(err)
	}
	if err := verifyRuntimeSchema(ctx, db, true); err == nil {
		t.Fatal("uninitialized drafts accepted")
	}
	if err := markRuntimeSchema(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	if err := verifyRuntimeSchema(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	if err := invalidateRuntimeSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := verifyRuntimeSchema(ctx, db, false); err == nil {
		t.Fatal("partial reinitialization left readiness enabled")
	}
}
