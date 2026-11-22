package drafts

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

type migrationReceiptObject struct {
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type migrationReceipt struct {
	Version           int                               `json:"version"`
	SourceSHA256      string                            `json:"sourceSha256"`
	DestinationSHA256 string                            `json:"destinationSha256"`
	Complete          bool                              `json:"complete"`
	Objects           map[string]migrationReceiptObject `json:"objects"`
}

func openMigrationReceipt(path, source, destination string) (*migrationReceipt, func(), error) {
	if !filepath.IsAbs(path) {
		return nil, nil, ErrObjectMigration
	}
	lock, err := os.OpenFile(path+".lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, nil, ErrObjectMigration
	}
	if err := lock.Close(); err != nil {
		os.Remove(path + ".lock")
		return nil, nil, ErrObjectMigration
	}
	release := func() { _ = os.Remove(path + ".lock") }
	fail := func() (*migrationReceipt, func(), error) { release(); return nil, nil, ErrObjectMigration }
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		receipt := &migrationReceipt{Version: 1, SourceSHA256: source, DestinationSHA256: destination, Objects: map[string]migrationReceiptObject{}}
		if err := writeMigrationReceipt(path, receipt); err != nil {
			return fail()
		}
		return receipt, release, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 64*1024*1024 {
		return fail()
	}
	file, err := os.Open(path)
	if err != nil {
		return fail()
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		file.Close()
		return fail()
	}
	var receipt migrationReceipt
	decoder := json.NewDecoder(io.LimitReader(file, 64*1024*1024+1))
	decoder.DisallowUnknownFields()
	decodeErr := decoder.Decode(&receipt)
	var trailing any
	endErr := decoder.Decode(&trailing)
	if errors.Join(decodeErr, file.Close()) != nil || endErr != io.EOF || receipt.Version != 1 || receipt.SourceSHA256 != source || receipt.DestinationSHA256 != destination || receipt.Objects == nil || len(receipt.Objects) > maxMigrationObjects {
		return fail()
	}
	for key, value := range receipt.Objects {
		if !assetDigest.MatchString(key) || !assetDigest.MatchString(value.SHA256) || value.Bytes <= 0 || value.Bytes > maxStoredObjectBytes {
			return fail()
		}
	}
	return &receipt, release, nil
}

func writeMigrationReceipt(path string, receipt *migrationReceipt) error {
	prior, err := os.Lstat(path)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return ErrObjectMigration
	}
	if exists && (!prior.Mode().IsRegular() || prior.Mode().Perm() != 0600) {
		return ErrObjectMigration
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".draft-migration-receipt-*")
	if err != nil {
		return ErrObjectMigration
	}
	defer os.Remove(file.Name())
	encodeErr := json.NewEncoder(file).Encode(receipt)
	syncErr := file.Sync()
	if errors.Join(encodeErr, syncErr, file.Close()) != nil {
		return ErrObjectMigration
	}
	if exists {
		current, err := os.Lstat(path)
		if err != nil || !os.SameFile(prior, current) || !current.Mode().IsRegular() {
			return ErrObjectMigration
		}
		if err := os.Rename(file.Name(), path); err != nil {
			return ErrObjectMigration
		}
	} else {
		// Publish the first complete JSON file without replacing another writer.
		if err := os.Link(file.Name(), path); err != nil {
			return ErrObjectMigration
		}
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return ErrObjectMigration
	}
	return errors.Join(directory.Sync(), directory.Close())
}
