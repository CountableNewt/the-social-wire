package scheduledata

import (
	"context"
	"errors"
	"testing"
)

func stringPointer(value string) *string { return &value }

func TestRemoteVaultRejectsPlaintextAndPendingCleanupWithoutModifyingRows(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:", "fixture-key", true)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.db.Writer().Exec("INSERT INTO oauth_sessions(did,handle,token_json,dpop_key_json,updated_at) VALUES('actor','actor','private-plaintext','private-key','now')"); err != nil {
		t.Fatal(err)
	}
	if err := store.verifyRemoteVault(ctx, store.db.Reader()); !errors.Is(err, errRemoteVault) {
		t.Fatalf("plaintext result: %v", err)
	}
	var raw string
	if err := store.db.Reader().QueryRow("SELECT token_json FROM oauth_sessions").Scan(&raw); err != nil || raw != "private-plaintext" {
		t.Fatal("remote validation rewrote plaintext")
	}
	if err := store.CreateOAuthSession(ctx, OAuthSession{DID: "actor", Handle: stringPointer("actor"), TokenJSON: "{}", DPoPKeyJSON: "{}"}, "now"); err != nil {
		t.Fatal(err)
	}
	if err := store.verifyRemoteVault(ctx, store.db.Reader()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Writer().Exec("INSERT OR REPLACE INTO auth_vault_cleanup(id,pending) VALUES(1,1)"); err != nil {
		t.Fatal(err)
	}
	if err := store.verifyRemoteVault(ctx, store.db.Reader()); !errors.Is(err, errRemoteVault) {
		t.Fatalf("cleanup result: %v", err)
	}
}

func TestSchedulingReadinessIsExplicitAndKeyBound(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:", "fixture-key", true)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.verifyReady(ctx); err == nil {
		t.Fatal("local migration alone certified remote readiness")
	}
	if err := store.markReady(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.verifyReady(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateOAuthSession(ctx, OAuthSession{DID: "actor", Handle: stringPointer("actor"), TokenJSON: "{}", DPoPKeyJSON: "{}"}, "now"); err != nil {
		t.Fatal(err)
	}
	wrong, err := encryptedStore(store.db, "wrong-key", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := wrong.verifyReady(ctx); err == nil {
		t.Fatal("wrong encryption key was accepted")
	}
	if err := store.invalidateReadiness(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.verifyReady(ctx); err == nil {
		t.Fatal("partial scheduler initialization remained ready")
	}
}

func TestSchedulingProbeChecksConnectivityWithoutRescanningVault(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:", "fixture-key", true)
	if err != nil {
		t.Fatal(err)
	}
	// The live probe checks connectivity, while startup retains full vault checks.
	if _, err := store.db.Writer().Exec("INSERT INTO oauth_sessions(did,handle,token_json,dpop_key_json,updated_at) VALUES('actor','actor','invalid-private-plaintext','key','now')"); err != nil {
		t.Fatal(err)
	}
	if err := store.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.verifyRemoteVault(ctx, store.db.Reader()); err == nil {
		t.Fatal("startup vault check was weakened")
	}
	if err := store.markReady(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.verifySchemaVersion(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.invalidateReadiness(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.verifySchemaVersion(ctx); err == nil {
		t.Fatal("invalidated scheduling marker remained ready")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Ready(ctx); err == nil {
		t.Fatal("closed scheduling database reported ready")
	}
}
