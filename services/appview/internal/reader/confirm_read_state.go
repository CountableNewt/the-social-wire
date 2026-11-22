package reader

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/readstate"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

type strictPDSResolver struct {
	resolver *atproto.Resolver
}

func (s strictPDSResolver) ResolvePDS(ctx context.Context, viewer string) (string, error) {
	return s.resolver.ResolvePDSBaseStrict(ctx, viewer, atproto.EndpointPublicHTTPS)
}

func (s *service) confirmReadState(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		ManifestCID            string `json:"manifestCid"`
		ExpectedLegacyRevision *int64 `json:"expectedLegacyRevision"`
	}
	if err := httpx.DecodeJSON(r, &body, 0); err != nil {
		return err
	}
	if body.ManifestCID == "" || len(body.ManifestCID) > 256 {
		return httpx.Errorf(http.StatusBadRequest, "manifestCid is required")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	status, err := s.confirmGeneration(ctx, reqctx.ViewerDID(ctx), body.ManifestCID, body.ExpectedLegacyRevision)
	if errors.Is(err, errLegacyScopeOverlap) {
		return feedError(w, r, http.StatusConflict, "ReadStateMigrationScopeConflict",
			"Overlapping publications have different read boundaries. Your existing read state is preserved; reconcile those boundaries before retrying migration.", true)
	}
	if err != nil {
		return err
	}
	return pdsJSON(w, status)
}

func (s *service) confirmGeneration(ctx context.Context, viewer, manifestCID string, expectedRevision *int64) (readstate.Status, error) {
	var empty readstate.Status
	if s.generationFetcher == nil {
		return empty, httpx.Status(http.StatusServiceUnavailable)
	}
	select {
	case s.confirmationSlots <- struct{}{}:
		defer func() { <-s.confirmationSlots }()
	case <-ctx.Done():
		return empty, ctx.Err()
	}
	record, err := s.generationFetcher.Fetch(ctx, viewer, readstate.ManifestCollection, "self", nil)
	if err != nil || record.CID != manifestCID {
		return empty, httpx.Errorf(http.StatusConflict, "Read-state manifest changed; refresh and retry")
	}
	manifest, err := record.VerifiedManifest(viewer)
	if err != nil {
		return empty, invalidGeneration()
	}
	projection, err := readstate.LoadRecords(ctx, manifest, viewer, readstate.LoadLimits{},
		func(ctx context.Context, reference readstate.Reference) ([]byte, error) {
			if cached, ok := s.verifiedChunks.Value(reference, time.Now()); ok {
				return cached, nil
			}
			if err := readstate.ValidateReference(reference, viewer); err != nil {
				return nil, err
			}
			at, ok := atproto.ParseRecordURI(reference.URI)
			if !ok || at.Repo != viewer {
				return nil, readstate.ErrInvalidReference
			}
			record, err := s.generationFetcher.Fetch(ctx, viewer, at.Collection, at.RKey, &reference.CID)
			if err != nil {
				return nil, err
			}
			if record.CID != reference.CID {
				return nil, readstate.ErrIncompleteGeneration
			}
			data, err := record.Verified(viewer, at.Collection, at.RKey)
			if err != nil {
				return nil, err
			}
			if err := s.verifiedChunks.Insert(data, reference, time.Now()); err != nil {
				return nil, err
			}
			return data, nil
		})
	if err != nil {
		return empty, invalidGeneration()
	}
	current, err := s.generationFetcher.Fetch(ctx, viewer, readstate.ManifestCollection, "self", nil)
	if err != nil || current.CID != manifestCID || current.URI != record.URI {
		return empty, httpx.Errorf(http.StatusConflict, "Read-state manifest changed during verification")
	}
	if _, err := current.VerifiedManifest(viewer); err != nil {
		return empty, invalidGeneration()
	}
	return s.activateGeneration(ctx, viewer, manifestCID, manifest, projection, expectedRevision)
}

func invalidGeneration() error {
	return httpx.Errorf(http.StatusConflict, "Read-state generation is invalid or incomplete")
}

var errLegacyScopeOverlap = errors.New("readstate: overlapping legacy scope floors")

func (s *service) activateGeneration(ctx context.Context, viewer, manifestCID string, manifest readstate.Manifest,
	projection *readstate.Projection, expectedRevision *int64) (readstate.Status, error) {
	var status readstate.Status
	if manifestCID == "" || projection == nil || projection.LastSequence != manifest.LastSequence || projection.ProtocolVersion != manifest.Version {
		return status, invalidGeneration()
	}
	if err := readstate.ValidateManifest(manifest, viewer); err != nil {
		return status, invalidGeneration()
	}
	encodedManifest, err := json.Marshal(manifest)
	if err != nil {
		return status, err
	}
	err = s.db.Write(ctx, func(tx *sql.Tx) error {
		if s.recoveryClaim != nil {
			var owned bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pds_recovery_jobs WHERE viewer_did=? AND status='running' AND lease_owner=? AND lease_until_us>?)`, viewer, s.recoveryClaim.owner, time.Now().UnixMicro()).Scan(&owned); err != nil {
				return err
			}
			if !owned {
				return errors.New("read-state recovery lease lost")
			}
		}
		prior, err := pdsStatus(ctx, tx, viewer)
		if err != nil {
			return err
		}
		if prior.Authority == readstate.AuthorityAppView {
			if manifest.Version != 1 {
				return migrationError("parityMismatch")
			}
			if expectedRevision == nil || *expectedRevision != prior.LegacyRevision {
				return migrationError("revisionChanged")
			}
			if err := verifyLegacyParity(ctx, tx, viewer, projection); err != nil {
				return err
			}
		} else {
			if prior.Manifest == nil || prior.ManifestCID == nil {
				return invalidGeneration()
			}
			if err := readstate.ValidateManifestTransition(*prior.Manifest, *prior.ManifestCID, manifest, manifestCID); err != nil {
				return invalidGeneration()
			}
			if prior.ProjectionReady && *prior.ManifestCID == manifestCID {
				status = prior
				return nil
			}
			if prior.ProjectionReady && readstate.IsMaintenanceTransition(prior.Manifest, manifest) {
				_, err := tx.ExecContext(ctx, "UPDATE appview_pds_read_state_authority SET manifest=?,manifest_cid=? WHERE viewer_did=?",
					string(encodedManifest), manifestCID, viewer)
				if err != nil {
					return err
				}
				status, err = pdsStatus(ctx, tx, viewer)
				return err
			}
		}
		if err := installProjection(ctx, tx, viewer, string(encodedManifest), manifestCID, projection); err != nil {
			return err
		}
		status, err = pdsStatus(ctx, tx, viewer)
		return err
	})
	return status, err
}

func verifyLegacyParity(ctx context.Context, tx *sql.Tx, viewer string, projection *readstate.Projection) error {
	var overlaps bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM appview_publication_scopes a JOIN appview_publication_read_floors af
			ON af.viewer_did=a.viewer_did AND af.publication_id=a.publication_id
		JOIN appview_publication_scopes b ON b.viewer_did=a.viewer_did AND b.author_did=a.author_did AND b.publication_id<>a.publication_id
		LEFT JOIN appview_publication_read_floors bf ON bf.viewer_did=b.viewer_did AND bf.publication_id=b.publication_id
		WHERE a.viewer_did=? AND (json_array_length(a.scope_keys)=0 OR json_array_length(b.scope_keys)=0
			OR EXISTS(SELECT 1 FROM json_each(a.scope_keys) ak JOIN json_each(b.scope_keys) bk ON ak.value=bk.value))
			AND (af.read_floor_at IS NOT bf.read_floor_at OR af.read_floor_uri IS NOT bf.read_floor_uri))`, viewer).Scan(&overlaps); err != nil {
		return err
	}
	if overlaps {
		return errLegacyScopeOverlap
	}
	bySequence := make(map[int64][]readstate.Operation, len(projection.Operations))
	for _, op := range projection.Operations {
		bySequence[op.Sequence] = append(bySequence[op.Sequence], op)
	}
	const pageSize = 1000
	for offset := 0; ; offset += pageSize {
		rows, err := legacyRows(ctx, tx, viewer, pageSize, offset)
		if err != nil {
			return err
		}
		for index, row := range rows {
			ops := bySequence[int64(offset+index+1)]
			if len(ops) == 0 {
				return migrationError("parityMismatch")
			}
			for _, op := range ops {
				if !matchesLegacyOperation(row, op) {
					return migrationError("parityMismatch")
				}
			}
		}
		if len(rows) < pageSize {
			return nil
		}
	}
}

func matchesLegacyOperation(row readstate.LegacyRow, op readstate.Operation) bool {
	actionTime, actionErr := readstate.ParseDate(op.ActedAt)
	priorTime, priorErr := readstate.ParseDate(row.ActedAt)
	if actionErr != nil || priorErr != nil || !actionTime.Equal(priorTime) || op.Calendar != nil {
		return false
	}
	if row.Boundary != nil {
		if op.State != readstate.StateRead || op.Selection != readstate.SelectionBoundaries || len(op.Boundaries) != 1 {
			return false
		}
		a, b := op.Boundaries[0], *row.Boundary
		return a.CreatedAt == b.CreatedAt && sameOptionalString(a.EntryID, b.EntryID) &&
			a.Scope.PublicationID == b.Scope.PublicationID && a.Scope.AuthorDID == b.Scope.AuthorDID &&
			slices.Equal(a.Scope.PublicationSiteKeys, b.Scope.PublicationSiteKeys)
	}
	expected := readstate.StateUnread
	if row.Kind == readstate.LegacyRead {
		expected = readstate.StateRead
	}
	return row.SubjectURI != nil && op.State == expected && op.Selection == readstate.SelectionExact &&
		len(op.SubjectURIs) == 1 && op.SubjectURIs[0] == *row.SubjectURI
}

func installProjection(ctx context.Context, tx *sql.Tx, viewer, manifest, cid string, projection *readstate.Projection) error {
	if _, err := tx.ExecContext(ctx, "UPDATE appview_pds_projection_write SET active=1 WHERE singleton=1"); err != nil {
		return err
	}
	for _, table := range []string{"appview_pds_read_state_exact", "appview_pds_read_state_boundaries",
		"read_marks", "appview_unread_overrides", "appview_publication_read_floors"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE viewer_did=?", viewer); err != nil {
			return err
		}
	}
	exact, err := tx.PrepareContext(ctx, `INSERT INTO appview_pds_read_state_exact VALUES(?,?,?,?,?)
		ON CONFLICT(viewer_did,subject_uri) DO UPDATE SET sequence=excluded.sequence,is_read=excluded.is_read,acted_at=excluded.acted_at
		WHERE sequence < excluded.sequence`)
	if err != nil {
		return err
	}
	defer exact.Close()
	boundaries, err := tx.PrepareContext(ctx, "INSERT OR REPLACE INTO appview_pds_read_state_boundaries (viewer_did,sequence,rule_key,is_read,acted_at,author_did,scope_keys,boundary_at,boundary_uri,boundary_at_us) VALUES(?,?,?,?,?,?,?,?,?,?)")
	if err != nil {
		return err
	}
	defer boundaries.Close()
	for _, op := range projection.Operations {
		for _, id := range op.SubjectURIs {
			if _, err := exact.ExecContext(ctx, viewer, id, op.Sequence, op.State == readstate.StateRead, op.ActedAt); err != nil {
				return err
			}
		}
		for _, boundary := range op.Boundaries {
			key, err := json.Marshal(boundary)
			if err != nil {
				return err
			}
			siteKeys := boundary.Scope.PublicationSiteKeys
			if siteKeys == nil {
				siteKeys = []string{}
			}
			keys, err := json.Marshal(siteKeys)
			if err != nil {
				return err
			}
			boundaryMicros, err := sqlitedb.TimestampMicroseconds(boundary.CreatedAt)
			if err != nil {
				return err
			}
			if _, err := boundaries.ExecContext(ctx, viewer, op.Sequence, string(key), op.State == readstate.StateRead,
				op.ActedAt, boundary.Scope.AuthorDID, string(keys), boundary.CreatedAt, boundary.EntryID, boundaryMicros); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO read_marks
		SELECT ?,e.subject_uri,e.acted_at FROM appview_pds_read_state_exact e
		WHERE e.viewer_did=? AND e.is_read=1 AND NOT EXISTS(SELECT 1 FROM content_items c WHERE c.uri=e.subject_uri)`, viewer, viewer); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO read_marks SELECT ?,c.uri,c.created_at FROM content_items c
		WHERE (EXISTS(SELECT 1 FROM appview_pds_read_state_exact e WHERE e.viewer_did=? AND e.subject_uri=c.uri)
			OR c.author_did IN (SELECT b.author_did FROM appview_pds_read_state_boundaries b WHERE b.viewer_did=?))
		AND COALESCE((SELECT state FROM (
			SELECT e.is_read AS state,e.sequence FROM appview_pds_read_state_exact e WHERE e.viewer_did=? AND e.subject_uri=c.uri
			UNION ALL
			SELECT b.is_read,b.sequence FROM appview_pds_read_state_boundaries b WHERE b.viewer_did=? AND b.author_did=c.author_did
				AND (json_array_length(b.scope_keys)=0 OR EXISTS(SELECT 1 FROM json_each(b.scope_keys) WHERE value=c.publication_site))
				AND (c.created_at_us<b.boundary_at_us
					OR (c.created_at_us=b.boundary_at_us AND (b.boundary_uri IS NULL OR c.uri<=b.boundary_uri)))
		) ORDER BY sequence DESC LIMIT 1),0)=1`, viewer, viewer, viewer, viewer, viewer); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO appview_pds_read_state_authority(viewer_did,manifest,manifest_cid,projection_ready,last_accessed_at)
		VALUES(?,?,?,1,?) ON CONFLICT(viewer_did) DO UPDATE SET manifest=excluded.manifest,
			manifest_cid=excluded.manifest_cid,projection_ready=1,last_accessed_at=excluded.last_accessed_at`,
		viewer, manifest, cid, float64(time.Now().Unix())); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE appview_unread_counters SET dirty=1 WHERE viewer_did=?", viewer); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM sidebar_projection_cache WHERE viewer_did=?", viewer); err != nil {
		return err
	}
	if err := invalidateCaches(ctx, tx, viewer); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE appview_pds_projection_write SET active=0 WHERE singleton=1")
	return err
}
