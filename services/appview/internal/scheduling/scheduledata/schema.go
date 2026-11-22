package scheduledata

const scheduleSchema = `
CREATE TABLE IF NOT EXISTS scheduler_actor_leases (
    purpose TEXT NOT NULL, actor_did TEXT NOT NULL, owner TEXT NOT NULL, expires_at_us INTEGER NOT NULL,
    PRIMARY KEY (purpose,actor_did)
);
CREATE INDEX IF NOT EXISTS scheduler_actor_leases_expiry ON scheduler_actor_leases(expires_at_us);
CREATE TABLE IF NOT EXISTS oauth_refresh_leases (
    did TEXT NOT NULL, token_hash TEXT NOT NULL, owner TEXT NOT NULL,
    expires_at_us INTEGER NOT NULL, request_started INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(did,token_hash)
);
CREATE TABLE IF NOT EXISTS pending_schedule_submissions (
    did TEXT NOT NULL, rkey TEXT NOT NULL, next_check_at TEXT NOT NULL, created_at TEXT NOT NULL,
    PRIMARY KEY (did, rkey)
);
CREATE TABLE IF NOT EXISTS imported_schedule_queues (
    did TEXT PRIMARY KEY
);
CREATE TABLE IF NOT EXISTS schedule_edit_leases (
    did TEXT NOT NULL, rkey TEXT NOT NULL, lease_id TEXT NOT NULL, expires_at TEXT NOT NULL,
    PRIMARY KEY (did, rkey)
);
CREATE TABLE IF NOT EXISTS service_auth_replays (
    issuer TEXT NOT NULL,
    jti TEXT NOT NULL,
    expires_at INTEGER NOT NULL,
    PRIMARY KEY (issuer, jti)
);
CREATE INDEX IF NOT EXISTS service_auth_replays_expiry_idx ON service_auth_replays (expires_at);
CREATE TABLE IF NOT EXISTS oauth_states (
    state TEXT PRIMARY KEY,
    handle TEXT NOT NULL,
    pkce_verifier TEXT NOT NULL,
    nonce TEXT NOT NULL,
    auth_server TEXT,
    token_endpoint TEXT,
    pds_endpoint TEXT,
    dpop_key_json TEXT,
    purpose TEXT NOT NULL DEFAULT 'sign_in',
    initiator_did TEXT,
    invite_token TEXT,
    return_to TEXT,
    expires_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS oauth_sessions (
    did TEXT PRIMARY KEY,
    handle TEXT,
    token_json TEXT NOT NULL,
    dpop_key_json TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS web_sessions (
    session_id TEXT PRIMARY KEY,
    did TEXT NOT NULL,
    handle TEXT,
    display_name TEXT,
    avatar TEXT,
    expires_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS scheduled_jobs (
    did TEXT NOT NULL,
    rkey TEXT NOT NULL,
    scheduled_for TEXT NOT NULL,
    scheduled_at TEXT,
    status TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    locked_until TEXT,
    last_error TEXT,
    next_attempt_at TEXT,
    last_attempt_at TEXT,
    publish_rkey TEXT,
    record_type TEXT,
    published_uri TEXT,
    published_cid TEXT,
    depends_on_schedule_uri TEXT,
    parent_published_uri TEXT,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (did, rkey)
);
CREATE TABLE IF NOT EXISTS pds_schedule_records (
    did TEXT NOT NULL,
    rkey TEXT NOT NULL,
    record_json TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (did, rkey)
);
CREATE TABLE IF NOT EXISTS pds_protocol_records (
    did TEXT NOT NULL,
    collection TEXT NOT NULL,
    rkey TEXT NOT NULL,
    record_json TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (did, collection, rkey)
);
CREATE TABLE IF NOT EXISTS managed_accounts (
    did TEXT PRIMARY KEY,
    handle TEXT,
    display_name TEXT,
    avatar TEXT,
    pds_endpoint TEXT,
    status TEXT NOT NULL,
    is_default INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS viewer_accounts (
    viewer_did TEXT NOT NULL,
    account_did TEXT NOT NULL,
    access_kind TEXT NOT NULL,
    team_uri TEXT,
    capabilities_json TEXT NOT NULL DEFAULT '[]',
    is_default INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (viewer_did, account_did)
);
CREATE INDEX IF NOT EXISTS viewer_accounts_viewer_idx
    ON viewer_accounts (viewer_did, is_default DESC, account_did);
CREATE TABLE IF NOT EXISTS team_invites (
    id TEXT PRIMARY KEY,
    token TEXT NOT NULL UNIQUE,
    team_uri TEXT NOT NULL,
    team_title TEXT NOT NULL,
    owner_did TEXT NOT NULL,
    invited_handle TEXT,
    invited_did TEXT,
    role TEXT NOT NULL,
    status TEXT NOT NULL,
    inviter_did TEXT NOT NULL,
    accepted_did TEXT,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS team_invites_owner_idx
    ON team_invites (owner_did, team_uri, status, created_at DESC);
CREATE TABLE IF NOT EXISTS audit_events (
    id TEXT PRIMARY KEY,
    did TEXT NOT NULL,
    schedule_rkey TEXT,
    action TEXT NOT NULL,
    message TEXT NOT NULL,
    created_at TEXT NOT NULL
);
`
