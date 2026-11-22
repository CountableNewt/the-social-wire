package scheduledata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

type ManagedAccount struct {
	DID         string  `json:"did"`
	Handle      *string `json:"handle,omitempty"`
	DisplayName *string `json:"displayName,omitempty"`
	Avatar      *string `json:"avatar,omitempty"`
	PDSEndpoint *string `json:"pdsEndpoint,omitempty"`
	Status      string  `json:"status"`
	IsDefault   bool    `json:"isDefault"`
}

type ViewerAccountAccess struct {
	ViewerDID    string   `json:"viewerDid"`
	AccountDID   string   `json:"accountDid"`
	AccessKind   string   `json:"accessKind"`
	TeamURI      *string  `json:"teamUri,omitempty"`
	Capabilities []string `json:"capabilities"`
	IsDefault    bool     `json:"isDefault"`
	CreatedAt    string   `json:"createdAt"`
	UpdatedAt    string   `json:"updatedAt"`
}

func (s *Store) UpsertManagedAccount(ctx context.Context, account ManagedAccount, now string) error {
	if account.Status == "" {
		account.Status = "active"
	}
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		if account.IsDefault {
			if _, err := tx.ExecContext(ctx, "UPDATE managed_accounts SET is_default=0 WHERE did!=? AND is_default=1", account.DID); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO managed_accounts(did,handle,display_name,avatar,pds_endpoint,status,is_default,updated_at)
			VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(did) DO UPDATE SET handle=excluded.handle,display_name=excluded.display_name,
			avatar=excluded.avatar,pds_endpoint=excluded.pds_endpoint,status=excluded.status,
			is_default=MAX(excluded.is_default,managed_accounts.is_default),updated_at=excluded.updated_at`,
			account.DID, account.Handle, account.DisplayName, account.Avatar, account.PDSEndpoint, account.Status, account.IsDefault, now)
		return err
	})
}

const accountColumns = "did,handle,display_name,avatar,pds_endpoint,status,is_default"

func scanAccount(row rowScanner) (ManagedAccount, error) {
	var account ManagedAccount
	err := row.Scan(&account.DID, &account.Handle, &account.DisplayName, &account.Avatar, &account.PDSEndpoint, &account.Status, &account.IsDefault)
	return account, err
}

func (s *Store) ManagedAccount(ctx context.Context, did string) (*ManagedAccount, error) {
	account, err := scanAccount(s.db.Reader().QueryRowContext(ctx, "SELECT "+accountColumns+" FROM managed_accounts WHERE did=?", did))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &account, nil
}

func (s *Store) ListManagedAccounts(ctx context.Context) ([]ManagedAccount, error) {
	rows, err := s.db.Reader().QueryContext(ctx, "SELECT "+accountColumns+" FROM managed_accounts ORDER BY is_default DESC,handle,did")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := []ManagedAccount{}
	for rows.Next() {
		account, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func (s *Store) MarkAccountNeedsReauth(ctx context.Context, did, now string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE managed_accounts SET status='needs_reauth',updated_at=? WHERE did=?", now, did)
	return err
}

func (s *Store) UpsertViewerAccount(ctx context.Context, access ViewerAccountAccess) error {
	if access.Capabilities == nil {
		access.Capabilities = []string{}
	}
	data, err := json.Marshal(access.Capabilities)
	if err != nil {
		return err
	}
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		if access.IsDefault {
			if _, err := tx.ExecContext(ctx, "UPDATE viewer_accounts SET is_default=0 WHERE viewer_did=? AND account_did!=? AND is_default=1",
				access.ViewerDID, access.AccountDID); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO viewer_accounts(viewer_did,account_did,access_kind,team_uri,capabilities_json,is_default,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(viewer_did,account_did) DO UPDATE SET access_kind=excluded.access_kind,
				team_uri=excluded.team_uri,capabilities_json=excluded.capabilities_json,is_default=excluded.is_default,updated_at=excluded.updated_at`,
			access.ViewerDID, access.AccountDID, access.AccessKind, access.TeamURI, string(data), access.IsDefault, access.CreatedAt, access.UpdatedAt)
		return err
	})
}

const viewerAccountColumns = "viewer_did,account_did,access_kind,team_uri,capabilities_json,is_default,created_at,updated_at"

func scanViewerAccount(row rowScanner) (ViewerAccountAccess, error) {
	var access ViewerAccountAccess
	var capabilities string
	err := row.Scan(&access.ViewerDID, &access.AccountDID, &access.AccessKind, &access.TeamURI, &capabilities,
		&access.IsDefault, &access.CreatedAt, &access.UpdatedAt)
	if err == nil && json.Unmarshal([]byte(capabilities), &access.Capabilities) != nil {
		access.Capabilities = nil
	}
	if access.Capabilities == nil {
		access.Capabilities = []string{}
	}
	return access, err
}

func (s *Store) ViewerAccount(ctx context.Context, viewerDID, accountDID string) (*ViewerAccountAccess, error) {
	access, err := scanViewerAccount(s.db.Reader().QueryRowContext(ctx, "SELECT "+viewerAccountColumns+
		" FROM viewer_accounts WHERE viewer_did=? AND account_did=?", viewerDID, accountDID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &access, nil
}

func (s *Store) ListViewerAccounts(ctx context.Context, viewerDID string) ([]ViewerAccountAccess, error) {
	rows, err := s.db.Reader().QueryContext(ctx, "SELECT "+viewerAccountColumns+
		" FROM viewer_accounts WHERE viewer_did=? ORDER BY is_default DESC,account_did", viewerDID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := []ViewerAccountAccess{}
	for rows.Next() {
		account, err := scanViewerAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func (s *Store) DeleteViewerAccount(ctx context.Context, viewerDID, accountDID string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM viewer_accounts WHERE viewer_did=? AND account_did=?", viewerDID, accountDID)
	return err
}
