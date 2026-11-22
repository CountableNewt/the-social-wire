package drafts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

var ErrObjectMigration = errors.New("encrypted object migration failed; preserve source objects and the private receipt")

const maxMigrationObjects = 100_000

type migrationObject struct {
	key     string
	size    int64
	version string
}

type migrationSource interface {
	Get(context.Context, string) (io.ReadCloser, error)
	Walk(context.Context, func(migrationObject) error) error
}

type migrationDestination interface {
	Get(context.Context, string) (io.ReadCloser, error)
	Create(context.Context, string, io.ReadSeeker, int64) error
}

type migrationBlobDestination struct{ *vercelBlobStore }

func (d migrationBlobDestination) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return d.get(ctx, key, true)
}

func (d migrationBlobDestination) Create(ctx context.Context, key string, body io.ReadSeeker, size int64) error {
	// Migration never overwrites an existing destination, even when another
	// process creates it between the existence check and this request.
	return d.put(ctx, key, body, size, false)
}

type migrationS3Source struct{ *s3Store }

func (s migrationS3Source) Walk(ctx context.Context, visit func(migrationObject) error) error {
	pages := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{Bucket: &s.bucket, MaxKeys: aws.Int32(1000)})
	for pages.HasMorePages() {
		pageCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		page, err := pages.NextPage(pageCtx)
		cancel()
		if err != nil {
			return ErrObjectMigration
		}
		for _, object := range page.Contents {
			if object.Key == nil || object.Size == nil || object.ETag == nil {
				return ErrObjectMigration
			}
			if err := visit(migrationObject{*object.Key, *object.Size, *object.ETag}); err != nil {
				return err
			}
		}
	}
	return nil
}

// ObjectMigrationResult contains only aggregate measurements, never object
// names, credentials, plaintext, encryption keys or provider response bodies.
type ObjectMigrationResult struct {
	Objects  int   `json:"objects"`
	Bytes    int64 `json:"bytes"`
	Complete bool  `json:"complete"`
}

// MigrateObjectsFromEnvironment preserves existing bytes from the S3 bucket
// to an explicitly selected private Blob store. Both writers must be stopped.
func MigrateObjectsFromEnvironment(ctx context.Context, env config.Env, receipt string) (ObjectMigrationResult, error) {
	if env["SOCIAL_DRAFT_MIGRATION_SOURCE"] != "s3" || env["SOCIAL_DRAFT_MIGRATION_DESTINATION"] != "vercel-blob" || env["SOCIAL_DRAFT_MIGRATION_WRITERS_FROZEN"] != "true" {
		return ObjectMigrationResult{}, ErrObjectMigration
	}
	sourceEnv := make(config.Env, len(env))
	for key, value := range env {
		sourceEnv[key] = value
	}
	sourceEnv["SOCIAL_DRAFT_STORAGE_BACKEND"] = "s3"
	store, err := objectStoreFromEnvironment(sourceEnv)
	if err != nil {
		return ObjectMigrationResult{}, ErrObjectMigration
	}
	source, ok := store.(*s3Store)
	if !ok {
		return ObjectMigrationResult{}, ErrObjectMigration
	}
	destination, err := migrationBlobStoreFromEnvironment(env)
	if err != nil {
		return ObjectMigrationResult{}, ErrObjectMigration
	}
	sourceURL, _ := url.Parse(env["SOCIAL_DRAFT_S3_ENDPOINT"])
	destinationURL, _ := url.Parse(destination.privateOrigin)
	if strings.EqualFold(strings.TrimSuffix(sourceURL.Hostname(), "."), destinationURL.Hostname()) || strings.EqualFold(sourceURL.Hostname(), "vercel.com") {
		return ObjectMigrationResult{}, ErrObjectMigration
	}
	sourceID := migrationDigest(encode([]string{"s3", env["SOCIAL_DRAFT_S3_ENDPOINT"], env["SOCIAL_DRAFT_S3_BUCKET"]}))
	destinationID := migrationDigest(encode([]string{"private-vercel-blob", destination.privateOrigin}))
	return migrateObjects(ctx, migrationS3Source{source}, migrationBlobDestination{destination}, sourceID, destinationID, receipt)
}

func migrateObjects(ctx context.Context, source migrationSource, destination migrationDestination, sourceID, destinationID, path string) (result ObjectMigrationResult, err error) {
	if !assetDigest.MatchString(sourceID) || !assetDigest.MatchString(destinationID) || sourceID == destinationID {
		return result, ErrObjectMigration
	}
	receipt, release, err := openMigrationReceipt(path, sourceID, destinationID)
	if err != nil {
		return result, ErrObjectMigration
	}
	defer release()
	// A previous completed run is rechecked in full, never treated as a shortcut.
	receipt.Complete = false
	if err := writeMigrationReceipt(path, receipt); err != nil {
		return result, ErrObjectMigration
	}
	inventory := make(map[string]string)
	err = source.Walk(ctx, func(object migrationObject) error {
		if !validObjectKey(object.key) || object.size <= 0 || object.size > maxStoredObjectBytes || len(inventory) >= maxMigrationObjects {
			return ErrObjectMigration
		}
		keyID := migrationDigest([]byte(object.key))
		if _, duplicate := inventory[keyID]; duplicate {
			return ErrObjectMigration
		}
		inventory[keyID] = migrationDigest(encode([]any{object.key, object.size, object.version}))
		work, cancel := context.WithTimeout(ctx, 4*time.Minute)
		defer cancel()
		entry, err := migrateObject(work, source, destination, object, receipt.Objects[keyID])
		if err != nil {
			return ErrObjectMigration
		}
		receipt.Objects[keyID] = entry
		if err := writeMigrationReceipt(path, receipt); err != nil {
			return ErrObjectMigration
		}
		result.Objects++
		result.Bytes += entry.Bytes
		return nil
	})
	if err != nil {
		return result, ErrObjectMigration
	}
	// List again after copying so object additions, deletions, replacements and
	// unexpected paginator duplicates cannot produce a completed receipt.
	seen := make(map[string]bool, len(inventory))
	err = source.Walk(ctx, func(object migrationObject) error {
		keyID := migrationDigest([]byte(object.key))
		if seen[keyID] || inventory[keyID] != migrationDigest(encode([]any{object.key, object.size, object.version})) {
			return ErrObjectMigration
		}
		seen[keyID] = true
		return nil
	})
	if err != nil || len(seen) != len(inventory) || len(receipt.Objects) != len(inventory) {
		return result, ErrObjectMigration
	}
	receipt.Complete = true
	if err := writeMigrationReceipt(path, receipt); err != nil {
		return result, ErrObjectMigration
	}
	result.Complete = true
	return result, nil
}

func migrateObject(ctx context.Context, source migrationSource, destination migrationDestination, object migrationObject, prior migrationReceiptObject) (entry migrationReceiptObject, err error) {
	file, err := os.CreateTemp("", "social-draft-ciphertext-*")
	if err != nil {
		return entry, ErrObjectMigration
	}
	defer discard(file)
	body, err := source.Get(ctx, object.key)
	if err != nil {
		return entry, ErrObjectMigration
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(body, object.size+1))
	if err := errors.Join(copyErr, body.Close(), ctx.Err()); err != nil || n != object.size {
		return entry, ErrObjectMigration
	}
	entry = migrationReceiptObject{Bytes: n, SHA256: hex.EncodeToString(hash.Sum(nil))}
	if prior != (migrationReceiptObject{}) && prior != entry {
		return entry, ErrObjectMigration
	}
	if existing, err := migrationTargetDigest(ctx, destination, object.key); err == nil {
		if existing != entry {
			return entry, ErrObjectMigration
		}
		return entry, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return entry, ErrObjectMigration
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return entry, ErrObjectMigration
	}
	_ = destination.Create(ctx, object.key, file, object.size)
	// Inspect after both success and unknown outcomes; matching stored bytes are
	// sufficient evidence, while mismatches never trigger an overwrite.
	stored, err := migrationTargetDigest(ctx, destination, object.key)
	if err != nil || stored != entry {
		return entry, ErrObjectMigration
	}
	return entry, nil
}

func migrationTargetDigest(ctx context.Context, destination migrationDestination, key string) (migrationReceiptObject, error) {
	body, err := destination.Get(ctx, key)
	if err != nil {
		return migrationReceiptObject{}, err
	}
	hash := sha256.New()
	n, readErr := io.Copy(hash, io.LimitReader(body, maxStoredObjectBytes+1))
	if err := errors.Join(readErr, body.Close(), ctx.Err()); err != nil || n > maxStoredObjectBytes {
		return migrationReceiptObject{}, ErrObjectMigration
	}
	return migrationReceiptObject{Bytes: n, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func migrationDigest(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}
