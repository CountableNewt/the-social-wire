// Package oauthclient implements the scheduling backend's outbound ATProto OAuth client.
package oauthclient

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"golang.org/x/sync/singleflight"
)

// Scope is the published scheduling client permission set in metadata order.
const Scope = "atproto repo:at.skej.schedule?action=create&action=update repo:community.lexicon.calendar.event?action=create&action=update repo:app.bsky.feed.post?action=create repo:site.standard.document?action=create blob:image/*"

var (
	ErrIdentity            = errors.New("could not resolve the scheduling account")
	ErrAuthorizationServer = errors.New("OAuth server does not support scheduling authorization")
	ErrState               = errors.New("OAuth state is missing or contains invalid authorization metadata")
	ErrToken               = errors.New("OAuth did not grant the requested scheduling account and permissions")
	ErrNotConfigured       = errors.New("scheduling OAuth session is not configured")
)
var plcDID = regexp.MustCompile(`^did:plc:[a-z2-7]{24}$`)
var webDIDPart = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// Client discovers authorization servers and makes OAuth and authenticated PDS requests.
type Client struct {
	origin  string
	http    *http.Client
	refresh singleflight.Group
	mu      sync.Mutex
	nonces  map[string]nonceEntry
}

// New configures a client. The shared HTTP transport must validate public DNS
// for outbound requests. Redirects are disabled even if the supplied client allows them.
func New(publicOrigin string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Transport: publicTransport{}}
	}
	client := *httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{origin: strings.TrimRight(strings.TrimSpace(publicOrigin), "/"), http: &client, nonces: make(map[string]nonceEntry)}
}

func (c *Client) ClientID() string    { return c.origin + "/oauth/scheduling/client-metadata.json" }
func (c *Client) RedirectURI() string { return c.origin + "/oauth/scheduling/callback" }

// StartResult contains the authorization redirect and server metadata persisted with the state.
type StartResult struct {
	RedirectURL   string
	AuthServer    string
	TokenEndpoint string
	PDSEndpoint   string
	DPoPKeyJSON   string
}

// State carries the persisted OAuth start fields after the callback claims its state.
type State struct {
	Handle        string
	InitiatorDID  string
	PKCEVerifier  string
	AuthServer    string
	TokenEndpoint string
	PDSEndpoint   string
	DPoPKeyJSON   string
}

// Session is the database-facing OAuth session. TokenJSON and DPoPKeyJSON
// use the field names and P-256 key representation written by Swift.
type Session struct {
	DID         string
	Handle      *string
	TokenJSON   string
	DPoPKeyJSON string
}

type tokenPayload struct {
	AccessToken   string `json:"accessToken"`
	RefreshToken  string `json:"refreshToken,omitempty"`
	Type          string `json:"tokenType"`
	ExpiresIn     *int   `json:"expiresIn,omitempty"`
	Scope         string `json:"scope"`
	Subject       string `json:"sub"`
	PDSEndpoint   string `json:"pdsEndpoint"`
	TokenEndpoint string `json:"tokenEndpoint"`
}

type tokenReply struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Type         string `json:"token_type"`
	ExpiresIn    *int   `json:"expires_in"`
	Scope        string `json:"scope"`
	Subject      string `json:"sub"`
}

type authorizationMetadata struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	PAREndpoint           string   `json:"pushed_authorization_request_endpoint"`
	SupportedScopes       []string `json:"scopes_supported"`
}

// Start binds a PAR request to the selected account's DID and stored proof key.
func (c *Client) Start(ctx context.Context, did, state, pkceVerifier string) (StartResult, error) {
	if state == "" || pkceVerifier == "" {
		return StartResult{}, ErrState
	}
	pds, err := c.resolvePDS(ctx, did)
	if err != nil {
		return StartResult{}, err
	}
	issuer, server, err := c.discover(ctx, pds)
	if err != nil {
		return StartResult{}, err
	}
	private, err := generateKey()
	if err != nil {
		return StartResult{}, err
	}
	form := url.Values{
		"client_id": {c.ClientID()}, "redirect_uri": {c.RedirectURI()}, "response_type": {"code"}, "scope": {Scope},
		"state": {state}, "code_challenge": {sha256URL(pkceVerifier)}, "code_challenge_method": {"S256"}, "login_hint": {did},
	}
	target, _ := endpoint(server.PAREndpoint)
	response, err := c.signedWithNonceRetry(ctx, http.MethodPost, target, []byte(form.Encode()), "application/x-www-form-urlencoded", private, "", "")
	if err != nil {
		return StartResult{}, err
	}
	var par struct {
		RequestURI string `json:"request_uri"`
	}
	if json.Unmarshal(response.body, &par) != nil || par.RequestURI == "" || len(par.RequestURI) > 2048 {
		return StartResult{}, ErrAuthorizationServer
	}
	redirect, _ := endpoint(server.AuthorizationEndpoint)
	query := redirect.Query()
	query.Set("client_id", c.ClientID())
	query.Set("request_uri", par.RequestURI)
	redirect.RawQuery = query.Encode()
	keyJSON, err := encodeKey(private)
	if err != nil {
		return StartResult{}, err
	}
	return StartResult{RedirectURL: redirect.String(), AuthServer: issuer, TokenEndpoint: server.TokenEndpoint, PDSEndpoint: pds, DPoPKeyJSON: keyJSON}, nil
}

// Complete exchanges a code after the caller consumes state and validates the callback issuer.
func (c *Client) Complete(ctx context.Context, state State, code string) (Session, error) {
	if state.InitiatorDID == "" || state.PKCEVerifier == "" || code == "" || len(code) > 2048 {
		return Session{}, ErrState
	}
	if _, ok := publicOrigin(state.AuthServer); !ok {
		return Session{}, ErrState
	}
	if _, ok := publicOrigin(state.PDSEndpoint); !ok {
		return Session{}, ErrState
	}
	target, ok := endpoint(state.TokenEndpoint)
	if !ok {
		return Session{}, ErrState
	}
	key, err := decodeKey(state.DPoPKeyJSON)
	if err != nil {
		return Session{}, ErrState
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {c.RedirectURI()},
		"client_id": {c.ClientID()}, "code_verifier": {state.PKCEVerifier}}
	reply, err := c.requestToken(ctx, target, key, form)
	if err != nil {
		return Session{}, err
	}
	if !validToken(reply, state.InitiatorDID) || reply.RefreshToken == "" {
		return Session{}, ErrToken
	}
	handle := state.Handle
	if handle == "" {
		handle = state.InitiatorDID
	}
	return encodeSession(Session{DID: state.InitiatorDID, Handle: &handle, DPoPKeyJSON: state.DPoPKeyJSON}, reply, state.PDSEndpoint, state.TokenEndpoint)
}

func (c *Client) requestToken(ctx context.Context, target *url.URL, key *ecdsa.PrivateKey, form url.Values) (tokenReply, error) {
	var reply tokenReply
	response, err := c.signedWithNonceRetry(ctx, http.MethodPost, target, []byte(form.Encode()), "application/x-www-form-urlencoded", key, "", "")
	if err != nil {
		return reply, err
	}
	if json.Unmarshal(response.body, &reply) != nil {
		return reply, ErrToken
	}
	return reply, nil
}

func validToken(token tokenReply, did string) bool {
	return validActorToken(token, did) && hasScopes(token.Scope)
}

func validActorToken(token tokenReply, did string) bool {
	return token.Subject == did && token.AccessToken != "" && len(token.AccessToken) <= 32<<10 &&
		!strings.ContainsAny(token.AccessToken, " \r\n\t") && strings.EqualFold(token.Type, "dpop")
}

func hasScopes(scope string) bool {
	has := make(map[string]bool)
	for _, value := range strings.Fields(scope) {
		has[value] = true
	}
	for _, scope := range strings.Fields(Scope) {
		if !has[scope] {
			return false
		}
	}
	return true
}

func encodeSession(session Session, reply tokenReply, pds, token string) (Session, error) {
	payload := tokenPayload{AccessToken: reply.AccessToken, RefreshToken: reply.RefreshToken, Type: reply.Type, ExpiresIn: reply.ExpiresIn,
		Scope: reply.Scope, Subject: reply.Subject, PDSEndpoint: pds, TokenEndpoint: token}
	data, err := json.Marshal(payload)
	if err != nil {
		return Session{}, err
	}
	session.TokenJSON = string(data)
	return session, nil
}

func publicOrigin(raw string) (string, bool) {
	return atproto.ValidatedPDSBase(raw, atproto.EndpointPublicHTTPS)
}
