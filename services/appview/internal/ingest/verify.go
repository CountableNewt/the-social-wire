package ingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/repo"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

var errVerification = errors.New("authoritative article check unavailable")

type entryProof struct {
	cid     string
	expires time.Time
}

// EntryVerifier reconciles a small number of article detail reads directly
// against the author's PDS. Provider omission cannot remove an article.
type EntryVerifier struct {
	content   thinstore.SQLiteIngestStore
	transport repo.HTTPTransport
	plc       string
	retention time.Duration
	slots     chan struct{}
	mu        sync.Mutex
	cache     map[string]entryProof
}

func NewEntryVerifier(db *sqlitedb.DB, plc string, retention time.Duration) *EntryVerifier {
	return &EntryVerifier{
		content: thinstore.SQLiteIngestStore{DB: db}, transport: repo.PublicHTTPTransport{UserAgent: "TheSocialWire/1 (+https://thesocialwire.app)"},
		plc: plc, retention: retention, slots: make(chan struct{}, 4), cache: make(map[string]entryProof),
	}
}

func (v *EntryVerifier) Verify(ctx context.Context, entry string) (bool, error) {
	uri, ok := atproto.ParseRecordURI(entry)
	if !ok || uri.Repo == atproto.RSSAuthorDID || (uri.Collection != "site.standard.document" && uri.Collection != "site.standard.entry") {
		return true, nil
	}
	if len(entry) > 2048 || !repo.ValidRecordPath(uri.Collection+"/"+uri.RKey) {
		return true, errVerification
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var indexed string
	err := v.content.DB.Reader().QueryRowContext(ctx, "SELECT cid FROM content_items WHERE uri=?", entry).Scan(&indexed)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	v.mu.Lock()
	proof := v.cache[entry]
	v.mu.Unlock()
	if proof.cid != "" && proof.cid == indexed && time.Now().Before(proof.expires) {
		return true, nil
	}
	select {
	case v.slots <- struct{}{}:
		defer func() { <-v.slots }()
	default:
		return true, errVerification
	}
	base, err := v.identity(ctx, uri.Repo)
	if err != nil {
		return true, err
	}
	query := url.Values{"repo": {uri.Repo}, "collection": {uri.Collection}, "rkey": {uri.RKey}}
	endpoint, err := url.Parse(base + "/xrpc/com.atproto.repo.getRecord?" + query.Encode())
	if err != nil {
		return true, err
	}
	reply, err := v.transport.Get(ctx, endpoint, "application/json", 2<<20)
	if err != nil {
		return true, err
	}
	var result struct {
		Error string          `json:"error"`
		URI   string          `json:"uri"`
		CID   string          `json:"cid"`
		Value json.RawMessage `json:"value"`
	}
	if json.Unmarshal(reply.Body, &result) != nil {
		return true, errVerification
	}
	missing := (reply.Status == http.StatusBadRequest || reply.Status == http.StatusNotFound) && result.Error == "RecordNotFound"
	var value map[string]any
	if !missing && (reply.Status != http.StatusOK || result.URI != entry ||
		repo.VerifyPublicRecordCID(result.Value, result.CID) != nil ||
		json.Unmarshal(result.Value, &value) != nil || value["$type"] != uri.Collection) {
		return true, errVerification
	}
	current, err := v.identity(ctx, uri.Repo)
	if err != nil || current != base {
		return true, errVerification
	}
	if missing {
		deleted, err := v.content.DB.Writer().ExecContext(ctx, "DELETE FROM content_items WHERE uri=? AND cid=?", entry, indexed)
		if err != nil {
			return true, err
		}
		count, err := deleted.RowsAffected()
		if err != nil || count != 1 {
			return true, errVerification
		}
		v.mu.Lock()
		delete(v.cache, entry)
		v.mu.Unlock()
		return false, nil
	}
	if result.CID != indexed {
		now := time.Now().UTC()
		item := documentContent(entry, result.CID, uri.Repo, uri.Collection, value, now)
		if v.retention > 0 {
			item.ExpiresAt = now.Add(v.retention)
		}
		updated, err := v.content.CompareAndSwapContent(ctx, item, indexed)
		if err != nil || !updated {
			return true, errVerification
		}
	}
	v.mu.Lock()
	if len(v.cache) >= 512 {
		clear(v.cache)
	}
	v.cache[entry] = entryProof{cid: result.CID, expires: time.Now().Add(time.Minute)}
	v.mu.Unlock()
	return true, nil
}

func (v *EntryVerifier) identity(ctx context.Context, did string) (string, error) {
	var target *url.URL
	var err error
	switch {
	case strings.HasPrefix(did, "did:plc:"):
		base, ok := atproto.ValidatedPDSBase(v.plc, atproto.EndpointPublicHTTPS)
		if !ok || strings.ContainsAny(did, "/?#%\\ \r\n\t") {
			return "", errVerification
		}
		target, err = url.Parse(base + "/" + did)
	case strings.HasPrefix(did, "did:web:"):
		segments := strings.Split(strings.TrimPrefix(did, "did:web:"), ":")
		base, ok := atproto.ValidatedPDSBase("https://"+segments[0], atproto.EndpointPublicHTTPS)
		if !ok {
			return "", errVerification
		}
		target, err = url.Parse(base)
		if len(segments) == 1 {
			target = target.JoinPath(".well-known")
		}
		for _, segment := range segments[1:] {
			decoded, err := url.PathUnescape(segment)
			if err != nil || decoded == "" || decoded == "." || decoded == ".." ||
				strings.ContainsAny(decoded, "/?#\\ \r\n\t") {
				return "", errVerification
			}
			target = target.JoinPath(url.PathEscape(decoded))
		}
		target = target.JoinPath("did.json")
	default:
		return "", errVerification
	}
	if err != nil {
		return "", errVerification
	}
	reply, err := v.transport.Get(ctx, target, "application/json", 64<<10)
	if err != nil || reply.Status != http.StatusOK {
		return "", errVerification
	}
	var document struct {
		ID       string `json:"id"`
		Services []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Endpoint string `json:"serviceEndpoint"`
		} `json:"service"`
	}
	if json.Unmarshal(reply.Body, &document) != nil || document.ID != did {
		return "", errVerification
	}
	var pds string
	for _, service := range document.Services {
		if service.Type != "AtprotoPersonalDataServer" {
			continue
		}
		if pds != "" || (service.ID != "#atproto_pds" && service.ID != did+"#atproto_pds") {
			return "", errVerification
		}
		var ok bool
		pds, ok = atproto.ValidatedPDSBase(service.Endpoint, atproto.EndpointPublicHTTPS)
		if !ok {
			return "", errVerification
		}
	}
	if pds == "" {
		return "", errVerification
	}
	return pds, nil
}
