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

type testObjects map[string][]byte

func (store testObjects) Put(_ context.Context, key string, body io.ReadSeeker, _ int64) error {
	data, err := io.ReadAll(body)
	if err == nil {
		store[key] = data
	}
	return err
}
func (store testObjects) Get(_ context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(store[key])), nil
}
func (store testObjects) Delete(_ context.Context, key string) error {
	delete(store, key)
	return nil
}

func TestPrivateAttachmentLifecycle(t *testing.T) {
	db, err := sqlitedb.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := t.Context()
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	service, err := newService(db, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	objects := testObjects{}
	service.objects = objects
	image := []byte("GIF87afixture")
	digest := sha256.Sum256(image)
	owner, id := "did:plc:viewer", "00000000-0000-0000-0000-000000000001"
	attachment := asset{ID: id, Name: "fixture.gif", ContentType: "image/gif", SHA256: hex.EncodeToString(digest[:]), Kind: "gif", ByteLength: int64(len(image))}
	input := manifestInput{ID: "aaaaaaaaaaaaa", Refs: []manifestRef{{AssetID: id, Role: "gif", Ref: "fixture"}}}
	if result, err := service.reserve(ctx, owner, attachment); err != nil || result.State != "reserved" {
		t.Fatalf("reserve = %#v, %v", result, err)
	}
	if _, err := service.putManifest(ctx, owner, input, false); !errors.Is(err, errPending) {
		t.Fatalf("binding while reserved returned %v", err)
	}
	if result, err := service.upload(ctx, owner, id, bytes.NewReader(image), int64(len(image))); err != nil || result.State != "ready" {
		t.Fatalf("upload = %#v, %v", result, err)
	}
	for _, raw := range objects {
		if bytes.Contains(raw, image) || !bytes.HasPrefix(raw, fileMagic) {
			t.Fatal("object store received plaintext or incompatible framing")
		}
	}
	saved, err := service.putManifest(ctx, owner, input, false)
	if err != nil || saved.Revision != 1 || len(saved.Attachments) != 1 {
		t.Fatalf("manifest = %#v, %v", saved, err)
	}
	file, _, err := service.download(ctx, owner, id)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := io.ReadAll(file)
	discard(file)
	if err != nil || !bytes.Equal(actual, image) {
		t.Fatalf("download = %q, %v", actual, err)
	}
	input.Expected = 1
	input.Refs = nil
	if _, err := service.putManifest(ctx, owner, input, true); err != nil {
		t.Fatal(err)
	}
	if err := service.cleanup(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if len(objects) != 0 {
		t.Fatal("retired object was retained")
	}
	if _, err := service.reserve(ctx, owner, attachment); !errors.Is(err, errRetired) {
		t.Fatalf("retired identifier could be reused: %v", err)
	}
}
