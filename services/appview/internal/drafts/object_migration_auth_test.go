package drafts

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
)

func migrationDelegationFixture(t *testing.T, mutate func(map[string]any)) config.Env {
	t.Helper()
	until := time.Now().Add(time.Hour).UnixMilli()
	payload := map[string]any{"storeId": "store_teststore", "pathname": "*", "operations": []string{"get", "put"}, "validUntil": until, "maximumSizeInBytes": 106_000_000}
	if mutate != nil {
		mutate(payload)
	}
	encoded, _ := json.Marshal(payload)
	signature := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	issued, _ := json.Marshal(map[string]any{"delegationToken": base64.RawURLEncoding.EncodeToString(encoded) + "." + signature, "clientSigningToken": signature, "validUntil": payload["validUntil"]})
	path := filepath.Join(t.TempDir(), "migration-auth.json")
	if err := os.WriteFile(path, issued, 0600); err != nil {
		t.Fatal(err)
	}
	return config.Env{"BLOB_MIGRATION_STORE_ID": "store_teststore", "BLOB_MIGRATION_SIGNED_TOKEN_FILE": path}
}

func TestMigrationDelegationUsesOfficialPresignCanonicalContract(t *testing.T) {
	env := migrationDelegationFixture(t, nil)
	s, err := migrationBlobStoreFromEnvironment(env)
	if err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("a", 64)
	var gets, puts int
	s.client.Transport = blobTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" || r.URL.Query().Get("vercel-blob-delegation") != s.delegation.delegation {
			t.Fatal("delegation was treated as a bearer credential")
		}
		switch r.Method {
		case "GET":
			gets++
			if r.URL.Host != "teststore.private.blob.vercel-storage.com" || r.URL.Path != "/"+key || r.URL.Query().Get("cache") != "0" || r.URL.Query().Get("vercel-blob-signature") != "PsRwo-J8Ig_o5QcKAHdt3E2w7zeS71X5tcuhnTQosGg" {
				t.Fatal("GET signature differs from the official SDK canonical vector")
			}
			return blobTestResponse(200, "ciphertext"), nil
		case "PUT":
			puts++
			q := r.URL.Query()
			if r.URL.Host != "vercel.com" || q.Get("pathname") != key || q.Get("vercel-blob-add-random-suffix") != "false" || q.Get("vercel-blob-allow-overwrite") != "false" || q.Get("vercel-blob-cache-control-max-age") != "60" || q.Get("vercel-blob-signature") != "p2lfI6cuCkzfICHSq8GYPNnWDdVfp6r0nPWia3igMKs" || r.Header.Get("X-Allow-Overwrite") != "0" {
				t.Fatal("PUT signature changed immutable migration constraints")
			}
			return blobTestResponse(200, `{"url":"https://teststore.private.blob.vercel-storage.com/`+key+`","pathname":"`+key+`"}`), nil
		default:
			t.Fatal("delegation allowed an unapproved operation")
			return nil, errUnavailable
		}
	})
	d := migrationBlobDestination{s}
	for range 2 {
		body, err := d.Get(t.Context(), key)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(body)
		body.Close()
		if err != nil || string(data) != "ciphertext" {
			t.Fatal("delegated ciphertext read failed")
		}
		if err := d.Create(t.Context(), key, bytes.NewReader(data), int64(len(data))); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Put(t.Context(), key, bytes.NewReader([]byte{1}), 1); err == nil {
		t.Fatal("delegated credentials permitted overwrite")
	}
	if err := s.Delete(t.Context(), key); err == nil {
		t.Fatal("delegated credentials permitted deletion")
	}
	if gets != 2 || puts != 2 {
		t.Fatal("invalid delegation use reached the network")
	}
}

func TestMigrationDelegationRejectsWrongOrOverbroadScope(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"store":          func(p map[string]any) { p["storeId"] = "store_elsewhere" },
		"path":           func(p map[string]any) { p["pathname"] = "specific-key" },
		"delete":         func(p map[string]any) { p["operations"] = []string{"get", "delete"} },
		"duplicate":      func(p map[string]any) { p["operations"] = []string{"get", "get"} },
		"expiry":         func(p map[string]any) { p["validUntil"] = time.Now().Add(-time.Second).UnixMilli() },
		"duration":       func(p map[string]any) { p["validUntil"] = time.Now().Add(2 * time.Hour).UnixMilli() },
		"unbounded-size": func(p map[string]any) { delete(p, "maximumSizeInBytes") },
		"overbroad-size": func(p map[string]any) { p["maximumSizeInBytes"] = 106_000_001 },
		"type":           func(p map[string]any) { p["allowedContentTypes"] = []string{"image/*"} },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := migrationBlobStoreFromEnvironment(migrationDelegationFixture(t, mutate)); err == nil {
				t.Fatal("unapproved delegation accepted")
			}
		})
	}
	for _, perm := range []os.FileMode{0644, 0660} {
		env := migrationDelegationFixture(t, nil)
		if err := os.Chmod(env["BLOB_MIGRATION_SIGNED_TOKEN_FILE"], perm); err != nil {
			t.Fatal(err)
		}
		if _, err := migrationBlobStoreFromEnvironment(env); err == nil {
			t.Fatal("readable credential file accepted")
		}
	}
	env := migrationDelegationFixture(t, nil)
	link := env["BLOB_MIGRATION_SIGNED_TOKEN_FILE"] + ".link"
	if err := os.Symlink(env["BLOB_MIGRATION_SIGNED_TOKEN_FILE"], link); err != nil {
		t.Fatal(err)
	}
	env["BLOB_MIGRATION_SIGNED_TOKEN_FILE"] = link
	if _, err := migrationBlobStoreFromEnvironment(env); err == nil {
		t.Fatal("symlink credential accepted")
	}
}

func TestMigrationDelegationExpiresBeforeNetworkAndIsMigrationOnly(t *testing.T) {
	env := migrationDelegationFixture(t, nil)
	s, err := migrationBlobStoreFromEnvironment(env)
	if err != nil {
		t.Fatal(err)
	}
	s.delegation.validUntil = time.Now().Add(-time.Second).UnixMilli()
	s.client.Transport = blobTestTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("expired delegation reached network")
		return nil, errUnavailable
	})
	if _, err := s.Get(t.Context(), strings.Repeat("a", 64)); err == nil {
		t.Fatal("expired token accepted")
	}
	env["SOCIAL_DRAFT_STORAGE_BACKEND"] = "vercel-blob"
	if _, err := objectStoreFromEnvironment(env); err == nil {
		t.Fatal("runtime accepted migration-only delegated credentials")
	}
	env["BLOB_READ_WRITE_TOKEN"] = "vercel_blob_rw_teststore_testsecret"
	if _, err := migrationBlobStoreFromEnvironment(env); err == nil {
		t.Fatal("ambiguous credentials accepted")
	}
}
