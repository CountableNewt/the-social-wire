package drafts

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

const maxFileBytes = 100 << 20

var assetDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)
var errRetired = errors.New("draft identifier retired")

func (a asset) validReservation() bool {
	if !uuid.MatchString(a.ID) || a.Name == "" || len(a.Name) > 1024 || strings.ContainsRune(a.Name, 0) ||
		a.ByteLength <= 0 || a.ByteLength > maxFileBytes || !assetDigest.MatchString(a.SHA256) {
		return false
	}
	switch a.Kind {
	case "photo", "poster", "gif", "video", "caption":
	default:
		return false
	}
	switch a.ContentType {
	case "image/jpeg", "image/png", "image/webp", "image/gif", "video/mp4", "video/webm", "text/vtt":
		return true
	}
	return false
}

type assetQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Service) readAsset(ctx context.Context, db assetQuery, owner, id string) (asset, string, error) {
	var metadata []byte
	var state, key string
	if err := db.QueryRowContext(ctx, `SELECT metadata,state,object_key FROM private_social_draft_assets WHERE owner=? AND asset_id=?`, owner, id).
		Scan(&metadata, &state, &key); err != nil {
		return asset{}, "", err
	}
	value, err := s.decodeAsset(owner, id, state, metadata)
	return value, key, err
}

func nowSeconds() float64 { return float64(time.Now().UnixMicro()) / 1e6 }

func (s *Service) reserve(ctx context.Context, owner string, wanted asset) (asset, error) {
	if !wanted.validReservation() {
		return asset{}, errInvalid
	}
	var result asset
	err := s.db.Write(ctx, func(tx *sql.Tx) error {
		existing, key, err := s.readAsset(ctx, tx, owner, wanted.ID)
		if err == nil {
			wanted.State = existing.State
			if existing != wanted {
				return errConflict
			}
			result = existing
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if key != "" {
			return errRetired
		}
		var used int64
		var count int
		err = tx.QueryRowContext(ctx, `SELECT coalesce(sum(byte_length),0),count(*) FROM private_social_draft_assets WHERE owner=? AND state<>'purged'`, owner).
			Scan(&used, &count)
		if err != nil {
			return err
		}
		if used+wanted.ByteLength > 1<<30 || count >= 10000 {
			return errQuota
		}
		wanted.State = "reserved"
		metadata, err := s.sealMetadata(owner, wanted.ID, encode(wanted))
		if err != nil {
			return err
		}
		token := make([]byte, 32)
		if _, err := rand.Read(token); err != nil {
			return err
		}
		key = hex.EncodeToString(token)
		now := nowSeconds()
		if _, err = tx.ExecContext(ctx, `INSERT INTO private_social_draft_accounts(owner) VALUES(?) ON CONFLICT DO NOTHING`, owner); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO private_social_draft_assets(owner,asset_id,metadata,byte_length,object_key,state,created_at)
			VALUES(?,?,?,?,?,'reserved',?)`, owner, wanted.ID, metadata, wanted.ByteLength, key, now); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO private_social_draft_cleanup(owner,asset_id,available_at,attempts)
			VALUES(?,?,?,0)`, owner, wanted.ID, now+86400); err != nil {
			return err
		}
		result = wanted
		return nil
	})
	return result, err
}

func (s *Service) upload(ctx context.Context, owner, id string, body io.Reader, length int64) (asset, error) {
	ctx, release, err := s.ensureTransfer(ctx, owner)
	if err != nil {
		return asset{}, err
	}
	defer release()
	reserved, key, err := s.readAsset(ctx, s.db.Reader(), owner, id)
	if err != nil {
		return asset{}, err
	}
	if length != reserved.ByteLength {
		return asset{}, errInvalid
	}
	plain, err := tempFile()
	if err != nil {
		return asset{}, err
	}
	defer discard(plain)
	digest := sha256.New()
	n, err := io.Copy(io.MultiWriter(plain, digest), io.LimitReader(body, length+1))
	if err != nil || n != length || hex.EncodeToString(digest.Sum(nil)) != reserved.SHA256 {
		return asset{}, errInvalid
	}
	if err := validateUploadContent(plain, reserved.ContentType); err != nil {
		return asset{}, err
	}
	if reserved.State == "ready" {
		return reserved, nil
	}
	if s.objects == nil {
		return asset{}, errUnavailable
	}
	encrypted, err := tempFile()
	if err != nil {
		return asset{}, err
	}
	defer discard(encrypted)
	if _, err := plain.Seek(0, io.SeekStart); err != nil {
		return asset{}, err
	}
	if err := s.encrypt(encrypted, plain, owner, id); err != nil {
		return asset{}, err
	}
	size, err := encrypted.Seek(0, io.SeekCurrent)
	if err != nil {
		return asset{}, err
	}
	if _, err = encrypted.Seek(0, io.SeekStart); err != nil {
		return asset{}, err
	}
	if err := s.checkOwner(ctx, s.db.Reader(), owner); err != nil {
		return asset{}, err
	}
	if err = s.objects.Put(ctx, key, encrypted, size); err != nil {
		return asset{}, errUnavailable
	}
	err = s.db.Write(ctx, func(tx *sql.Tx) error {
		if err := s.checkOwner(ctx, tx, owner); err != nil {
			return err
		}
		current, currentKey, err := s.readAsset(ctx, tx, owner, id)
		if err != nil {
			return err
		}
		if current != reserved || currentKey != key {
			return errConflict
		}
		_, err = tx.ExecContext(ctx, `UPDATE private_social_draft_assets SET state='ready' WHERE owner=? AND asset_id=?`, owner, id)
		return err
	})
	reserved.State = "ready"
	return reserved, err
}

func validateUploadContent(file *os.File, media string) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if media == "text/vtt" {
		return validateCaption(file)
	}
	buf := make([]byte, 512)
	count, err := io.ReadFull(file, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return err
	}
	buf = buf[:count]
	valid := false
	switch media {
	case "image/jpeg":
		valid = bytes.HasPrefix(buf, []byte{0xff, 0xd8, 0xff})
	case "image/png":
		valid = bytes.HasPrefix(buf, []byte("\x89PNG\r\n\x1a\n"))
	case "image/gif":
		valid = bytes.HasPrefix(buf, []byte("GIF87a")) || bytes.HasPrefix(buf, []byte("GIF89a"))
	case "image/webp":
		valid = count >= 14 && string(buf[:4]) == "RIFF" && string(buf[8:14]) == "WEBPVP"
	case "video/webm":
		valid = bytes.HasPrefix(buf, []byte{0x1a, 0x45, 0xdf, 0xa3})
	case "video/mp4":
		if count >= 12 && string(buf[4:8]) == "ftyp" {
			length := int(binary.BigEndian.Uint32(buf[:4]))
			if length >= 12 && length <= count && length%4 == 0 {
				for i := 8; i < length; i += 4 {
					if i != 12 && string(buf[i:i+3]) == "mp4" {
						valid = true
					}
				}
			}
		}
	}
	if !valid {
		return errInvalid
	}
	return nil
}

func (s *Service) download(ctx context.Context, owner, id string) (*os.File, asset, error) {
	ctx, release, err := s.ensureTransfer(ctx, owner)
	if err != nil {
		return nil, asset{}, err
	}
	defer release()
	value, key, err := s.readAsset(ctx, s.db.Reader(), owner, id)
	if err != nil {
		return nil, asset{}, err
	}
	if value.State != "ready" {
		return nil, asset{}, errPending
	}
	if s.objects == nil {
		return nil, asset{}, errUnavailable
	}
	response, err := s.objects.Get(ctx, key)
	if err != nil {
		return nil, asset{}, errUnavailable
	}
	defer response.Close()
	file, err := tempFile()
	if err != nil {
		return nil, asset{}, err
	}
	success := false
	defer func() {
		if !success {
			discard(file)
		}
	}()
	digest := sha256.New()
	frames := (value.ByteLength+65535)/65536 + 1
	maxCiphertext := value.ByteLength + 6 + frames*32
	if err := s.decrypt(io.MultiWriter(file, digest), io.LimitReader(response, maxCiphertext+1), owner, id, value.ByteLength); err != nil ||
		hex.EncodeToString(digest.Sum(nil)) != value.SHA256 {
		return nil, asset{}, errUnavailable
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, asset{}, err
	}
	success = true
	return file, value, nil
}
