// Package backend runs the standalone API's embedded scheduling service.
package backend

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling/oauthclient"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling/scheduledata"
)

type Service struct {
	origin           string
	core             config.Core
	store            *scheduledata.Store
	client           *oauthclient.Client
	pds              pds
	uploadBlob       blobUpload
	submissionSecret [32]byte
	logger           *slog.Logger
	oauthAdmission   chan struct{}
}

// Open validates scheduling configuration and opens the durable Swift-compatible store.
func Open(ctx context.Context, core config.Core, env config.Env, client *http.Client, logger *slog.Logger, initializeRemote ...bool) (*Service, error) {
	if core.DatabaseBackend != "turso" && !filepath.IsAbs(core.SQLitePath) {
		return nil, errors.New("embedded scheduling requires an absolute persistent SQLite path")
	}
	origin := env.String("SCHEDULING_PUBLIC_ORIGIN")
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil ||
		parsed.Path != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return nil, errors.New("SCHEDULING_PUBLIC_ORIGIN must be an HTTPS origin")
	}
	key := env.String("SCHEDULING_STORAGE_KEY")
	decoded, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(decoded) != 32 {
		return nil, errors.New("SCHEDULING_STORAGE_KEY must be a Base64-encoded 32-byte key")
	}
	var store *scheduledata.Store
	if core.DatabaseBackend == "turso" {
		initialize := len(initializeRemote) > 0 && initializeRemote[0]
		store, err = scheduledata.OpenTurso(ctx, core.SchedulingTursoDatabaseURL, core.SchedulingTursoAuthToken, key, initialize)
	} else {
		store, err = scheduledata.Open(ctx, filepath.Join(filepath.Dir(core.SQLitePath), "scheduling.sqlite"), key, true)
	}
	if err != nil {
		return nil, err
	}
	oauth := oauthclient.New(origin, client)
	service := &Service{
		origin: origin, core: core, store: store, client: oauth, logger: logger,
		submissionSecret: sha256.Sum256([]byte("schedule-submissions:" + key)),
		oauthAdmission:   make(chan struct{}, 32),
	}
	service.uploadBlob = func(ctx context.Context, did, contentType string, body []byte) ([]byte, error) {
		return oauth.XRPC(ctx, did, http.MethodPost, "com.atproto.repo.uploadBlob", nil, body, contentType, sessionStore{store})
	}
	service.pds = pds{
		rpc: func(ctx context.Context, did, method, nsid string, values url.Values, body []byte) ([]byte, error) {
			return oauth.XRPC(ctx, did, method, nsid, values, body, "application/json", sessionStore{store})
		},
		isNotFound: func(err error) bool {
			var response *oauthclient.XRPCError
			return errors.As(err, &response) && (response.Status == http.StatusNotFound || response.Code == "RecordNotFound")
		},
	}
	return service, nil
}

func (s *Service) Close() error { return s.store.Close() }

// Ready checks scheduling connectivity and remote schema certification without
// repeating the encrypted-vault scan performed when the service was opened.
func (s *Service) Ready(ctx context.Context) error { return s.store.Ready(ctx) }

func randomToken() string {
	var entropy [32]byte
	rand.Read(entropy[:])
	return base64.RawURLEncoding.EncodeToString(entropy[:])
}

type sessionStore struct{ store *scheduledata.Store }

func (s sessionStore) LoadOAuthSession(ctx context.Context, did string) (*oauthclient.Session, error) {
	session, err := s.store.OAuthSession(ctx, did)
	if err != nil || session == nil {
		return nil, err
	}
	return &oauthclient.Session{
		DID: session.DID, Handle: session.Handle, TokenJSON: session.TokenJSON, DPoPKeyJSON: session.DPoPKeyJSON,
	}, nil
}

func (s sessionStore) ClaimOAuthRefresh(ctx context.Context, did, owner, hash string, now, until int64) (bool, error) {
	held, err := s.store.ClaimOAuthRefresh(ctx, did, owner, hash, now, until)
	if errors.Is(err, scheduledata.ErrRefreshUncertain) {
		return false, oauthclient.ErrNotConfigured
	}
	return held, err
}
func (s sessionStore) BeginOAuthRefresh(ctx context.Context, did, owner, hash string, now int64) error {
	return s.store.BeginOAuthRefresh(ctx, did, owner, hash, now)
}
func (s sessionStore) ReleaseOAuthRefresh(ctx context.Context, did, owner string) error {
	return s.store.ReleaseActorLease(ctx, "oauth-refresh", did, owner)
}
func (s sessionStore) CompleteOAuthRefresh(ctx context.Context, session oauthclient.Session, previous, owner, hash string, now int64) error {
	return s.store.CompleteOAuthRefresh(ctx, scheduledata.OAuthSession{DID: session.DID, Handle: session.Handle, TokenJSON: session.TokenJSON, DPoPKeyJSON: session.DPoPKeyJSON}, previous, owner, hash, scheduling.ScheduleTimestamp(time.Now()), now)
}
