package drafts

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
)

const uploadChunkBytes int64 = 3 * 1024 * 1024

type uploadChunkMetadata struct {
	PlainSHA  string `json:"plainSha"`
	CipherSHA string `json:"cipherSha"`
	Nonce     []byte `json:"nonce"`
	Key       string `json:"key"`
	Length    int64  `json:"length"`
}
type storedUploadChunk struct {
	part     int
	metadata uploadChunkMetadata
	state    string
	expires  float64
}

func expectedChunkLength(length int64, part int) (int64, error) {
	if length <= 0 || length > maxFileBytes || part < 0 || part >= 34 || int64(part)*uploadChunkBytes >= length {
		return 0, errInvalid
	}
	return min(uploadChunkBytes, length-int64(part)*uploadChunkBytes), nil
}

func (s *Service) readChunk(ctx context.Context, db assetQuery, owner, id string, part int) (storedUploadChunk, error) {
	result := storedUploadChunk{part: part}
	var encoded []byte
	var key string
	err := db.QueryRowContext(ctx, "SELECT metadata,object_key,state,expires_at FROM private_social_draft_upload_chunks WHERE owner=? AND asset_id=? AND part=?", owner, id, part).Scan(&encoded, &key, &result.state, &result.expires)
	if err != nil {
		return result, err
	}
	plain, err := unseal(s.cipher, encoded, aad(owner, id, "upload-chunk-metadata", uint64(part)))
	if err != nil || json.Unmarshal(plain, &result.metadata) != nil || result.metadata.Key != key || !assetDigest.MatchString(result.metadata.PlainSHA) || !assetDigest.MatchString(result.metadata.CipherSHA) || len(result.metadata.Nonce) != s.cipher.NonceSize() || result.metadata.Length <= 0 || result.metadata.Length > uploadChunkBytes {
		return result, errUnavailable
	}
	return result, nil
}

func (s *Service) uploadChunk(ctx context.Context, owner, id string, part int, body io.Reader, length int64) error {
	if err := s.checkOwner(ctx, s.db.Reader(), owner); err != nil {
		return err
	}
	reserved, _, err := s.readAsset(ctx, s.db.Reader(), owner, id)
	if err != nil {
		return err
	}
	expected, err := expectedChunkLength(reserved.ByteLength, part)
	if err != nil || length != expected {
		return errInvalid
	}
	plain, err := io.ReadAll(io.LimitReader(body, expected+1))
	if err != nil || int64(len(plain)) != expected {
		return errInvalid
	}
	digest := sha256.Sum256(plain)
	plainSHA := hex.EncodeToString(digest[:])
	var chunk storedUploadChunk
	alreadyReady := false
	err = s.db.Write(ctx, func(tx *sql.Tx) error {
		if err := s.checkOwner(ctx, tx, owner); err != nil {
			return err
		}
		current, _, err := s.readAsset(ctx, tx, owner, id)
		if err != nil {
			return err
		}
		chunk, err = s.readChunk(ctx, tx, owner, id, part)
		if err == nil {
			if chunk.metadata.PlainSHA != plainSHA || chunk.metadata.Length != expected {
				return errConflict
			}
			if current.State == "ready" {
				alreadyReady = true
				return nil
			}
			if current.State != "reserved" || chunk.state == "retired" || chunk.state == "purged" || chunk.expires <= nowSeconds() {
				return errRetired
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if current.State != "reserved" {
			return errConflict
		}
		var created float64
		if err := tx.QueryRowContext(ctx, "SELECT created_at FROM private_social_draft_assets WHERE owner=? AND asset_id=?", owner, id).Scan(&created); err != nil {
			return err
		}
		if created+86400 <= nowSeconds() {
			return errRetired
		}
		nonce := make([]byte, s.cipher.NonceSize())
		var keyBytes [32]byte
		if _, err := rand.Read(nonce); err != nil {
			return err
		}
		if _, err := rand.Read(keyBytes[:]); err != nil {
			return err
		}
		key := hex.EncodeToString(keyBytes[:])
		ciphertext := s.cipher.Seal(append([]byte{}, nonce...), nonce, plain, aad(owner, id, "upload-chunk", uint64(part)))
		cipherDigest := sha256.Sum256(ciphertext)
		chunk = storedUploadChunk{part: part, state: "reserved", expires: created + 86400, metadata: uploadChunkMetadata{PlainSHA: plainSHA, CipherSHA: hex.EncodeToString(cipherDigest[:]), Nonce: nonce, Key: key, Length: expected}}
		metadata, err := seal(s.cipher, encode(chunk.metadata), aad(owner, id, "upload-chunk-metadata", uint64(part)))
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO private_social_draft_upload_chunks(owner,asset_id,part,metadata,object_key,state,expires_at) VALUES(?,?,?,?,?,'reserved',?)`, owner, id, part, metadata, key, chunk.expires)
		return err
	})
	if err != nil {
		return err
	}
	if alreadyReady || reserved.State == "ready" || chunk.state == "ready" || chunk.state == "purged" {
		return nil
	}
	if s.objects == nil {
		return errUnavailable
	}
	// Reusing this nonce is allowed only after the durable immutable plaintext
	// hash comparison above. Identical retries produce identical ciphertext.
	ciphertext := s.cipher.Seal(append([]byte{}, chunk.metadata.Nonce...), chunk.metadata.Nonce, plain, aad(owner, id, "upload-chunk", uint64(part)))
	digest = sha256.Sum256(ciphertext)
	if hex.EncodeToString(digest[:]) != chunk.metadata.CipherSHA {
		return errUnavailable
	}
	if err := s.checkOwner(ctx, s.db.Reader(), owner); err != nil {
		return err
	}
	if err := s.objects.Put(ctx, chunk.metadata.Key, bytes.NewReader(ciphertext), int64(len(ciphertext))); err != nil {
		return errUnavailable
	}
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		if err := s.checkOwner(ctx, tx, owner); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, "UPDATE private_social_draft_upload_chunks SET state='ready' WHERE owner=? AND asset_id=? AND part=? AND object_key=? AND state='reserved' AND expires_at>?", owner, id, part, chunk.metadata.Key, nowSeconds())
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err == nil && n != 1 {
			return errConflict
		}
		return err
	})
}

func (s *Service) completeUpload(ctx context.Context, owner, id string) (asset, error) {
	ctx, release, err := s.ensureTransfer(ctx, owner)
	if err != nil {
		return asset{}, err
	}
	defer release()
	if err := s.checkOwner(ctx, s.db.Reader(), owner); err != nil {
		return asset{}, err
	}
	value, _, err := s.readAsset(ctx, s.db.Reader(), owner, id)
	if err != nil {
		return asset{}, err
	}
	if value.State == "ready" {
		return value, nil
	}
	if value.State != "reserved" {
		return asset{}, errRetired
	}
	count := int((value.ByteLength + uploadChunkBytes - 1) / uploadChunkBytes)
	chunks := make([]storedUploadChunk, 0, count)
	for part := 0; part < count; part++ {
		chunk, err := s.readChunk(ctx, s.db.Reader(), owner, id, part)
		if errors.Is(err, sql.ErrNoRows) {
			return asset{}, errPending
		}
		if err != nil {
			return asset{}, err
		}
		expected, _ := expectedChunkLength(value.ByteLength, part)
		if chunk.state != "ready" || chunk.expires <= nowSeconds() || chunk.metadata.Length != expected {
			return asset{}, errPending
		}
		chunks = append(chunks, chunk)
	}
	reader := &uploadChunkReader{ctx: ctx, service: s, owner: owner, id: id, chunks: chunks}
	result, err := s.upload(ctx, owner, id, reader, value.ByteLength)
	if err != nil {
		return asset{}, err
	}
	err = s.db.Write(ctx, func(tx *sql.Tx) error {
		if err := s.checkOwner(ctx, tx, owner); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE private_social_draft_upload_chunks SET state='retired',expires_at=? WHERE owner=? AND asset_id=? AND state IN ('reserved','ready')", nowSeconds(), owner, id)
		return err
	})
	return result, err
}

type uploadChunkReader struct {
	ctx       context.Context
	service   *Service
	owner, id string
	chunks    []storedUploadChunk
	index     int
	current   *bytes.Reader
}

func (r *uploadChunkReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	for {
		if r.current != nil {
			n, err := r.current.Read(buffer)
			if n > 0 {
				return n, nil
			}
			if err != io.EOF {
				return n, err
			}
			r.current = nil
		}
		if r.index == len(r.chunks) {
			return 0, io.EOF
		}
		chunk := r.chunks[r.index]
		r.index++
		if r.service.objects == nil {
			return 0, errUnavailable
		}
		body, err := r.service.objects.Get(r.ctx, chunk.metadata.Key)
		if err != nil {
			return 0, errUnavailable
		}
		expected := chunk.metadata.Length + int64(r.service.cipher.NonceSize()+r.service.cipher.Overhead())
		ciphertext, readErr := io.ReadAll(io.LimitReader(body, expected+1))
		closeErr := body.Close()
		if readErr != nil || closeErr != nil || int64(len(ciphertext)) != expected {
			return 0, errUnavailable
		}
		digest := sha256.Sum256(ciphertext)
		if hex.EncodeToString(digest[:]) != chunk.metadata.CipherSHA {
			return 0, errUnavailable
		}
		plain, err := unseal(r.service.cipher, ciphertext, aad(r.owner, r.id, "upload-chunk", uint64(chunk.part)))
		if err != nil || int64(len(plain)) != chunk.metadata.Length {
			return 0, errUnavailable
		}
		digest = sha256.Sum256(plain)
		if hex.EncodeToString(digest[:]) != chunk.metadata.PlainSHA {
			return 0, errUnavailable
		}
		r.current = bytes.NewReader(plain)
	}
}

func (s *Service) cleanupChunks(ctx context.Context, limit int) error {
	for range limit {
		var owner, id string
		var part int
		err := s.db.Reader().QueryRowContext(ctx, `SELECT owner,asset_id,part FROM private_social_draft_upload_chunks WHERE state!='purged' AND expires_at<=? ORDER BY expires_at,owner,asset_id,part LIMIT 1`, nowSeconds()).Scan(&owner, &id, &part)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		owned, release, err := s.acquire(ctx, owner)
		if err != nil {
			return err
		}
		err = s.cleanupChunk(owned, owner, id, part)
		release()
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) cleanupChunk(ctx context.Context, owner, id string, part int) error {
	var key string
	err := s.db.Write(ctx, func(tx *sql.Tx) error {
		if err := s.checkOwner(ctx, tx, owner); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, "UPDATE private_social_draft_upload_chunks SET state='retired' WHERE owner=? AND asset_id=? AND part=? AND state!='purged' AND expires_at<=? RETURNING object_key", owner, id, part, nowSeconds()).Scan(&key)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if s.objects == nil {
		return errUnavailable
	}
	if err := s.checkOwner(ctx, s.db.Reader(), owner); err != nil {
		return err
	}
	if err := s.objects.Delete(ctx, key); err != nil {
		retryErr := s.db.Write(ctx, func(tx *sql.Tx) error {
			if err := s.checkOwner(ctx, tx, owner); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, "UPDATE private_social_draft_upload_chunks SET expires_at=?,attempts=min(attempts+1,30) WHERE owner=? AND asset_id=? AND part=? AND state='retired'", nowSeconds()+60, owner, id, part)
			return err
		})
		return errors.Join(errUnavailable, retryErr)
	}
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		if err := s.checkOwner(ctx, tx, owner); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE private_social_draft_upload_chunks SET state='purged' WHERE owner=? AND asset_id=? AND part=? AND state='retired'", owner, id, part)
		return err
	})
}
