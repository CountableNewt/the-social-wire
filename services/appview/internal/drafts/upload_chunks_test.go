package drafts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

type unknownChunkObjects struct {
	testObjects
	fail bool
}

func (o *unknownChunkObjects) Put(ctx context.Context, key string, body io.ReadSeeker, size int64) error {
	err := o.testObjects.Put(ctx, key, body, size)
	if o.fail {
		o.fail = false
		return errors.New("unknown outcome")
	}
	return err
}
func chunkFixture(t *testing.T) (*Service, context.Context, string, asset, []byte, *unknownChunkObjects) {
	t.Helper()
	db, err := sqlitedb.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	s, err := newService(db, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	objects := &unknownChunkObjects{testObjects: testObjects{}}
	s.objects = objects
	body := append([]byte("GIF89a"), bytes.Repeat([]byte{42}, int(uploadChunkBytes)+13)...)
	digest := sha256.Sum256(body)
	value := asset{ID: "00000000-0000-0000-0000-000000000012", Name: "chunk.gif", ContentType: "image/gif", Kind: "gif", ByteLength: int64(len(body)), SHA256: hex.EncodeToString(digest[:])}
	owner := "did:plc:chunks"
	if _, err := s.reserve(t.Context(), owner, value); err != nil {
		t.Fatal(err)
	}
	ctx, release, err := s.acquire(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	return s, ctx, owner, value, body, objects
}
func TestChunkUnknownPutRetryAndCompletion(t *testing.T) {
	s, ctx, owner, value, body, objects := chunkFixture(t)
	first := body[:uploadChunkBytes]
	objects.fail = true
	if err := s.uploadChunk(ctx, owner, value.ID, 0, bytes.NewReader(first), int64(len(first))); !errors.Is(err, errUnavailable) {
		t.Fatalf("unknown PUT = %v", err)
	}
	before, err := s.readChunk(ctx, s.db.Reader(), owner, value.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext := append([]byte{}, objects.testObjects[before.metadata.Key]...)
	if bytes.Contains(ciphertext, first[:100]) {
		t.Fatal("temporary chunk stored plaintext")
	}
	if err := s.uploadChunk(ctx, owner, value.ID, 0, bytes.NewReader(first), int64(len(first))); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ciphertext, objects.testObjects[before.metadata.Key]) || len(objects.testObjects) != 1 {
		t.Fatal("retry changed durable object")
	}
	changed := append([]byte{}, first...)
	changed[100]++
	if err := s.uploadChunk(ctx, owner, value.ID, 0, bytes.NewReader(changed), int64(len(changed))); !errors.Is(err, errConflict) {
		t.Fatalf("different retry = %v", err)
	}
	if _, err := s.completeUpload(ctx, owner, value.ID); !errors.Is(err, errPending) {
		t.Fatalf("missing part = %v", err)
	}
	last := body[uploadChunkBytes:]
	if err := s.uploadChunk(ctx, owner, value.ID, 1, bytes.NewReader(last), int64(len(last))); err != nil {
		t.Fatal(err)
	}
	result, err := s.completeUpload(ctx, owner, value.ID)
	if err != nil || result.State != "ready" {
		t.Fatalf("complete = %#v %v", result, err)
	}
	file, _, err := s.download(ctx, owner, value.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer discard(file)
	actual, err := io.ReadAll(file)
	if err != nil || !bytes.Equal(actual, body) {
		t.Fatal("roundtrip mismatch", err)
	}
	if err := s.cleanupChunk(ctx, owner, value.ID, 0); err != nil {
		t.Fatal(err)
	}
	if _, exists := objects.testObjects[before.metadata.Key]; exists {
		t.Fatal("retired temporary chunk retained")
	}
	if err := s.uploadChunk(ctx, owner, value.ID, 0, bytes.NewReader(first), int64(len(first))); err != nil {
		t.Fatal("ready retry", err)
	}
}
func TestChunkAuthenticationAndStaleOwner(t *testing.T) {
	s, ctx, owner, value, body, objects := chunkFixture(t)
	for part := 0; part < 2; part++ {
		start := int64(part) * uploadChunkBytes
		end := min(start+uploadChunkBytes, int64(len(body)))
		if err := s.uploadChunk(ctx, owner, value.ID, part, bytes.NewReader(body[start:end]), end-start); err != nil {
			t.Fatal(err)
		}
	}
	chunk, err := s.readChunk(ctx, s.db.Reader(), owner, value.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	objects.testObjects[chunk.metadata.Key][20] ^= 1
	if _, err := s.completeUpload(ctx, owner, value.ID); err == nil {
		t.Fatal("corrupt encrypted part accepted")
	}
	if _, err := s.db.Writer().ExecContext(ctx, "UPDATE private_social_draft_owner_leases SET token='successor' WHERE owner=?", owner); err != nil {
		t.Fatal(err)
	}
	if err := s.cleanupChunk(ctx, owner, value.ID, 0); !errors.Is(err, errUnavailable) {
		t.Fatalf("stale cleanup owner = %v", err)
	}
	if err := s.uploadChunk(ctx, owner, value.ID, 0, bytes.NewReader(body[:uploadChunkBytes]), uploadChunkBytes); !errors.Is(err, errUnavailable) {
		t.Fatalf("stale upload owner = %v", err)
	}
}
func TestChunkPartBoundaries(t *testing.T) {
	for _, tc := range []struct {
		length int64
		part   int
	}{{0, 0}, {maxFileBytes + 1, 0}, {1, -1}, {1, 1}, {maxFileBytes, 34}} {
		if _, err := expectedChunkLength(tc.length, tc.part); err == nil {
			t.Fatalf("accepted %#v", tc)
		}
	}
	if got, err := expectedChunkLength(maxFileBytes, 33); err != nil || got != maxFileBytes-33*uploadChunkBytes {
		t.Fatalf("final part = %d %v", got, err)
	}
}

func TestChunkWholeFileDigestAndMetadataAuthentication(t *testing.T) {
	s, ctx, owner, value, body, _ := chunkFixture(t)
	changed := append([]byte{}, body...)
	changed[len(changed)-1] ^= 1
	for part := 0; part < 2; part++ {
		start := int64(part) * uploadChunkBytes
		end := min(start+uploadChunkBytes, int64(len(changed)))
		if err := s.uploadChunk(ctx, owner, value.ID, part, bytes.NewReader(changed[start:end]), end-start); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.completeUpload(ctx, owner, value.ID); !errors.Is(err, errInvalid) {
		t.Fatalf("whole SHA mismatch = %v", err)
	}
	current, _, err := s.readAsset(ctx, s.db.Reader(), owner, value.ID)
	if err != nil || current.State != "reserved" {
		t.Fatalf("digest failure committed asset: %#v %v", current, err)
	}
	var metadata []byte
	if err := s.db.Reader().QueryRowContext(ctx, "SELECT metadata FROM private_social_draft_upload_chunks WHERE owner=? AND asset_id=? AND part=0", owner, value.ID).Scan(&metadata); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(metadata, []byte(value.Name)) || bytes.Contains(metadata, []byte(value.SHA256)) {
		t.Fatal("chunk metadata plaintext")
	}
	metadata[len(metadata)-1] ^= 1
	if _, err := s.db.Writer().ExecContext(ctx, "UPDATE private_social_draft_upload_chunks SET metadata=? WHERE owner=? AND asset_id=? AND part=0", metadata, owner, value.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.readChunk(ctx, s.db.Reader(), owner, value.ID, 0); !errors.Is(err, errUnavailable) {
		t.Fatalf("corrupt metadata = %v", err)
	}
}
