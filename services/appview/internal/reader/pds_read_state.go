package reader

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/readstate"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

func pdsStatus(ctx context.Context, db rowsQueryer, viewer string) (readstate.Status, error) {
	status := readstate.Status{Authority: readstate.AuthorityAppView, MigrationState: readstate.MigrationNotStarted, ProjectionReady: true}
	var manifest *string
	err := db.QueryRowContext(ctx, `SELECT legacy_revision,manifest,manifest_cid,projection_ready
		FROM appview_pds_read_state_authority WHERE viewer_did=?`, viewer).
		Scan(&status.LegacyRevision, &manifest, &status.ManifestCID, &status.ProjectionReady)
	if errors.Is(err, sql.ErrNoRows) {
		return status, nil
	}
	if err != nil {
		return status, err
	}
	if status.ManifestCID != nil {
		status.Authority = readstate.AuthorityPDS
		status.MigrationState = readstate.MigrationVerified
	}
	if manifest != nil {
		parsed, err := readstate.DecodeManifest([]byte(*manifest))
		if err != nil {
			return status, err
		}
		status.Manifest = &parsed
	}
	return status, nil
}

func (s *service) readStateStatus(w http.ResponseWriter, r *http.Request) error {
	viewer := reqctx.ViewerDID(r.Context())
	status, err := pdsStatus(r.Context(), s.db.Reader(), viewer)
	if err != nil {
		return err
	}
	s.scheduleRecovery(viewer, status)
	return pdsJSON(w, status)
}

func (s *service) scheduleRecovery(viewer string, status readstate.Status) {
	if status.ProjectionReady || status.ManifestCID == nil || s.generationFetcher == nil {
		return
	}
	if _, running := s.recovering.LoadOrStore(viewer, struct{}{}); running {
		return
	}
	select {
	case s.recoverySlots <- struct{}{}:
	default:
		s.recovering.Delete(viewer)
		return
	}
	go func() {
		defer func() {
			<-s.recoverySlots
			s.recovering.Delete(viewer)
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// A newer manifest may have committed before the old projection was
		// evicted. Confirm the viewer's current verified record in either case.
		record, err := s.generationFetcher.Fetch(ctx, viewer, readstate.ManifestCollection, "self", nil)
		if err == nil && record.CID != "" {
			_, _ = s.confirmGeneration(ctx, viewer, record.CID, nil)
		}
	}()
}

func pdsJSON(w http.ResponseWriter, body any) error {
	w.Header().Set("Cache-Control", "no-store")
	return httpx.OK(w, body)
}

const legacyReadStateCTE = `WITH baseline AS (
	SELECT 0 AS phase, f.publication_id AS key, f.updated_at AS acted_at,
		f.read_floor_at AS boundary_at, f.read_floor_uri AS boundary_uri, ps.author_did, ps.scope_keys
	FROM appview_publication_read_floors f LEFT JOIN appview_publication_scopes ps
		ON ps.viewer_did=f.viewer_did AND ps.publication_id=f.publication_id WHERE f.viewer_did=?
	UNION ALL SELECT 1,subject_uri,created_at,NULL,NULL,NULL,NULL FROM appview_unread_overrides WHERE viewer_did=?
	UNION ALL SELECT 2,subject_uri,created_at,NULL,NULL,NULL,NULL FROM read_marks WHERE viewer_did=?
)`

func legacyRows(ctx context.Context, db rowsQueryer, viewer string, limit, offset int) ([]readstate.LegacyRow, error) {
	rows, err := db.QueryContext(ctx, legacyReadStateCTE+`SELECT phase,key,acted_at,boundary_at,boundary_uri,author_did,scope_keys
		FROM baseline ORDER BY phase,key COLLATE BINARY LIMIT ? OFFSET ?`, viewer, viewer, viewer, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	page := []readstate.LegacyRow{}
	for rows.Next() {
		var row readstate.LegacyRow
		var phase int
		var key string
		var at, entry, author, rawKeys *string
		if err := rows.Scan(&phase, &key, &row.ActedAt, &at, &entry, &author, &rawKeys); err != nil {
			return nil, err
		}
		switch phase {
		case 0:
			if at == nil || author == nil || rawKeys == nil {
				return nil, migrationError("legacyScopeUnavailable")
			}
			var keys []string
			if err := json.Unmarshal([]byte(*rawKeys), &keys); err != nil {
				return nil, err
			}
			slices.Sort(keys)
			row.Kind = readstate.LegacyBoundary
			row.Boundary = &readstate.Boundary{
				Scope:     readstate.Scope{PublicationID: key, AuthorDID: *author, PublicationSiteKeys: keys},
				CreatedAt: *at, EntryID: entry,
			}
		case 1:
			row.Kind = readstate.LegacyUnread
			row.SubjectURI = &key
		default:
			row.Kind = readstate.LegacyRead
			row.SubjectURI = &key
		}
		page = append(page, row)
	}
	return page, rows.Err()
}

func migrationError(reason string) error {
	return httpx.Errorf(http.StatusConflict, "Read-state migration could not be confirmed: "+reason)
}

func (s *service) exportReadState(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Cursor                 *string `json:"cursor"`
		ExpectedLegacyRevision *int64  `json:"expectedLegacyRevision"`
		Limit                  *int    `json:"limit"`
	}
	if err := httpx.DecodeJSON(r, &body, 0); err != nil {
		return err
	}
	limit, offset := 100, 0
	if body.Limit != nil {
		limit = min(max(*body.Limit, 1), 500)
	}
	if body.Cursor != nil {
		value, err := base64.StdEncoding.DecodeString(*body.Cursor)
		offset, _ = strconv.Atoi(string(value))
		if err != nil || offset < 0 || strconv.Itoa(offset) != string(value) || body.ExpectedLegacyRevision == nil {
			return httpx.Errorf(http.StatusBadRequest, "Read-state migration could not be confirmed: invalidCursor")
		}
	}
	ctx := r.Context()
	viewer := reqctx.ViewerDID(ctx)
	var result readstate.ExportPage
	err := s.db.Read(ctx, func(tx *sql.Tx) error {
		status, err := pdsStatus(ctx, tx, viewer)
		if err != nil {
			return err
		}
		if status.Authority != readstate.AuthorityAppView {
			return migrationError("alreadyMigrated")
		}
		if body.ExpectedLegacyRevision != nil && *body.ExpectedLegacyRevision != status.LegacyRevision {
			return migrationError("revisionChanged")
		}
		page, err := legacyRows(ctx, tx, viewer, limit+1, offset)
		if err != nil {
			return err
		}
		if len(page) == 0 && offset != 0 {
			var total int
			if err := tx.QueryRowContext(ctx, legacyReadStateCTE+"SELECT count(*) FROM baseline", viewer, viewer, viewer).Scan(&total); err != nil {
				return err
			}
			if offset > total {
				return httpx.Errorf(http.StatusBadRequest, "Read-state migration could not be confirmed: invalidCursor")
			}
		}
		result = readstate.ExportPage{LegacyRevision: status.LegacyRevision, Rows: page}
		if len(page) > limit {
			cursor := base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(offset + limit)))
			result.Cursor, result.Rows = &cursor, page[:limit]
		}
		return nil
	})
	if err != nil {
		return err
	}
	return pdsJSON(w, result)
}

type preparedSelection struct {
	ActedAt            string                       `json:"actedAt"`
	Selection          readstate.Selection          `json:"selection"`
	Boundaries         *[]readstate.Boundary        `json:"boundaries,omitempty"`
	SubjectURIs        *[]string                    `json:"subjectUris,omitempty"`
	Calendar           *readstate.CalendarSelection `json:"calendar,omitempty"`
	LegacyRevision     int64                        `json:"legacyRevision"`
	ManifestCID        *string                      `json:"manifestCid,omitempty"`
	PreviewSubjectURIs *[]string                    `json:"previewSubjectUris,omitempty"`
}

func (s *service) prepareReadState(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Scope         bulkScope `json:"scope"`
		Before        *string   `json:"before"`
		TimeZone      *string   `json:"timeZone"`
		ReferenceDate *string   `json:"referenceDate"`
		Preview       *[]string `json:"previewSubjectUris"`
	}
	if err := httpx.DecodeJSON(r, &body, 0); err != nil {
		return err
	}
	if err := validateBulkScope(body.Scope); err != nil {
		return err
	}
	if body.Preview != nil && (len(*body.Preview) > 1000 || slices.ContainsFunc(*body.Preview, func(uri string) bool {
		return uri == "" || len(uri) > 2048
	})) {
		return httpx.Errorf(http.StatusBadRequest, "Preview supports at most 1000 cached subject IDs")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	auth := reqctx.AuthFrom(ctx)
	requested, err := s.bulkScopes(ctx, auth, body.Scope)
	if err != nil {
		return err
	}
	now := time.Now()
	actedAt := readAgeTimestamp(now)
	var result preparedSelection
	err = s.db.Read(ctx, func(tx *sql.Tx) error {
		status, err := pdsStatus(ctx, tx, auth.DID)
		if err != nil {
			return err
		}
		if !status.ProjectionReady {
			return httpx.Named(http.StatusServiceUnavailable, "ReadStateNotReady", "Read state is syncing. Please retry shortly.")
		}
		result = preparedSelection{ActedAt: actedAt, LegacyRevision: status.LegacyRevision, ManifestCID: status.ManifestCID}
		if body.Before != nil {
			cutoff, err := time.Parse(time.RFC3339Nano, *body.Before)
			if err != nil || cutoff.After(now) {
				return httpx.Errorf(http.StatusBadRequest, "before must be an ISO 8601 timestamp at or before now")
			}
			if body.TimeZone == nil || body.ReferenceDate == nil {
				return httpx.Errorf(http.StatusBadRequest, "Age selection requires timeZone and referenceDate")
			}
			calendar := &readstate.CalendarSelection{Cutoff: readAgeTimestamp(cutoff), TimeZone: *body.TimeZone, ReferenceDate: *body.ReferenceDate}
			if err := readstate.ValidateOperation(readstate.NewExactOperation("calendar-validation", 1, readstate.StateRead,
				actedAt, []string{"validation"}, calendar)); err != nil {
				return httpx.Errorf(http.StatusBadRequest, "Invalid read-state calendar selection")
			}
			ids, scanned, selectionBytes := []string{}, 0, 0
			if err := forEachUnread(ctx, tx, auth.DID, requested, func(uri string, publishedAt time.Time) error {
				scanned++
				if publishedAt.Before(cutoff) {
					ids = append(ids, uri)
					selectionBytes += len(uri) + 4
				}
				if len(ids) > 100000 || scanned > 250000 || selectionBytes > 8*1024*1024 {
					return httpx.Errorf(http.StatusRequestEntityTooLarge, "Select a smaller read scope")
				}
				return nil
			}); err != nil {
				return err
			}
			result.Selection, result.SubjectURIs, result.Calendar = readstate.SelectionExact, &ids, calendar
			return nil
		}
		boundaries, err := prepareBoundaries(ctx, tx, auth.DID, requested, now)
		if err != nil {
			return err
		}
		result.Selection, result.Boundaries = readstate.SelectionBoundaries, &boundaries
		if body.Preview != nil {
			preview, err := previewBoundaries(ctx, tx, boundaries, *body.Preview)
			if err != nil {
				return err
			}
			result.PreviewSubjectURIs = &preview
		}
		return nil
	})
	if err != nil {
		return err
	}
	status, err := pdsStatus(ctx, s.db.Reader(), auth.DID)
	if err != nil {
		return err
	}
	if status.LegacyRevision != result.LegacyRevision || !sameOptionalString(status.ManifestCID, result.ManifestCID) {
		return httpx.Errorf(http.StatusConflict, "Read state changed during selection; refresh and retry")
	}
	return pdsJSON(w, result)
}

func sameOptionalString(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func prepareBoundaries(ctx context.Context, db rowsQueryer, viewer string, requested []thinstore.PublicationScope, now time.Time) ([]readstate.Boundary, error) {
	slices.SortFunc(requested, func(a, b thinstore.PublicationScope) int { return strings.Compare(a.PublicationID, b.PublicationID) })
	result := make([]readstate.Boundary, 0, len(requested))
	for _, scope := range requested {
		if len(scope.ScopeKeys) == 0 && (scope.PublicationAtURI != nil || len(scope.PublicationScopeAtURIs)+len(scope.PublicationSiteURLs) > 0) {
			return nil, migrationError("legacyScopeUnavailable")
		}
		keys, err := json.Marshal(scope.ScopeKeys)
		if err != nil {
			return nil, err
		}
		boundary := thinstore.ReadWatermarkBoundary{PublicationID: scope.PublicationID, CreatedAt: now}
		var at, entry string
		err = db.QueryRowContext(ctx, `SELECT uri,created_at FROM content_items
			WHERE author_did=? AND tsw_timestamp_us(created_at)<=tsw_timestamp_us(?) AND
				(? OR EXISTS(SELECT 1 FROM json_each(?) WHERE value=publication_site))
			ORDER BY `+thinstore.FeedTimestampSQL("created_at")+` DESC,uri DESC LIMIT 1`,
			scope.AuthorDID, thinstore.CursorTimestamp(now), len(scope.ScopeKeys) == 0, string(keys)).Scan(&entry, &at)
		if err == nil {
			boundary.CreatedAt, err = readstate.ParseDate(at)
			boundary.EntryID = &entry
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		var priorAt string
		var priorEntry *string
		err = db.QueryRowContext(ctx, "SELECT read_floor_at,read_floor_uri FROM appview_publication_read_floors WHERE viewer_did=? AND publication_id=?",
			viewer, scope.PublicationID).Scan(&priorAt, &priorEntry)
		if err == nil {
			date, err := readstate.ParseDate(priorAt)
			if err != nil {
				return nil, err
			}
			prior := thinstore.ReadWatermarkBoundary{PublicationID: scope.PublicationID, CreatedAt: date, EntryID: priorEntry}
			if prior.IsAfter(boundary) {
				boundary = prior
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		result = append(result, readstate.Boundary{
			Scope:     readstate.Scope{PublicationID: scope.PublicationID, AuthorDID: scope.AuthorDID, PublicationSiteKeys: scope.ScopeKeys},
			CreatedAt: thinstore.CursorTimestamp(boundary.CreatedAt), EntryID: boundary.EntryID,
		})
	}
	return result, nil
}

func previewBoundaries(ctx context.Context, db rowsQueryer, boundaries []readstate.Boundary, subjectURIs []string) ([]string, error) {
	if len(boundaries) > 1000 {
		return nil, httpx.Errorf(http.StatusRequestEntityTooLarge, "Select a smaller read scope")
	}
	operations := make([]readstate.Operation, len(boundaries))
	for index, boundary := range boundaries {
		operations[index] = readstate.NewBoundaryOperation("preview-"+strconv.Itoa(index), int64(index+1), readstate.StateRead,
			boundary.CreatedAt, []readstate.Boundary{boundary})
	}
	projection, err := readstate.NewProjection(operations, int64(len(operations)), readstate.ProjectionSource{})
	if err != nil {
		return nil, err
	}
	values, err := json.Marshal(subjectURIs)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT uri,author_did,publication_site,created_at FROM content_items
		WHERE uri IN (SELECT value FROM json_each(?)) ORDER BY uri COLLATE BINARY`, string(values))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []string{}
	for rows.Next() {
		var subject readstate.Subject
		var at string
		if err := rows.Scan(&subject.URI, &subject.AuthorDID, &subject.PublicationSite, &at); err != nil {
			return nil, err
		}
		subject.CreatedAt, err = readstate.ParseDate(at)
		if err != nil {
			return nil, err
		}
		if projection.Resolve(subject).IsRead {
			result = append(result, subject.URI)
		}
	}
	return result, rows.Err()
}
