package config

import (
	"strings"
	"testing"
)

func remoteEnvironment() Env {
	return Env{"APP_ENV": "dev", "PDS_ATTESTATION_RECEIPT_SECRET": "fixture", "DATABASE_BACKEND": "turso", "TURSO_DATABASE_URL": "turso://app.example", "TURSO_AUTH_TOKEN": "app-token", "SCHEDULING_TURSO_DATABASE_URL": "https://scheduler.example/", "SCHEDULING_TURSO_AUTH_TOKEN": "scheduler-token"}
}

func TestTursoConfigurationIsExplicitAndKeepsDatabasesDistinct(t *testing.T) {
	env := remoteEnvironment()
	core, err := LoadCore(env)
	if err != nil {
		t.Fatal(err)
	}
	if core.DatabaseBackend != "turso" || core.SQLitePath != "" || core.TursoDatabaseURL != "https://app.example" || core.SchedulingTursoDatabaseURL != "https://scheduler.example" {
		t.Fatalf("unexpected remote configuration")
	}
	for _, duplicate := range []string{"https://APP.example/", "https://app.example:443", "https://app.example./"} {
		env["SCHEDULING_TURSO_DATABASE_URL"] = duplicate
		if _, err := LoadCore(env); err == nil {
			t.Fatal("shared scheduling/AppView database was accepted")
		}
	}
}

func TestTursoConfigurationRejectsPartialOrUnsafeCredentialsWithoutDisclosure(t *testing.T) {
	for _, key := range []string{"TURSO_DATABASE_URL", "TURSO_AUTH_TOKEN", "SCHEDULING_TURSO_DATABASE_URL", "SCHEDULING_TURSO_AUTH_TOKEN"} {
		env := remoteEnvironment()
		delete(env, key)
		if _, err := LoadCore(env); err == nil {
			t.Fatalf("accepted missing %s", key)
		}
	}
	for _, endpoint := range []string{"http://app.example", "turso://user:private-secret@app.example", "https://app.example?authToken=private-secret", "https://app.example/#private-secret", "https://app.example/database", "file:/tmp/database"} {
		env := remoteEnvironment()
		env["TURSO_DATABASE_URL"] = endpoint
		_, err := LoadCore(env)
		if err == nil || strings.Contains(err.Error(), "private-secret") {
			t.Fatalf("unsafe endpoint result: %v", err)
		}
	}
	env := remoteEnvironment()
	env["DATABASE_BACKEND"] = "sqlite"
	if _, err := LoadCore(env); err == nil {
		t.Fatal("remote credentials silently ignored in local mode")
	}
	env = remoteEnvironment()
	env["DATABASE_BACKEND"] = "libsql"
	if _, err := LoadCore(env); err == nil {
		t.Fatal("retired engine selection accepted")
	}
}
