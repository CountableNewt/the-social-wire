package drafts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

func (s *Service) article(ctx context.Context, tx *sql.Tx, owner, id string) (articleDocument, []byte, error) {
	result := articleDocument{ID: id, Attachments: []binding{}}
	var payload, fingerprint []byte
	var updated float64
	err := tx.QueryRowContext(ctx, `SELECT revision,deleted,mutation_id,updated_at,payload,mutation_hash
		FROM private_article_drafts WHERE owner=? AND draft_id=?`, owner, id).
		Scan(&result.Revision, &result.Deleted, &result.MutationID, &updated, &payload, &fingerprint)
	if err != nil {
		return result, nil, err
	}
	result.Updated = time.Unix(int64(updated), 0).UTC().Format(time.RFC3339)
	if result.Deleted {
		return result, fingerprint, nil
	}
	result.Draft, err = s.openMetadata(owner, "article-draft:"+id, payload)
	if err != nil || !json.Valid(result.Draft) {
		return result, nil, errUnavailable
	}
	rows, err := tx.QueryContext(ctx, `SELECT b.local_asset_id,a.asset_id,a.metadata,a.state FROM private_article_draft_bindings b
		JOIN private_social_draft_assets a ON a.owner=b.owner AND a.asset_id=b.asset_id
		WHERE b.owner=? AND b.draft_id=? ORDER BY b.local_asset_id`, owner, id)
	if err != nil {
		return result, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var value binding
		var assetID, state string
		var metadata []byte
		if err := rows.Scan(&value.LocalID, &assetID, &metadata, &state); err != nil {
			return result, nil, err
		}
		value.Asset, err = s.decodeAsset(owner, assetID, state, metadata)
		if err != nil {
			return result, nil, err
		}
		result.Attachments = append(result.Attachments, value)
	}
	return result, fingerprint, rows.Err()
}

func (s *Service) decodeAsset(owner, id, state string, metadata []byte) (asset, error) {
	var value asset
	if state == "deleted" || state == "purged" {
		return value, sql.ErrNoRows
	}
	bytes, err := s.openMetadata(owner, id, metadata)
	if err != nil || json.Unmarshal(bytes, &value) != nil {
		return value, errUnavailable
	}
	value.State = state
	return value, nil
}

func (s *Service) listArticles(ctx context.Context, owner, cursor string, limit int) (map[string]any, error) {
	page := map[string]any{}
	err := s.db.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT draft_id FROM private_article_drafts WHERE owner=? AND draft_id>? ORDER BY draft_id LIMIT ?`, owner, cursor, limit+1)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(ids) > limit {
			page["cursor"] = ids[limit-1]
			ids = ids[:limit]
		}
		documents := make([]articleDocument, 0, len(ids))
		for _, id := range ids {
			doc, _, err := s.article(ctx, tx, owner, id)
			if err != nil {
				return err
			}
			documents = append(documents, doc)
		}
		page["drafts"] = documents
		return nil
	})
	return page, err
}

func (s *Service) mutateArticle(ctx context.Context, owner, id, mutation string, revision int64, draft *articlePayload, clear []byte, refs []reference, body map[string]any) (articleDocument, error) {
	var result articleDocument
	fingerprint := sha256.Sum256(encode(body))
	err := s.db.Write(ctx, func(tx *sql.Tx) error {
		previous, storedHash, err := s.article(ctx, tx, owner, id)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		exists := err == nil
		if exists && previous.MutationID == mutation {
			if !bytes.Equal(storedHash, fingerprint[:]) {
				return errConflict
			}
			result = previous
			return nil
		}
		if previous.Deleted || previous.Revision != revision {
			return errConflict
		}
		if draft != nil && previous.Draft != nil {
			var old articlePayload
			if err := json.Unmarshal(previous.Draft, &old); err != nil {
				return errUnavailable
			}
			if old.Published != nil && (draft.Published == nil || *draft.Published != *old.Published) {
				return errConflict
			}
			if old.locked() && (!old.sameContent(*draft) || !sameBindings(previous.Attachments, refs) ||
				(old.Claim != nil && draft.locked() && (draft.Claim == nil || *draft.Claim != *old.Claim))) {
				return errConflict
			}
		}
		if draft != nil || !exists {
			var total, live, used, oldSize int
			if err := tx.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(deleted=0),0),coalesce(sum(payload_bytes),0),
				coalesce(sum(CASE WHEN draft_id=? THEN payload_bytes ELSE 0 END),0) FROM private_article_drafts WHERE owner=?`, id, owner).Scan(&total, &live, &used, &oldSize); err != nil {
				return err
			}
			if (!exists && (total >= 1000 || (draft != nil && live >= 200))) || used-oldSize+len(clear) > 32<<20 {
				return errQuota
			}
		}
		if err := s.validateBoundAssets(ctx, tx, owner, draft, refs); err != nil {
			return err
		}
		var encrypted []byte
		if draft != nil {
			encrypted, err = s.sealMetadata(owner, "article-draft:"+id, clear)
			if err != nil {
				return err
			}
		}
		now := float64(time.Now().UnixMicro()) / 1e6
		if _, err := tx.ExecContext(ctx, `INSERT INTO private_social_draft_accounts(owner) VALUES(?) ON CONFLICT DO NOTHING`, owner); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO private_article_drafts(owner,draft_id,revision,deleted,mutation_id,mutation_hash,payload,payload_bytes,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(owner,draft_id) DO UPDATE SET revision=excluded.revision,deleted=excluded.deleted,
			mutation_id=excluded.mutation_id,mutation_hash=excluded.mutation_hash,payload=excluded.payload,payload_bytes=excluded.payload_bytes,
			updated_at=excluded.updated_at`, owner, id, revision+1, draft == nil, mutation, fingerprint[:], encrypted, len(clear), now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM private_article_draft_bindings WHERE owner=? AND draft_id=?`, owner, id); err != nil {
			return err
		}
		for _, ref := range refs {
			if _, err := tx.ExecContext(ctx, `INSERT INTO private_article_draft_bindings(owner,draft_id,local_asset_id,asset_id) VALUES(?,?,?,?)`, owner, id, ref.LocalID, ref.AssetID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM private_social_draft_cleanup WHERE owner=? AND asset_id=?`, owner, ref.AssetID); err != nil {
				return err
			}
		}
		for _, old := range previous.Attachments {
			if err := retireIfUnreferenced(ctx, tx, owner, old.Asset.ID, now); err != nil {
				return err
			}
		}
		result, _, err = s.article(ctx, tx, owner, id)
		return err
	})
	return result, err
}

func retireIfUnreferenced(ctx context.Context, tx *sql.Tx, owner, id string, now float64) error {
	updated, err := tx.ExecContext(ctx, `UPDATE private_social_draft_assets SET state='deleted' WHERE owner=? AND asset_id=?
	AND NOT EXISTS(SELECT 1 FROM private_social_draft_bindings WHERE owner=? AND asset_id=?)
	AND NOT EXISTS(SELECT 1 FROM private_article_draft_bindings WHERE owner=? AND asset_id=?)`, owner, id, owner, id, owner, id)
	if err != nil {
		return err
	}
	count, err := updated.RowsAffected()
	if err != nil || count == 0 {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO private_social_draft_cleanup(owner,asset_id,available_at,attempts) VALUES(?,?,?,0)
	ON CONFLICT(owner,asset_id) DO UPDATE SET available_at=excluded.available_at,attempts=0`, owner, id, now)
	return err
}

func sameBindings(bindings []binding, refs []reference) bool {
	if len(bindings) != len(refs) {
		return false
	}
	wanted := map[string]string{}
	for _, ref := range refs {
		wanted[ref.LocalID] = ref.AssetID
	}
	for _, bound := range bindings {
		if wanted[bound.LocalID] != bound.Asset.ID {
			return false
		}
	}
	return true
}

func (s *Service) validateBoundAssets(ctx context.Context, tx *sql.Tx, owner string, draft *articlePayload, refs []reference) error {
	if draft == nil {
		return nil
	}
	descriptors := map[string]articleAsset{}
	for _, a := range draft.Assets {
		descriptors[a.ID] = a
	}
	for _, ref := range refs {
		var data []byte
		var state string
		if err := tx.QueryRowContext(ctx, `SELECT metadata,state FROM private_social_draft_assets WHERE owner=? AND asset_id=?`, owner, ref.AssetID).Scan(&data, &state); err != nil {
			return err
		}
		value, err := s.decodeAsset(owner, ref.AssetID, state, data)
		if err != nil {
			return err
		}
		if value.State != "ready" {
			return errPending
		}
		descriptor := descriptors[ref.LocalID]
		if value.ContentType != descriptor.MIMEType || value.Name != descriptor.Name || value.Kind != "photo" || value.ByteLength > 1000000 {
			return errInvalid
		}
	}
	return nil
}
