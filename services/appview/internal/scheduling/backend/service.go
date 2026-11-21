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
	"sync"
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
	importLegacy     bool
	submissionSecret [32]byte
	logger           *slog.Logger
	admissionMu      sync.Mutex
	activeCreates    map[string]bool
	oauthAdmission   chan struct{}
}

// Open validates scheduling configuration and opens the durable Swift-compatible store.
func Open(ctx context.Context, core config.Core, env config.Env, client *http.Client, logger *slog.Logger) (*Service, error) {
	if !env.True("EMBEDDED_SCHEDULING_ENABLED") {
		return nil, nil
	}
	if !filepath.IsAbs(core.SQLitePath) {
		return nil, errors.New("embedded scheduling requires an absolute persistent SQLite path")
	}
	origin := env.String("EMBEDDED_SCHEDULING_PUBLIC_ORIGIN")
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil ||
		parsed.Path != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return nil, errors.New("EMBEDDED_SCHEDULING_PUBLIC_ORIGIN must be an HTTPS origin")
	}
	key := env.String("EMBEDDED_SCHEDULING_STORAGE_KEY")
	decoded, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(decoded) != 32 {
		return nil, errors.New("EMBEDDED_SCHEDULING_STORAGE_KEY must be a Base64-encoded 32-byte key")
	}
	store, err := scheduledata.Open(ctx, filepath.Join(filepath.Dir(core.SQLitePath), "scheduling.sqlite"), key, true)
	if err != nil {
		return nil, err
	}
	oauth := oauthclient.New(origin, client)
	service := &Service{
		origin: origin, core: core, store: store, client: oauth, logger: logger,
		importLegacy:     env.True("EMBEDDED_SCHEDULING_IMPORT_LEGACY"),
		submissionSecret: sha256.Sum256([]byte("schedule-submissions:" + key)),
		activeCreates:    make(map[string]bool), oauthAdmission: make(chan struct{}, 32),
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

func (s *Service) acquireCreate(did string) bool {
	s.admissionMu.Lock()
	defer s.admissionMu.Unlock()
	if s.activeCreates[did] || len(s.activeCreates) >= 32 {
		return false
	}
	s.activeCreates[did] = true
	return true
}

func (s *Service) releaseCreate(did string) {
	s.admissionMu.Lock()
	defer s.admissionMu.Unlock()
	delete(s.activeCreates, did)
}

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

func (s sessionStore) ReplaceOAuthSessionAfterRefresh(ctx context.Context, session oauthclient.Session, previous string) error {
	return s.store.ReplaceOAuthSessionAfterRefresh(ctx, scheduledata.OAuthSession{
		DID: session.DID, Handle: session.Handle, TokenJSON: session.TokenJSON, DPoPKeyJSON: session.DPoPKeyJSON,
	}, previous, scheduling.ScheduleTimestamp(time.Now()))
}
