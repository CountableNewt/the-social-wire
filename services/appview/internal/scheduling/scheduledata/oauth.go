package scheduledata

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type OAuthState struct {
	State, Handle, PKCEVerifier, Nonce                  string
	AuthServer, TokenEndpoint, PDSEndpoint, DPoPKeyJSON *string
	Purpose                                             string
	InitiatorDID, InviteToken, ReturnTo                 *string
	ExpiresAt                                           string
}

type OAuthSession struct {
	DID                    string
	Handle                 *string
	TokenJSON, DPoPKeyJSON string
}

type Viewer struct {
	DID               string  `json:"did"`
	Handle            *string `json:"handle,omitempty"`
	DisplayName       *string `json:"displayName,omitempty"`
	Avatar            *string `json:"avatar,omitempty"`
	DefaultAccountDID string  `json:"defaultAccountDid,omitempty"`
}

func (s *Store) CreateOAuthState(ctx context.Context, state OAuthState) error {
	verifier, err := s.seal(state.PKCEVerifier)
	if err != nil {
		return err
	}
	var dpop *string
	if state.DPoPKeyJSON != nil {
		key, err := s.seal(*state.DPoPKeyJSON)
		if err != nil {
			return err
		}
		dpop = &key
	}
	if state.Purpose == "" {
		state.Purpose = "sign_in"
	}
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM oauth_states WHERE julianday(expires_at)<=julianday(?)", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO oauth_states
			(state,handle,pkce_verifier,nonce,auth_server,token_endpoint,pds_endpoint,dpop_key_json,
			purpose,initiator_did,invite_token,return_to,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			state.State, state.Handle, verifier, state.Nonce, state.AuthServer, state.TokenEndpoint, state.PDSEndpoint, dpop,
			state.Purpose, state.InitiatorDID, state.InviteToken, state.ReturnTo, state.ExpiresAt)
		return err
	})
}

func (s *Store) ConsumeOAuthState(ctx context.Context, state, now string) (*OAuthState, error) {
	var value OAuthState
	var found bool
	err := s.db.Write(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT state,handle,pkce_verifier,nonce,auth_server,token_endpoint,pds_endpoint,
			dpop_key_json,purpose,initiator_did,invite_token,return_to,expires_at FROM oauth_states
			WHERE state=? AND julianday(expires_at)>julianday(?)`, state, now).
			Scan(&value.State, &value.Handle, &value.PKCEVerifier, &value.Nonce, &value.AuthServer, &value.TokenEndpoint, &value.PDSEndpoint,
				&value.DPoPKeyJSON, &value.Purpose, &value.InitiatorDID, &value.InviteToken, &value.ReturnTo, &value.ExpiresAt)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		found = err == nil
		_, err = tx.ExecContext(ctx, "DELETE FROM oauth_states WHERE state=?", state)
		return err
	})
	if err != nil || !found {
		return nil, err
	}
	value.PKCEVerifier, err = s.open(value.PKCEVerifier)
	if err != nil {
		return nil, err
	}
	if value.DPoPKeyJSON != nil {
		dpop, err := s.open(*value.DPoPKeyJSON)
		if err != nil {
			return nil, err
		}
		value.DPoPKeyJSON = &dpop
	}
	switch value.Purpose {
	case "sign_in", "connect_account", "brand_connection", "invite_accept":
	default:
		value.Purpose = "sign_in"
	}
	return &value, nil
}

func (s *Store) CreateWebSession(ctx context.Context, sessionID string, viewer Viewer, expiresAt string) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO web_sessions
		(session_id,did,handle,display_name,avatar,expires_at) VALUES(?,?,?,?,?,?)`,
		sessionID, viewer.DID, viewer.Handle, viewer.DisplayName, viewer.Avatar, expiresAt)
	return err
}

func (s *Store) Viewer(ctx context.Context, sessionID, now string) (*Viewer, error) {
	var value Viewer
	err := s.db.Reader().QueryRowContext(ctx, `SELECT did,handle,display_name,avatar FROM web_sessions
		WHERE session_id=? AND julianday(expires_at)>julianday(?)`, sessionID, now).
		Scan(&value.DID, &value.Handle, &value.DisplayName, &value.Avatar)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	value.DefaultAccountDID = value.DID
	return &value, nil
}

func (s *Store) DeleteWebSession(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM web_sessions WHERE session_id=?", sessionID)
	return err
}

type sessionQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type storeExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (s *Store) CreateOAuthSession(ctx context.Context, session OAuthSession, now string) error {
	return s.writeSession(ctx, s.db, session, now)
}

func (s *Store) writeSession(ctx context.Context, db storeExecer, session OAuthSession, now string) error {
	token, err := s.seal(session.TokenJSON)
	if err != nil {
		return err
	}
	key, err := s.seal(session.DPoPKeyJSON)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT OR REPLACE INTO oauth_sessions(did,handle,token_json,dpop_key_json,updated_at)
		VALUES(?,?,?,?,?)`, session.DID, session.Handle, token, key, now)
	return err
}

func (s *Store) OAuthSession(ctx context.Context, did string) (*OAuthSession, error) {
	return s.readSession(ctx, s.db.Reader(), did)
}

func (s *Store) readSession(ctx context.Context, db sessionQuerier, did string) (*OAuthSession, error) {
	var value OAuthSession
	err := db.QueryRowContext(ctx, "SELECT did,handle,token_json,dpop_key_json FROM oauth_sessions WHERE did=?", did).
		Scan(&value.DID, &value.Handle, &value.TokenJSON, &value.DPoPKeyJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	value.TokenJSON, err = s.open(value.TokenJSON)
	if err != nil {
		return nil, err
	}
	value.DPoPKeyJSON, err = s.open(value.DPoPKeyJSON)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func (s *Store) ClaimServiceAuth(ctx context.Context, issuer, jti string, expiresAt, now int64) (bool, error) {
	var success bool
	err := s.db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM service_auth_replays WHERE expires_at<=?", now); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO service_auth_replays(issuer,jti,expires_at) VALUES(?,?,?)", issuer, jti, expiresAt)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		success = n == 1
		return err
	})
	return success, err
}
