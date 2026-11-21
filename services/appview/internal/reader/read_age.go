package reader

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

type readAgeOption struct {
	Days   int    `json:"days"`
	Before string `json:"before"`
	Count  int    `json:"count"`
}

type readAgeResponse struct {
	Options      []readAgeOption `json:"options"`
	ReferenceDay string          `json:"referenceDay"`
}

func validateBulkScope(scope bulkScope) error {
	if scope.Kind == "publication" {
		if strings.TrimSpace(scope.PublicationID) == "" {
			return httpx.Errorf(http.StatusBadRequest, "publication scope requires publicationId")
		}
	} else if scope.Kind == "folder" {
		if strings.TrimSpace(scope.FolderRKey) == "" {
			return httpx.Errorf(http.StatusBadRequest, "folder scope requires folderRkey")
		}
	} else if scope.Kind != "subscribed" && scope.Kind != "following" {
		return httpx.Errorf(http.StatusBadRequest, "Unsupported read scope")
	}
	return nil
}

func (s *service) bulkScopes(ctx context.Context, auth *reqctx.Auth, scope bulkScope) ([]thinstore.PublicationScope, error) {
	sidebar, err := s.publications.Cached(ctx, auth.DID)
	if err != nil {
		return nil, err
	}
	if sidebar == nil {
		sidebar, err = s.publications.Load(ctx, auth, false)
		if err != nil {
			return nil, err
		}
	}
	return publicationScopes(auth.DID, sidebar.ScopeFor(scope.Kind, scope.PublicationID, scope.FolderRKey)), nil
}

func (s *service) readAgeOptions(w http.ResponseWriter, r *http.Request) error {
	scope := bulkScope{httpx.Query(r, "kind"), httpx.Query(r, "publicationId"), httpx.Query(r, "folderRkey")}
	if err := validateBulkScope(scope); err != nil {
		return err
	}
	zoneName := httpx.Query(r, "timeZone")
	if zoneName == "" {
		return httpx.Errorf(http.StatusBadRequest, "timeZone is required")
	}
	zone, err := time.LoadLocation(zoneName)
	if err != nil || zoneName == "Local" {
		return httpx.Errorf(http.StatusBadRequest, "timeZone must be an IANA time zone identifier")
	}
	ctx := r.Context()
	auth := reqctx.AuthFrom(ctx)
	requested, err := s.bulkScopes(ctx, auth, scope)
	if err != nil {
		return err
	}
	now := time.Now().In(zone)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone)
	var offsets [8]int
	stream := strings.Contains(r.Header.Get("Accept"), "application/x-ndjson")
	emit := func(map[string]any) error { return nil }
	if stream {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Cache-Control", "no-cache")
		encoder := json.NewEncoder(w)
		emit = func(event map[string]any) error {
			if err := encoder.Encode(event); err != nil {
				return err
			}
			return http.NewResponseController(w).Flush()
		}
	}
	var seen int
	err = s.db.Read(ctx, func(tx *sql.Tx) error {
		return forEachUnread(ctx, tx, auth.DID, requested, func(_ string, date time.Time) error {
			for days := 7; days >= 1; days-- {
				if date.Before(today.AddDate(0, 0, -(days - 1))) {
					offsets[days]++
					break
				}
			}
			seen++
			if stream && seen%1000 == 0 {
				options := ageOptions(today, offsets)
				return emit(map[string]any{"type": "options", "options": options.Options, "referenceDay": options.ReferenceDay})
			}
			return nil
		})
	})
	if err != nil {
		if stream {
			return emit(map[string]any{"type": "error", "message": "Couldn't load read-age options."})
		}
		return err
	}
	options := ageOptions(today, offsets)
	if stream {
		if err := emit(map[string]any{"type": "options", "options": options.Options, "referenceDay": options.ReferenceDay}); err != nil {
			return err
		}
		return emit(map[string]any{"type": "done"})
	}
	return httpx.OK(w, options)
}

func ageOptions(today time.Time, offsets [8]int) readAgeResponse {
	result := readAgeResponse{Options: []readAgeOption{}, ReferenceDay: readAgeTimestamp(today)}
	count := 0
	for days := 7; days >= 1; days-- {
		if offsets[days] == 0 {
			continue
		}
		count += offsets[days]
		result.Options = append(result.Options, readAgeOption{days, readAgeTimestamp(today.AddDate(0, 0, -(days - 1))), count})
	}
	for i, j := 0, len(result.Options)-1; i < j; i, j = i+1, j-1 {
		result.Options[i], result.Options[j] = result.Options[j], result.Options[i]
	}
	return result
}

func readAgeTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

func (s *service) markReadBefore(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Scope  bulkScope `json:"scope"`
		Before string    `json:"before"`
	}
	if err := httpx.DecodeJSON(r, &body, 0); err != nil {
		return err
	}
	if err := validateBulkScope(body.Scope); err != nil {
		return err
	}
	now := time.Now()
	cutoff, err := time.Parse(time.RFC3339Nano, body.Before)
	if err != nil || cutoff.After(now) {
		return httpx.Errorf(http.StatusBadRequest, "before must be an ISO 8601 timestamp at or before now")
	}
	ctx := r.Context()
	auth := reqctx.AuthFrom(ctx)
	requested, err := s.bulkScopes(ctx, auth, body.Scope)
	if err != nil {
		return err
	}
	entryIDs := []string{}
	unread := map[string]int{}
	err = s.db.Write(ctx, func(tx *sql.Tx) error {
		if err := requireLegacyWrite(ctx, tx, auth.DID); err != nil {
			return err
		}
		if err := forEachUnread(ctx, tx, auth.DID, requested, func(uri string, publishedAt time.Time) error {
			if publishedAt.Before(cutoff) {
				entryIDs = append(entryIDs, uri)
			}
			return nil
		}); err != nil {
			return err
		}
		if len(entryIDs) == 0 {
			var err error
			unread, err = counts(ctx, tx, auth.DID, requested)
			return err
		}
		jsonIDs, err := json.Marshal(entryIDs)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM appview_unread_overrides WHERE viewer_did=? AND
			subject_uri IN (SELECT value FROM json_each(?))`, auth.DID, string(jsonIDs)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO read_marks(viewer_did,subject_uri,created_at)
			SELECT ?,value,? FROM json_each(?)`, auth.DID, thinstore.CursorTimestamp(now), string(jsonIDs)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE appview_unread_counters SET dirty=1 WHERE viewer_did=?", auth.DID); err != nil {
			return err
		}
		if err := invalidateCaches(ctx, tx, auth.DID); err != nil {
			return err
		}
		unread, err = counts(ctx, tx, auth.DID, requested)
		return err
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"marked": len(entryIDs), "entryIds": entryIDs, "readAt": readAgeTimestamp(now), "unreadCounts": unread})
}

// Unlike presentation feeds, read mutations retain duplicate article records so
// they cannot reappear unread after another copy was acted on.
func forEachUnread(ctx context.Context, db rowsQueryer, viewer string, requested []thinstore.PublicationScope, visit func(string, time.Time) error) error {
	if len(requested) == 0 {
		return nil
	}
	migrated, err := readAuthority(ctx, db, viewer)
	if err != nil {
		return err
	}
	scopeJSON, err := matches(ctx, db, requested)
	if err != nil {
		return err
	}
	query := readerCTEStart + `SELECT ci.uri,ci.created_at,
		CASE WHEN json_valid(ci.render_json) THEN json_extract(ci.render_json,'$.publishedAt') END AS published_at, ` +
		readExpression + " AS is_read" + readerJoins +
		") SELECT DISTINCT uri,created_at,published_at FROM candidates WHERE is_read=0 ORDER BY " + thinstore.FeedTimestampSQL("created_at") + " DESC, uri DESC"
	rows, err := db.QueryContext(ctx, query, string(scopeJSON), migrated, viewer, viewer, viewer, thinstore.CursorTimestamp(time.Now()))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var uri, created string
		var published sql.NullString
		if err := rows.Scan(&uri, &created, &published); err != nil {
			return err
		}
		date, err := time.Parse(time.RFC3339Nano, published.String)
		if err != nil {
			date, err = time.Parse(time.RFC3339Nano, created)
			if err != nil {
				continue
			}
		}
		if err := visit(uri, date); err != nil {
			return err
		}
	}
	return rows.Err()
}
