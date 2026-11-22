package drafts

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type fakeMigrationSource struct {
	objects     map[string][]byte
	walks, gets int
	beforeWalk  func(int)
}

func (s *fakeMigrationSource) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	s.gets++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	value, exists := s.objects[key]
	if !exists {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(value)), nil
}

func (s *fakeMigrationSource) Walk(ctx context.Context, visit func(migrationObject) error) error {
	s.walks++
	if s.beforeWalk != nil {
		s.beforeWalk(s.walks)
	}
	keys := make([]string, 0, len(s.objects))
	for key := range s.objects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := visit(migrationObject{key: key, size: int64(len(s.objects[key])), version: migrationDigest(s.objects[key])}); err != nil {
			return err
		}
	}
	return nil
}

type fakeMigrationDestination struct {
	objects          map[string][]byte
	creates          int
	failKey, raceKey string
	unknown          bool
}

func (d *fakeMigrationDestination) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	value, exists := d.objects[key]
	if !exists {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(value)), nil
}

func (d *fakeMigrationDestination) Create(ctx context.Context, key string, body io.ReadSeeker, size int64) error {
	d.creates++
	if key == d.failKey {
		return errors.New("provider response must not appear in receipts")
	}
	if key == d.raceKey {
		d.objects[key] = []byte("other writer")
	}
	if _, exists := d.objects[key]; exists {
		return os.ErrExist
	}
	value, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	if int64(len(value)) != size {
		return io.ErrUnexpectedEOF
	}
	d.objects[key] = value
	if d.unknown {
		return errors.New("lost successful PUT response")
	}
	return ctx.Err()
}

func migrationFixture(t *testing.T) (*fakeMigrationSource, *fakeMigrationDestination, string, string, string) {
	t.Helper()
	source := &fakeMigrationSource{objects: map[string][]byte{strings.Repeat("a", 64): []byte("ciphertext first"), strings.Repeat("b", 64): []byte("ciphertext second")}}
	destination := &fakeMigrationDestination{objects: map[string][]byte{}}
	return source, destination, migrationDigest([]byte("source")), migrationDigest([]byte("destination")), filepath.Join(t.TempDir(), "receipt.json")
}

func TestObjectMigrationCopiesCiphertextAndRechecksCompletedResume(t *testing.T) {
	source, destination, sourceID, destinationID, path := migrationFixture(t)
	destination.unknown = true
	for range 2 {
		result, err := migrateObjects(t.Context(), source, destination, sourceID, destinationID, path)
		if err != nil || !result.Complete || result.Objects != 2 || result.Bytes != 33 {
			t.Fatalf("migration result: %+v %v", result, err)
		}
	}
	if destination.creates != 2 || source.gets != 4 || source.walks != 4 {
		t.Fatal("completed receipt skipped byte verification or rewrote objects")
	}
	for key, value := range source.objects {
		if !bytes.Equal(destination.objects[key], value) {
			t.Fatal("ciphertext changed")
		}
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("receipt permissions", err)
	}
	encoded, err := os.ReadFile(path)
	if err != nil || bytes.Contains(encoded, []byte("ciphertext")) || bytes.Contains(encoded, []byte(strings.Repeat("a", 64))) || bytes.Contains(encoded, []byte("provider response")) {
		t.Fatal("receipt contains private data", err)
	}
}

func TestObjectMigrationResumesOnlyMatchingStoreAndBytes(t *testing.T) {
	source, destination, sourceID, destinationID, path := migrationFixture(t)
	destination.failKey = strings.Repeat("b", 64)
	result, err := migrateObjects(t.Context(), source, destination, sourceID, destinationID, path)
	if err == nil || result.Complete || result.Objects != 1 {
		t.Fatalf("failure receipt: %+v %v", result, err)
	}
	gets, creates := source.gets, destination.creates
	if _, err := migrateObjects(t.Context(), source, destination, sourceID, migrationDigest([]byte("another destination")), path); err == nil || source.gets != gets || destination.creates != creates {
		t.Fatal("receipt switched destination or performed network operations")
	}
	destination.failKey = ""
	if result, err := migrateObjects(t.Context(), source, destination, sourceID, destinationID, path); err != nil || !result.Complete {
		t.Fatalf("resume: %+v %v", result, err)
	}
	// Existing bytes must never be replaced to make an old receipt appear valid.
	destination.objects[strings.Repeat("a", 64)] = []byte("different")
	creates = destination.creates
	if _, err := migrateObjects(t.Context(), source, destination, sourceID, destinationID, path); err == nil || destination.creates != creates {
		t.Fatal("destination mismatch overwritten")
	}
}

func TestObjectMigrationDetectsConcurrentDestinationAndSourceChanges(t *testing.T) {
	for _, changeSource := range []bool{false, true} {
		source, destination, sourceID, destinationID, path := migrationFixture(t)
		if changeSource {
			source.beforeWalk = func(walk int) {
				if walk == 2 {
					source.objects[strings.Repeat("a", 64)] = []byte("replacement with same length")
				}
			}
		} else {
			destination.raceKey = strings.Repeat("a", 64)
		}
		result, err := migrateObjects(t.Context(), source, destination, sourceID, destinationID, path)
		if err == nil || result.Complete {
			t.Fatal("concurrent replacement accepted")
		}
		if !changeSource && string(destination.objects[destination.raceKey]) != "other writer" {
			t.Fatal("racing destination overwritten")
		}
	}
}

func TestObjectMigrationRejectsUnsafeReceiptAndObjectInputs(t *testing.T) {
	for _, unsafe := range []string{"symlink", "permissions", "lock", "invalid-key", "oversize", "canceled"} {
		t.Run(unsafe, func(t *testing.T) {
			source, destination, sourceID, destinationID, path := migrationFixture(t)
			ctx := t.Context()
			switch unsafe {
			case "symlink":
				victim := filepath.Join(filepath.Dir(path), "victim")
				if err := os.WriteFile(victim, []byte("retain"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(victim, path); err != nil {
					t.Fatal(err)
				}
			case "permissions":
				if err := os.WriteFile(path, []byte(`{}`), 0644); err != nil {
					t.Fatal(err)
				}
			case "lock":
				if err := os.WriteFile(path+".lock", nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "invalid-key":
				source.objects = map[string][]byte{"../untrusted": []byte("ciphertext")}
			case "oversize":
				source.objects[strings.Repeat("a", 64)] = nil
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if result, err := migrateObjects(ctx, source, destination, sourceID, destinationID, path); err == nil || result.Complete || destination.creates != 0 {
				t.Fatalf("unsafe migration: %+v %v", result, err)
			}
		})
	}
}

func TestMigrationPrivateBlobCreatesWithoutOverwriteAndDistinguishesMissing(t *testing.T) {
	key := strings.Repeat("d", 64)
	s := blobTestStore(t, func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			return blobTestResponse(404, `{"error":"missing"}`), nil
		}
		if r.Header.Get("X-Allow-Overwrite") != "0" {
			t.Fatal("migration permits overwrite")
		}
		return blobTestResponse(200, `{"pathname":"`+key+`","url":"https://teststore.private.blob.vercel-storage.com/`+key+`"}`), nil
	})
	destination := migrationBlobDestination{s}
	if _, err := destination.Get(t.Context(), key); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing destination not distinguished", err)
	}
	if err := destination.Create(t.Context(), key, bytes.NewReader([]byte{1}), 1); err != nil {
		t.Fatal(err)
	}
}
