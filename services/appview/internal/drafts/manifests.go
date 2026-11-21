package drafts

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
)

var socialDraftID = regexp.MustCompile(`^[234567abcdefghij][234567abcdefghijklmnopqrstuvwxyz]{12}$`)

type manifestRef struct {
	AssetID string `json:"assetId"`
	Role    string `json:"role"`
	Ref     string `json:"ref"`
}
type manifestBinding struct {
	manifestRef
	Asset asset `json:"asset"`
}
type manifest struct {
	ID          string            `json:"draftId"`
	Revision    int64             `json:"revision"`
	Deleted     bool              `json:"deleted"`
	Attachments []manifestBinding `json:"attachments"`
}

type manifestInput struct {
	ID       string        `json:"draftId"`
	Expected int64         `json:"expectedRevision"`
	Refs     []manifestRef `json:"attachments"`
}

func validManifestRefs(refs []manifestRef) bool {
	if len(refs) > 800 {
		return false
	}
	seen := map[manifestRef]bool{}
	for _, ref := range refs {
		if seen[ref] || !uuid.MatchString(ref.AssetID) || len(ref.Ref) > 512 || strings.ContainsRune(ref.Ref, 0) {
			return false
		}
		switch ref.Role {
		case "original", "prepared", "video", "poster", "caption", "gif":
		default:
			return false
		}
		seen[ref] = true
	}
	return true
}

func (s *Service) readManifest(ctx context.Context, tx *sql.Tx, owner, id string) (manifest, error) {
	result := manifest{ID: id, Attachments: []manifestBinding{}}
	err := tx.QueryRowContext(ctx, `SELECT revision,deleted FROM private_social_draft_manifests WHERE owner=? AND draft_id=?`, owner, id).
		Scan(&result.Revision, &result.Deleted)
	if err != nil || result.Deleted {
		return result, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT b.asset_id,b.role,b.ref,a.state,a.metadata FROM private_social_draft_bindings b
		JOIN private_social_draft_assets a ON a.owner=b.owner AND a.asset_id=b.asset_id
		WHERE b.owner=? AND b.draft_id=? ORDER BY b.position`, owner, id)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var bound manifestBinding
		var state string
		var metadata []byte
		if err := rows.Scan(&bound.AssetID, &bound.Role, &bound.Ref, &state, &metadata); err != nil {
			return result, err
		}
		bound.Asset, err = s.decodeAsset(owner, bound.AssetID, state, metadata)
		if err != nil {
			return result, err
		}
		result.Attachments = append(result.Attachments, bound)
	}
	return result, rows.Err()
}

func (s *Service) putManifest(ctx context.Context, owner string, input manifestInput, deleted bool) (manifest, error) {
	var result manifest
	err := s.db.Write(ctx, func(tx *sql.Tx) error {
		prior, err := s.readManifest(ctx, tx, owner, input.ID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if prior.Revision != input.Expected {
			return errConflict
		}
		for _, ref := range input.Refs {
			value, _, err := s.readAsset(ctx, tx, owner, ref.AssetID)
			if err != nil {
				return err
			}
			if value.State != "ready" {
				return errPending
			}
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO private_social_draft_accounts(owner) VALUES(?) ON CONFLICT DO NOTHING`, owner); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO private_social_draft_manifests(owner,draft_id,revision,deleted) VALUES(?,?,?,?)
			ON CONFLICT(owner,draft_id) DO UPDATE SET revision=excluded.revision,deleted=excluded.deleted`, owner, input.ID, input.Expected+1, deleted); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM private_social_draft_bindings WHERE owner=? AND draft_id=?`, owner, input.ID); err != nil {
			return err
		}
		for i, ref := range input.Refs {
			if _, err = tx.ExecContext(ctx, `INSERT INTO private_social_draft_bindings(owner,draft_id,position,asset_id,role,ref) VALUES(?,?,?,?,?,?)`,
				owner, input.ID, i, ref.AssetID, ref.Role, ref.Ref); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `DELETE FROM private_social_draft_cleanup WHERE owner=? AND asset_id=?`, owner, ref.AssetID); err != nil {
				return err
			}
		}
		for _, old := range prior.Attachments {
			if err := retireIfUnreferenced(ctx, tx, owner, old.AssetID, nowSeconds()); err != nil {
				return err
			}
		}
		result, err = s.readManifest(ctx, tx, owner, input.ID)
		return err
	})
	return result, err
}
