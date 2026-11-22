// Package syncapi serves the authenticated viewer's preferences and cached PDS records.
package syncapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/isotime"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

type snapshot struct {
	CID      string
	Body     json.RawMessage
	Value    json.RawMessage
	CachedAt time.Time
}
type recordReader func(context.Context, string, string, string) (snapshot, error)

func Migrate(ctx context.Context, db *sqlitedb.DB) error {
	return db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			CREATE TABLE IF NOT EXISTS pds_repo_record_cache (
				owner_did TEXT NOT NULL, scope_key TEXT NOT NULL, cid TEXT, json_body TEXT NOT NULL,
				cached_at TEXT NOT NULL, expires_at TEXT NOT NULL, PRIMARY KEY (owner_did, scope_key)
			);
			CREATE INDEX IF NOT EXISTS idx_pds_record_expires_at ON pds_repo_record_cache(expires_at);
		`)
		return err
	})
}

type service struct {
	db   *sqlitedb.DB
	read recordReader
}

func Register(group *httpx.Group, db *sqlitedb.DB, repo *atproto.RepoClient) {
	dns := atproto.NewDNSAddressValidator(32, nil)
	s := service{db: db, read: publicReader(repo.Resolver(), dns)}
	group.Get("/v1/sync/preferences", s.preferences)
	group.Get("/xrpc/app.thesocialwire.sync.getPreferences", s.preferences)
	group.Get("/v1/pds/cache/record", s.record)
	group.Post("/v1/sync/migrate-lexicons", func(w http.ResponseWriter, r *http.Request) error {
		auth := reqctx.AuthFrom(r.Context())
		if auth == nil {
			return httpx.Status(http.StatusUnauthorized)
		}
		summary, err := atproto.MigrateLegacyLexiconsIfNeeded(r.Context(), repo, auth)
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]any{"summary": summary})
	})
}

func (s service) preferences(w http.ResponseWriter, r *http.Request) error {
	fresh := r.URL.Query().Get("fresh")
	record, err := s.get(r, atproto.CollectionPreferences, atproto.PreferencesRKey, fresh == "true" || fresh == "1", 5*time.Minute, 30*time.Minute)
	if err != nil {
		return err
	}
	if matchETag(w, r, record.CID) {
		return nil
	}
	var revision *string
	if record.CID != "" {
		revision = &record.CID
	}
	return httpx.OK(w, map[string]any{
		"etag": revision, "cid": revision, "revision": revision,
		"cachedAt": isotime.Format(record.CachedAt), "record": record.Value,
	})
}

var recordKey = regexp.MustCompile(`^[a-zA-Z0-9._~:-]{1,512}$`)

func (s service) record(w http.ResponseWriter, r *http.Request) error {
	query := r.URL.Query()
	collection, rkey := query.Get("collection"), query.Get("rkey")
	if _, ok := atproto.ParseNSID(collection); !ok || !recordKey.MatchString(rkey) || rkey == "." || rkey == ".." {
		return httpx.Errorf(http.StatusBadRequest, "Query requires a valid collection and rkey")
	}
	record, err := s.get(r, collection, rkey, false, 2*time.Minute, 20*time.Minute)
	if err != nil {
		return err
	}
	if record.Body == nil {
		return httpx.Status(http.StatusNotFound)
	}
	if matchETag(w, r, record.CID) {
		return nil
	}
	return httpx.OK(w, record.Body)
}

func matchETag(w http.ResponseWriter, r *http.Request, cid string) bool {
	if cid == "" {
		return false
	}
	if strings.Trim(strings.TrimSpace(r.Header.Get("If-None-Match")), `"`) == cid {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	w.Header().Set("ETag", strconv.Quote(cid))
	return false
}

func (s service) get(r *http.Request, collection, rkey string, force bool, ttl, retention time.Duration) (snapshot, error) {
	ctx := r.Context()
	did := reqctx.ViewerDID(ctx)
	if did == "" {
		return snapshot{}, httpx.Status(http.StatusUnauthorized)
	}
	now := time.Now().UTC()
	scope := collection + ":" + rkey
	if !force {
		var body, cached, expires string
		var cid sql.NullString
		err := s.db.Reader().QueryRowContext(ctx, `
			SELECT cid, json_body, cached_at, expires_at FROM pds_repo_record_cache WHERE owner_did=? AND scope_key=?
		`, did, scope).Scan(&cid, &body, &cached, &expires)
		if err == nil {
			cachedAt, cachedErr := isotime.Parse(cached)
			expiresAt, expiresErr := isotime.Parse(expires)
			if cachedErr == nil && expiresErr == nil && now.Before(expiresAt) && now.Sub(cachedAt) < ttl {
				result, err := parseSnapshot([]byte(body), cachedAt)
				if err == nil {
					result.CID = cid.String
					return result, nil
				}
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			logRecordFailure(ctx, "cache-read", err)
			return snapshot{}, err
		}
	}
	result, err := s.read(ctx, did, collection, rkey)
	if err != nil {
		logRecordFailure(ctx, "public-read", err)
		return snapshot{}, err
	}
	result.CachedAt = now
	if result.Body == nil {
		_, err = s.db.Writer().ExecContext(ctx, `
			DELETE FROM pds_repo_record_cache WHERE owner_did=? AND scope_key=? AND cached_at<=?
		`, did, scope, isotime.Format(now))
		if err != nil {
			logRecordFailure(ctx, "cache-delete", err)
		}
		return result, err
	}
	_, err = s.db.Writer().ExecContext(ctx, `
		INSERT INTO pds_repo_record_cache(owner_did,scope_key,cid,json_body,cached_at,expires_at)
		VALUES(?,?,?,?,?,?) ON CONFLICT(owner_did,scope_key) DO UPDATE SET cid=excluded.cid,
		json_body=excluded.json_body,cached_at=excluded.cached_at,expires_at=excluded.expires_at
		WHERE pds_repo_record_cache.cached_at <= excluded.cached_at
	`, did, scope, result.CID, string(result.Body), isotime.Format(now), isotime.Format(now.Add(retention)))
	if err != nil {
		logRecordFailure(ctx, "cache-upsert", err)
	}
	return result, err
}

func publicReader(resolver *atproto.Resolver, dns *atproto.DNSAddressValidator) recordReader {
	return func(ctx context.Context, did, collection, rkey string) (snapshot, error) {
		ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
		unavailable := httpx.Errorf(http.StatusBadGateway, "PDS record lookup failed")
		base, err := resolver.ResolvePDSBaseStrict(ctx, did, atproto.EndpointPublicHTTPS)
		if err != nil || base == "" {
			return snapshot{}, unavailable
		}
		deadline, _ := ctx.Deadline()
		address, err := dns.ValidatedAddress(ctx, base, deadline)
		if err != nil {
			return snapshot{}, unavailable
		}
		query := url.Values{"repo": {did}, "collection": {collection}, "rkey": {rkey}}
		target := base + "/xrpc/com.atproto.repo.getRecord?" + query.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return snapshot{}, unavailable
		}
		req.Header.Set("Accept", "application/json")
		client := atproto.NewPinnedHTTPClient(address, 5*time.Second, 10*time.Second)
		defer client.CloseIdleConnections()
		resp, err := client.Do(req)
		if err != nil {
			return snapshot{}, unavailable
		}
		defer resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusBadRequest, http.StatusNotFound:
			return snapshot{}, nil
		case http.StatusOK:
		default:
			return snapshot{}, unavailable
		}
		const limit = 1024 * 1024
		body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		if err != nil || len(body) > limit {
			return snapshot{}, unavailable
		}
		return parseSnapshot(body, time.Time{})
	}
}

func parseSnapshot(body []byte, cachedAt time.Time) (snapshot, error) {
	var payload struct {
		CID   string          `json:"cid"`
		Value json.RawMessage `json:"value"`
	}
	if json.Unmarshal(body, &payload) != nil || len(payload.Value) == 0 {
		return snapshot{}, httpx.Errorf(http.StatusBadGateway, "Invalid PDS record response")
	}
	return snapshot{CID: strings.TrimSpace(payload.CID), Body: body, Value: payload.Value, CachedAt: cachedAt}, nil
}
