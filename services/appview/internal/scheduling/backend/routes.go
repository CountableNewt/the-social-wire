package backend

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling/oauthclient"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling/scheduledata"
)

func (s *Service) Register(router *httpx.Router, protected *httpx.Group) {
	router.Get("/oauth/scheduling/client-metadata.json", s.metadata)
	router.Get("/oauth/scheduling/start", s.start)
	router.Get("/oauth/scheduling/callback", s.callback)
	protected.Get("/xrpc/at.skej.actor.getSession", s.getSession)
	protected.Get("/xrpc/at.skej.schedule.list", s.list)
	protected.Post("/xrpc/at.skej.schedule.create", s.save("create"))
	protected.Post("/xrpc/at.skej.schedule.update", s.save("update"))
	protected.Post("/xrpc/at.skej.schedule.cancel", s.save("cancel"))
}

func (s *Service) metadata(w http.ResponseWriter, _ *http.Request) error {
	return httpx.OK(w, map[string]any{
		"client_id": s.client.ClientID(), "client_name": "The Social Wire Scheduling",
		"client_uri": s.origin, "application_type": "web",
		"grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"},
		"redirect_uris": []string{s.client.RedirectURI()}, "scope": oauthclient.Scope,
		"token_endpoint_auth_method": "none", "dpop_bound_access_tokens": true,
	})
}

func (s *Service) canConnect(did string) bool {
	return !s.core.ProGatingEnabled || s.core.HasPro(did)
}

func (s *Service) start(w http.ResponseWriter, r *http.Request) error {
	setOAuthHeaders(w)
	query, err := schedulingQuery(r, "accountDid")
	if err != nil {
		return oauthFailure(w, http.StatusBadRequest, "InvalidRequest", "Invalid scheduling query parameters")
	}
	did := query.Get("accountDid")
	if !validAccountDID(did) {
		return oauthFailure(w, http.StatusBadRequest, "InvalidActor", "An account DID is required")
	}
	if !s.canConnect(did) {
		return oauthFailure(w, http.StatusForbidden, "ProRequired", "A Pro subscription is required")
	}
	select {
	case s.oauthAdmission <- struct{}{}:
		defer func() { <-s.oauthAdmission }()
	default:
		return oauthFailure(w, http.StatusTooManyRequests, "RateLimitExceeded", "Too many scheduling connection requests")
	}
	state, verifier := randomToken(), randomToken()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	start, err := s.client.Start(ctx, did, state, verifier)
	if err != nil {
		return oauthFailure(w, http.StatusBadGateway, "ConnectionUnavailable", "Scheduling authorization could not be started")
	}
	err = s.store.CreateOAuthState(ctx, scheduledata.OAuthState{
		State: state, Handle: did, PKCEVerifier: verifier, Nonce: randomToken(), AuthServer: &start.AuthServer,
		TokenEndpoint: &start.TokenEndpoint, PDSEndpoint: &start.PDSEndpoint, DPoPKeyJSON: &start.DPoPKeyJSON,
		InitiatorDID: &did, ExpiresAt: scheduling.ScheduleTimestamp(time.Now().Add(10 * time.Minute)),
	})
	if err != nil {
		return err
	}
	w.Header().Set("Location", start.RedirectURL)
	w.WriteHeader(http.StatusFound)
	return nil
}

func (s *Service) callback(w http.ResponseWriter, r *http.Request) error {
	setOAuthHeaders(w)
	query := r.URL.Query()
	state, code := query.Get("state"), query.Get("code")
	if len(query["state"]) != 1 || len(query["code"]) != 1 || len(query["iss"]) != 1 ||
		state == "" || code == "" || len(state) > 128 || len(code) > 2048 {
		return invalidState(w)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	pending, err := s.store.ConsumeOAuthState(ctx, state, scheduling.ScheduleTimestamp(time.Now()))
	if err != nil {
		return err
	}
	if pending == nil || pending.InitiatorDID == nil || pending.AuthServer == nil ||
		pending.TokenEndpoint == nil || pending.PDSEndpoint == nil || pending.DPoPKeyJSON == nil ||
		query.Get("iss") != *pending.AuthServer {
		return invalidState(w)
	}
	did := *pending.InitiatorDID
	if !s.canConnect(did) {
		return oauthFailure(w, http.StatusForbidden, "ProRequired", "A Pro subscription is required")
	}
	session, err := s.client.Complete(ctx, oauthclient.State{
		Handle: pending.Handle, InitiatorDID: did, PKCEVerifier: pending.PKCEVerifier, AuthServer: *pending.AuthServer,
		TokenEndpoint: *pending.TokenEndpoint, PDSEndpoint: *pending.PDSEndpoint, DPoPKeyJSON: *pending.DPoPKeyJSON,
	}, code)
	if err != nil {
		return oauthFailure(w, http.StatusBadGateway, "ConnectionUnavailable", "Scheduling authorization could not be completed")
	}
	if session.DID != did {
		return oauthFailure(w, http.StatusForbidden, "ActorMismatch", "Authorize the selected account")
	}
	stamp := scheduling.ScheduleTimestamp(time.Now())
	if err := s.store.CreateOAuthSession(ctx, scheduledata.OAuthSession{
		DID: did, Handle: session.Handle, TokenJSON: session.TokenJSON, DPoPKeyJSON: session.DPoPKeyJSON,
	}, stamp); err != nil {
		return err
	}
	if err := s.store.UpsertManagedAccount(ctx, scheduledata.ManagedAccount{
		DID: did, Handle: session.Handle, PDSEndpoint: pending.PDSEndpoint, Status: "active", IsDefault: true,
	}, stamp); err != nil {
		return err
	}
	if _, err := s.loadSchedules(ctx, did); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'")
	_, err = io.WriteString(w, connectedPage)
	return err
}

func setOAuthHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

func invalidState(w http.ResponseWriter) error {
	return oauthFailure(w, http.StatusBadRequest, "InvalidOAuthState", "Scheduling authorization has expired")
}

func oauthFailure(w http.ResponseWriter, status int, code, message string) error {
	return httpx.JSON(w, status, map[string]string{"error": code, "message": message})
}

const connectedPage = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex"><title>Scheduling Connected</title><style>:root{color-scheme:light dark}body{font:1rem/1.6 system-ui,sans-serif;max-width:36rem;margin:12vh auto;padding:1.5rem}h1{font-size:1.5rem}</style></head><body><main><h1>Scheduling Connected</h1><p>Return to Social Wire and select Check Connection.</p><p>You can close this tab.</p></main></body></html>`

func (s *Service) getSession(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	w.Header().Set("Cache-Control", "private, no-store")
	if _, err := schedulingQuery(r); err != nil {
		return err
	}
	did, err := actor(ctx, nil)
	if err != nil {
		return err
	}
	account, err := s.connected(ctx, did)
	if err != nil {
		return err
	}
	if _, err := s.loadSchedules(ctx, did); err != nil {
		return safeScheduleError(err)
	}
	return httpx.OK(w, map[string]any{"did": did, "handle": account.Handle, "displayName": account.DisplayName, "avatar": account.Avatar})
}

func (s *Service) list(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	w.Header().Set("Cache-Control", "private, no-store")
	query, err := schedulingQuery(r, "accountDid")
	if err != nil {
		return err
	}
	did, err := actor(ctx, queryAccount(query))
	if err != nil {
		return err
	}
	if _, err := s.connected(ctx, did); err != nil {
		return err
	}
	items, err := s.loadSchedules(ctx, did)
	if err != nil {
		return safeScheduleError(err)
	}
	return httpx.OK(w, map[string]any{"records": items})
}

type scheduleInput struct {
	AccountDID *string           `json:"accountDid"`
	RKey       string            `json:"rkey"`
	Record     scheduling.Record `json:"record"`
}

func (s *Service) save(action string) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		w.Header().Set("Cache-Control", "private, no-store")
		if _, err := schedulingQuery(r); err != nil {
			return err
		}
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			return httpx.Named(http.StatusUnsupportedMediaType, "InvalidRequest", "Expected a JSON body")
		}
		var input scheduleInput
		if err := httpx.DecodeJSON(r, &input, 1<<20); err != nil {
			return err
		}
		if action != "cancel" && input.Record == nil {
			return httpx.Named(http.StatusBadRequest, "InvalidRequest", "Expected a schedule record")
		}
		did, err := actor(ctx, input.AccountDID)
		if err != nil {
			return err
		}
		if _, err := s.connected(ctx, did); err != nil {
			return err
		}
		var result summary
		status := http.StatusOK
		switch action {
		case "create":
			result, err = s.create(ctx, did, input.Record)
			status = http.StatusCreated
		case "update":
			result, err = s.edit(ctx, did, input.RKey, input.Record)
		case "cancel":
			result, err = s.edit(ctx, did, input.RKey, nil)
		}
		if err != nil {
			return safeScheduleError(err)
		}
		return httpx.JSON(w, status, result)
	}
}

func schedulingQuery(r *http.Request, allowed ...string) (url.Values, error) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err == nil {
		for key, values := range query {
			if !contains(allowed, key) || len(values) != 1 {
				err = errors.New("invalid query")
				break
			}
		}
	}
	if err != nil {
		return nil, httpx.Named(http.StatusBadRequest, "InvalidRequest", "Invalid scheduling query parameters")
	}
	return query, nil
}

func contains(values []string, key string) bool {
	for _, v := range values {
		if v == key {
			return true
		}
	}
	return false
}

func queryAccount(query url.Values) *string {
	if query.Has("accountDid") {
		v := query.Get("accountDid")
		return &v
	}
	return nil
}

func actor(ctx context.Context, requested *string) (string, error) {
	did := reqctx.ViewerDID(ctx)
	if did == "" {
		return "", httpx.ErrUnauthorized
	}
	if requested != nil && *requested != did {
		return "", httpx.Named(http.StatusForbidden, "ActorMismatch", "Use the authenticated account for scheduling")
	}
	return did, nil
}

func (s *Service) connected(ctx context.Context, did string) (*scheduledata.ManagedAccount, error) {
	reauth := httpx.Named(http.StatusConflict, "AccountNeedsReauth", "Connect this account to scheduling")
	account, err := s.store.ManagedAccount(ctx, did)
	if err != nil {
		return nil, err
	}
	if account == nil || account.Status != "active" {
		return nil, reauth
	}
	if err := s.pds.connection(ctx, did); err != nil {
		if oauthclient.IsAuthFailure(err) {
			return nil, reauth
		}
		return nil, safeScheduleError(err)
	}
	return account, nil
}

func safeScheduleError(err error) error {
	var apiError *httpx.Error
	if errors.As(err, &apiError) {
		return err
	}
	var downstream *oauthclient.XRPCError
	if errors.As(err, &downstream) {
		switch downstream.Code {
		case "InvalidSwap":
			return httpx.Named(http.StatusConflict, "ScheduleChanged", "The schedule changed. Reload and try again.")
		case "RecordNotFound":
			return httpx.Named(http.StatusNotFound, "ScheduleNotFound", "Schedule not found")
		}
	}
	return httpx.Named(http.StatusBadGateway, "SchedulingUnavailable", "The scheduling request could not be completed")
}

func validAccountDID(did string) bool {
	if len(did) > 512 {
		return false
	}
	if plc, ok := strings.CutPrefix(did, "did:plc:"); ok {
		return len(plc) == 24 && !strings.ContainsFunc(plc, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= '2' && r <= '7')
		})
	}
	web, ok := strings.CutPrefix(did, "did:web:")
	return ok && web != "" && !strings.ContainsFunc(web, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune(".%:_-", r))
	})
}
