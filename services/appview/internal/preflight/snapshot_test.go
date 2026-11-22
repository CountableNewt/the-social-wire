package preflight

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const appviewFixture = `CREATE TABLE content_items(id TEXT PRIMARY KEY,body TEXT);
CREATE TABLE grdb_migrations(identifier TEXT PRIMARY KEY);
INSERT INTO grdb_migrations VALUES('stored-timestamp-microseconds-v1');
INSERT INTO content_items VALUES('entry','private-row-content');`

func snapshotFixture(t *testing.T, statements string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "verified.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(statements); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInspectReportsOnlyInventoryAndLeavesSnapshotUnchanged(t *testing.T) {
	path := snapshotFixture(t, appviewFixture+`CREATE TABLE "odd""table"(value TEXT); INSERT INTO "odd""table" VALUES('another-secret');`)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	first, err := Inspect(context.Background(), path, "appview")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Inspect(context.Background(), path, "appview")
	if err != nil {
		t.Fatal(err)
	}
	if first.SchemaSHA256 != second.SchemaSHA256 || len(first.SchemaSHA256) != 64 || first.DatabaseBytes != int64(len(before)) {
		t.Fatalf("unstable inventory: %+v", first)
	}
	if len(first.Tables) != 3 || first.Tables[0].Name != "content_items" || first.Tables[0].Rows != 1 || first.Tables[2].Name != `odd"table` || first.Tables[2].Rows != 1 {
		t.Fatalf("wrong counts: %+v", first.Tables)
	}
	if len(first.MigrationIdentifiers) != 1 || first.MigrationIdentifiers[0] != "stored-timestamp-microseconds-v1" {
		t.Fatal("migration identity missing")
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"private-row-content", "another-secret", path, "CREATE TABLE"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("report disclosed %q", private)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("snapshot changed")
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(path + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("inspection created a sidecar: %s %v", suffix, err)
		}
	}
}

func TestInspectRejectsLiveSidecarsAndNonSnapshots(t *testing.T) {
	path := snapshotFixture(t, appviewFixture)
	link := filepath.Join(t.TempDir(), "linked.sqlite")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{"relative.sqlite", link, filepath.Dir(path), filepath.Join(t.TempDir(), "missing.sqlite")} {
		if _, err := Inspect(context.Background(), candidate, "appview"); !errors.Is(err, ErrSnapshot) {
			t.Fatalf("accepted non-snapshot %q: %v", candidate, err)
		}
	}
	for _, suffix := range []string{"-wal", "-journal", "-log"} {
		if err := os.WriteFile(path+suffix, []byte("live"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Inspect(context.Background(), path, "appview"); !errors.Is(err, ErrSidecar) {
			t.Fatalf("accepted sidecar: %v", err)
		}
		if err := os.Remove(path + suffix); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInspectRejectsNonportableSchemaAndForeignKeyFailure(t *testing.T) {
	path := snapshotFixture(t, appviewFixture+`CREATE TRIGGER legacy AFTER INSERT ON content_items BEGIN SELECT tsw_timestamp_us(NEW.id); END;`)
	if _, err := Inspect(context.Background(), path, "appview"); !errors.Is(err, ErrPortable) {
		t.Fatalf("private extension accepted: %v", err)
	}
	path = snapshotFixture(t, appviewFixture+`CREATE TABLE parents(id INTEGER PRIMARY KEY); CREATE TABLE children(parent_id INTEGER REFERENCES parents(id)); INSERT INTO children VALUES(99);`)
	if _, err := Inspect(context.Background(), path, "appview"); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("invalid foreign key accepted: %v", err)
	}
}

func schedulingFixture(value string, pending int) string {
	return `CREATE TABLE oauth_sessions(did TEXT PRIMARY KEY,token_json TEXT,dpop_key_json TEXT);
CREATE TABLE oauth_states(state TEXT PRIMARY KEY,pkce_verifier TEXT,dpop_key_json TEXT);
CREATE TABLE scheduled_jobs(id TEXT PRIMARY KEY);
CREATE TABLE auth_vault_cleanup(id INTEGER PRIMARY KEY,pending INTEGER NOT NULL);
INSERT INTO oauth_sessions VALUES('actor','` + value + `','` + value + `');
INSERT INTO oauth_states VALUES('state','` + value + `',NULL);
INSERT INTO auth_vault_cleanup VALUES(1,` + strconv.Itoa(pending) + `);`
}

func TestSchedulingVaultMustAlreadyBeEncryptedAndCleaned(t *testing.T) {
	sealed := "aesgcm:v1:" + base64.StdEncoding.EncodeToString(make([]byte, 28))
	for _, example := range []struct {
		value   string
		pending int
	}{{"plaintext-token-private", 0}, {sealed, 1}, {"aesgcm:v1:not-base64", 0}} {
		path := snapshotFixture(t, schedulingFixture(example.value, example.pending))
		if _, err := Inspect(context.Background(), path, "scheduling"); !errors.Is(err, ErrVault) {
			t.Fatalf("unsafe vault accepted: %v", err)
		}
	}
	path := snapshotFixture(t, schedulingFixture(sealed, 0))
	report, err := Inspect(context.Background(), path, "scheduling")
	if err != nil || len(report.Tables) != 4 {
		t.Fatalf("clean vault rejected: %+v %v", report, err)
	}
	// Choosing appview cannot bypass the same vault check in a mixed snapshot.
	path = snapshotFixture(t, appviewFixture+schedulingFixture("plaintext-token-private", 0))
	if _, err := Inspect(context.Background(), path, "appview"); !errors.Is(err, ErrVault) {
		t.Fatalf("kind bypassed vault checks: %v", err)
	}
}

func TestInspectRejectsMissingSchemaAndCorruptionWithoutRawErrors(t *testing.T) {
	path := snapshotFixture(t, "CREATE TABLE other(value TEXT)")
	if _, err := Inspect(context.Background(), path, "appview"); !errors.Is(err, ErrSchema) {
		t.Fatalf("wrong schema accepted: %v", err)
	}
	path = filepath.Join(t.TempDir(), "corrupt-private.sqlite")
	if err := os.WriteFile(path, []byte("private-token-content"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Inspect(context.Background(), path, "appview")
	if err == nil || strings.Contains(err.Error(), "private-token") || strings.Contains(err.Error(), path) {
		t.Fatalf("unsafe diagnostic: %v", err)
	}
}
