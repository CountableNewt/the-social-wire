#if canImport(SQLite3)
@preconcurrency import SQLite3
#elseif canImport(CSQLite)
@preconcurrency import CSQLite
#endif
@preconcurrency import Crypto
import Foundation

private let sqliteTransient = unsafeBitCast(-1, to: sqlite3_destructor_type.self)

public actor SQLiteStore {
    private var db: OpaquePointer?
    private let authKey: SymmetricKey?
    private let encoder = JSONEncoder()
    private let decoder = JSONDecoder()

    public init(path: String, authEncryptionSecret: String? = ProcessInfo.processInfo.environment["SKEJ_AUTH_ENCRYPTION_KEY"], requiresAuthEncryption: Bool = false) throws {
        let key = authEncryptionSecret.flatMap { $0.isEmpty ? nil : SymmetricKey(data: Data(SHA256.hash(data: Data($0.utf8)))) }
        if requiresAuthEncryption && key == nil {
            throw SQLiteStoreError.statement(message: "SKEJ_AUTH_ENCRYPTION_KEY is required for live OAuth storage")
        }
        authKey = key
        if path != ":memory:" {
            let url = URL(fileURLWithPath: path)
            try FileManager.default.createDirectory(
                at: url.deletingLastPathComponent(),
                withIntermediateDirectories: true
            )
        }
        guard sqlite3_open(path, &db) == SQLITE_OK else {
            throw SQLiteStoreError.open(message: "Could not open SQLite database")
        }
    }

    public func migrate() throws {
        try exec("PRAGMA foreign_keys = ON")
        try exec("PRAGMA journal_mode = WAL")
        try exec("PRAGMA secure_delete = ON")
        try exec(
            """
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
            CREATE INDEX IF NOT EXISTS scheduled_jobs_due_idx
                ON scheduled_jobs (status, scheduled_at, scheduled_for);
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
            CREATE TABLE IF NOT EXISTS pro_entitlements (
                viewer_did TEXT PRIMARY KEY,
                scope_type TEXT NOT NULL DEFAULT 'actor',
                scope_value TEXT,
                status TEXT NOT NULL,
                source TEXT NOT NULL,
                granted_by_did TEXT,
                expires_at TEXT,
                created_at TEXT NOT NULL,
                updated_at TEXT NOT NULL
            );
            CREATE TABLE IF NOT EXISTS audit_events (
                id TEXT PRIMARY KEY,
                did TEXT NOT NULL,
                schedule_rkey TEXT,
                action TEXT NOT NULL,
                message TEXT NOT NULL,
                created_at TEXT NOT NULL
            );
            """
        )
        try addColumnIfMissing(table: "oauth_states", column: "auth_server", definition: "TEXT")
        try addColumnIfMissing(table: "oauth_states", column: "token_endpoint", definition: "TEXT")
        try addColumnIfMissing(table: "oauth_states", column: "pds_endpoint", definition: "TEXT")
        try addColumnIfMissing(table: "oauth_states", column: "dpop_key_json", definition: "TEXT")
        try addColumnIfMissing(table: "oauth_states", column: "purpose", definition: "TEXT NOT NULL DEFAULT 'sign_in'")
        try addColumnIfMissing(table: "oauth_states", column: "initiator_did", definition: "TEXT")
        try addColumnIfMissing(table: "oauth_states", column: "invite_token", definition: "TEXT")
        try addColumnIfMissing(table: "oauth_states", column: "return_to", definition: "TEXT")
        try addColumnIfMissing(table: "pro_entitlements", column: "scope_type", definition: "TEXT NOT NULL DEFAULT 'actor'")
        try addColumnIfMissing(table: "pro_entitlements", column: "scope_value", definition: "TEXT")
        try addColumnIfMissing(table: "web_sessions", column: "display_name", definition: "TEXT")
        try addColumnIfMissing(table: "web_sessions", column: "avatar", definition: "TEXT")
        try addColumnIfMissing(table: "scheduled_jobs", column: "scheduled_at", definition: "TEXT")
        try addColumnIfMissing(table: "scheduled_jobs", column: "next_attempt_at", definition: "TEXT")
        try addColumnIfMissing(table: "scheduled_jobs", column: "last_attempt_at", definition: "TEXT")
        try addColumnIfMissing(table: "scheduled_jobs", column: "publish_rkey", definition: "TEXT")
        try addColumnIfMissing(table: "scheduled_jobs", column: "record_type", definition: "TEXT")
        try addColumnIfMissing(table: "scheduled_jobs", column: "depends_on_schedule_uri", definition: "TEXT")
        try addColumnIfMissing(table: "scheduled_jobs", column: "parent_published_uri", definition: "TEXT")
        try exec(
            """
            INSERT OR IGNORE INTO viewer_accounts
                (viewer_did, account_did, access_kind, team_uri, capabilities_json, is_default, created_at, updated_at)
            SELECT did, did, 'owner', NULL, '["create","approve","manage","viewAnalytics"]', 1,
                   updated_at, updated_at
            FROM oauth_sessions;
            INSERT OR IGNORE INTO viewer_accounts
                (viewer_did, account_did, access_kind, team_uri, capabilities_json, is_default, created_at, updated_at)
            SELECT did, did, 'owner', NULL, '["create","approve","manage","viewAnalytics"]', 1,
                   expires_at, expires_at
            FROM web_sessions;
            """
        )
        try migrateAuthVault()
    }

    /// Startup-only upgrade: validate every ciphertext before changing any row,
    /// then atomically encrypt legacy secrets and scrub freed database/WAL pages.
    private func migrateAuthVault() throws {
        try exec("CREATE TABLE IF NOT EXISTS auth_vault_cleanup (id INTEGER PRIMARY KEY CHECK(id = 1), pending INTEGER NOT NULL)")
        var changed = false
        try exec("BEGIN IMMEDIATE")
        do {
            // Hold the write lease through reading/validation so another process
            // cannot refresh a grant between the snapshot and its encrypted write.
            let sessions = try query("SELECT did, token_json, dpop_key_json FROM oauth_sessions", [])
            let states = try query("SELECT state, pkce_verifier, dpop_key_json FROM oauth_states", [])
            for row in sessions + states {
                for field in ["token_json", "dpop_key_json", "pkce_verifier"] {
                    if let value = row[field], value.hasPrefix("aesgcm:v1:") { _ = try decryptAuthMaterial(value) }
                }
            }
            guard authKey != nil else { try exec("COMMIT"); return }
            for row in sessions {
                guard let did = row["did"], let token = row["token_json"], let key = row["dpop_key_json"] else { continue }
                if !token.hasPrefix("aesgcm:v1:") || !key.hasPrefix("aesgcm:v1:") {
                    try run("UPDATE oauth_sessions SET token_json = ?, dpop_key_json = ? WHERE did = ?", [try encryptIfNeeded(token), try encryptIfNeeded(key), did])
                    changed = true
                }
            }
            for row in states {
                guard let state = row["state"], let verifier = row["pkce_verifier"] else { continue }
                let key = row["dpop_key_json"]
                if !verifier.hasPrefix("aesgcm:v1:") || key.map({ !$0.hasPrefix("aesgcm:v1:") }) == true {
                    try run("UPDATE oauth_states SET pkce_verifier = ?, dpop_key_json = ? WHERE state = ?", [try encryptIfNeeded(verifier), try key.map(encryptIfNeeded), state])
                    changed = true
                }
            }
            if changed { try exec("INSERT OR REPLACE INTO auth_vault_cleanup (id, pending) VALUES (1, 1)") }
            try exec("COMMIT")
        } catch { try? exec("ROLLBACK"); throw error }
        // The marker survives an interrupted cleanup; the next startup retries it.
        if try query("SELECT pending FROM auth_vault_cleanup WHERE id = 1", []).first?["pending"] == "1" {
            try checkpointAuthVault()
            try exec("VACUUM")
            try checkpointAuthVault()
            try exec("UPDATE auth_vault_cleanup SET pending = 0 WHERE id = 1")
            try checkpointAuthVault()
        }
    }

    private func checkpointAuthVault() throws {
        guard try query("PRAGMA wal_checkpoint(TRUNCATE)", []).first?["busy"] == "0" else {
            throw SQLiteStoreError.statement(message: "Auth vault cleanup is blocked by an active database reader")
        }
    }

    private func encryptIfNeeded(_ value: String) throws -> String {
        value.hasPrefix("aesgcm:v1:") ? value : try encryptAuthMaterial(value)
    }

    public func createOAuthState(
        state: String,
        handle: String,
        pkceVerifier: String,
        nonce: String,
        authServer: String? = nil,
        tokenEndpoint: String? = nil,
        pdsEndpoint: String? = nil,
        dpopKeyJSON: String? = nil,
        purpose: OAuthPurpose = .signIn,
        initiatorDid: String? = nil,
        inviteToken: String? = nil,
        returnTo: String? = nil,
        expiresAt: String
    ) throws {
        try run("DELETE FROM oauth_states WHERE expires_at <= ?", [Timestamp.iso8601()])
        try run(
            """
            INSERT OR REPLACE INTO oauth_states
                (state, handle, pkce_verifier, nonce, auth_server, token_endpoint, pds_endpoint, dpop_key_json,
                 purpose, initiator_did, invite_token, return_to, expires_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            [state, handle, try encryptAuthMaterial(pkceVerifier), nonce, authServer, tokenEndpoint, pdsEndpoint, try dpopKeyJSON.map(encryptAuthMaterial),
             purpose.rawValue, initiatorDid, inviteToken, returnTo, expiresAt]
        )
    }

    public func consumeOAuthState(state: String, now: String) throws -> OAuthStateRecord? {
        let rows = try query(
            """
            SELECT state, handle, pkce_verifier, nonce, auth_server, token_endpoint, pds_endpoint, dpop_key_json,
                   purpose, initiator_did, invite_token, return_to, expires_at
            FROM oauth_states
            WHERE state = ? AND expires_at > ?
            LIMIT 1
            """,
            [state, now]
        )
        try run("DELETE FROM oauth_states WHERE state = ?", [state])
        guard let row = rows.first,
              let state = row["state"],
              let handle = row["handle"],
              let pkceVerifier = row["pkce_verifier"],
              let nonce = row["nonce"],
              let expiresAt = row["expires_at"]
        else { return nil }
        return OAuthStateRecord(
            state: state,
            handle: handle,
            pkceVerifier: try decryptAuthMaterial(pkceVerifier),
            nonce: nonce,
            authServer: row["auth_server"],
            tokenEndpoint: row["token_endpoint"],
            pdsEndpoint: row["pds_endpoint"],
            dpopKeyJSON: try row["dpop_key_json"].map(decryptAuthMaterial),
            purpose: row["purpose"].flatMap(OAuthPurpose.init(rawValue:)) ?? .signIn,
            initiatorDid: row["initiator_did"],
            inviteToken: row["invite_token"],
            returnTo: row["return_to"],
            expiresAt: expiresAt
        )
    }

    public func createWebSession(
        sessionID: String,
        did: String,
        handle: String?,
        displayName: String?,
        avatar: String?,
        expiresAt: String
    ) throws {
        try run(
            """
            INSERT OR REPLACE INTO web_sessions
                (session_id, did, handle, display_name, avatar, expires_at)
            VALUES (?, ?, ?, ?, ?, ?)
            """,
            [sessionID, did, handle, displayName, avatar, expiresAt]
        )
    }

    public func viewer(forSessionID sessionID: String, now: String) throws -> Viewer? {
        let rows = try query(
            "SELECT did, handle, display_name, avatar FROM web_sessions WHERE session_id = ? AND expires_at > ? LIMIT 1",
            [sessionID, now]
        )
        guard let row = rows.first else { return nil }
        return Viewer(
            did: row["did"] ?? "",
            handle: row["handle"],
            displayName: row["display_name"],
            avatar: row["avatar"]
        )
    }

    public func deleteWebSession(sessionID: String) throws {
        try run("DELETE FROM web_sessions WHERE session_id = ?", [sessionID])
    }

    public func createOAuthSession(_ session: OAuthSessionRecord, now: String) throws {
        try run(
            """
            INSERT OR REPLACE INTO oauth_sessions
                (did, handle, token_json, dpop_key_json, updated_at)
            VALUES (?, ?, ?, ?, ?)
            """,
            [
                session.did,
                session.handle,
                try encryptAuthMaterial(session.tokenJSON),
                try encryptAuthMaterial(session.dpopKeyJSON),
                now,
            ]
        )
    }

    func replaceOAuthSessionAfterRefresh(
        _ replacement: OAuthSessionRecord,
        previousAccessToken: String,
        now: String
    ) throws {
        guard let current = try oauthSession(did: replacement.did),
              let token = try? decoder.decode(ATProtoTokenPayload.self, from: Data(current.tokenJSON.utf8)),
              token.accessToken == previousAccessToken else { return }
        try createOAuthSession(replacement, now: now)
    }

    public func upsertManagedAccount(_ account: ManagedAccount, now: String) throws {
        if account.isDefault {
            try run("UPDATE managed_accounts SET is_default = 0 WHERE did != ?", [account.did])
        }
        try run(
            """
            INSERT INTO managed_accounts
                (did, handle, display_name, avatar, pds_endpoint, status, is_default, updated_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(did) DO UPDATE SET
                handle = excluded.handle,
                display_name = excluded.display_name,
                avatar = excluded.avatar,
                pds_endpoint = excluded.pds_endpoint,
                status = excluded.status,
                is_default = CASE
                    WHEN excluded.is_default = 1 THEN 1
                    ELSE managed_accounts.is_default
                END,
                updated_at = excluded.updated_at
            """,
            [
                account.did,
                account.handle,
                account.displayName,
                account.avatar,
                account.pdsEndpoint,
                account.status.rawValue,
                account.isDefault ? "1" : "0",
                now,
            ]
        )
    }

    public func listManagedAccounts() throws -> [ManagedAccount] {
        try query(
            """
            SELECT did, handle, display_name, avatar, pds_endpoint, status, is_default
            FROM managed_accounts
            ORDER BY is_default DESC, handle ASC, did ASC
            """,
            []
        ).compactMap(account(from:))
    }

    public func managedAccount(did: String) throws -> ManagedAccount? {
        try query(
            """
            SELECT did, handle, display_name, avatar, pds_endpoint, status, is_default
            FROM managed_accounts
            WHERE did = ?
            LIMIT 1
            """,
            [did]
        ).first.flatMap(account(from:))
    }

    public func markAccountNeedsReauth(did: String, now: String) throws {
        try run(
            "UPDATE managed_accounts SET status = 'needs_reauth', updated_at = ? WHERE did = ?",
            [now, did]
        )
    }

    public func upsertViewerAccount(_ access: ViewerAccountAccess) throws {
        if access.isDefault {
            try run(
                "UPDATE viewer_accounts SET is_default = 0 WHERE viewer_did = ? AND account_did != ?",
                [access.viewerDid, access.accountDid]
            )
        }
        try run(
            """
            INSERT INTO viewer_accounts
                (viewer_did, account_did, access_kind, team_uri, capabilities_json, is_default, created_at, updated_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(viewer_did, account_did) DO UPDATE SET
                access_kind = excluded.access_kind,
                team_uri = excluded.team_uri,
                capabilities_json = excluded.capabilities_json,
                is_default = excluded.is_default,
                updated_at = excluded.updated_at
            """,
            [
                access.viewerDid,
                access.accountDid,
                access.accessKind.rawValue,
                access.teamUri,
                try encodeJSON(access.capabilities),
                access.isDefault ? "1" : "0",
                access.createdAt,
                access.updatedAt,
            ]
        )
    }

    public func viewerAccount(viewerDid: String, accountDid: String) throws -> ViewerAccountAccess? {
        try query(
            """
            SELECT viewer_did, account_did, access_kind, team_uri, capabilities_json,
                   is_default, created_at, updated_at
            FROM viewer_accounts
            WHERE viewer_did = ? AND account_did = ?
            LIMIT 1
            """,
            [viewerDid, accountDid]
        ).first.flatMap(viewerAccount(from:))
    }

    public func listViewerAccounts(viewerDid: String) throws -> [ViewerAccountAccess] {
        try query(
            """
            SELECT viewer_did, account_did, access_kind, team_uri, capabilities_json,
                   is_default, created_at, updated_at
            FROM viewer_accounts
            WHERE viewer_did = ?
            ORDER BY is_default DESC, account_did ASC
            """,
            [viewerDid]
        ).compactMap(viewerAccount(from:))
    }

    public func deleteViewerAccount(viewerDid: String, accountDid: String) throws {
        try run(
            "DELETE FROM viewer_accounts WHERE viewer_did = ? AND account_did = ?",
            [viewerDid, accountDid]
        )
    }

    public func createTeamInvite(_ invite: TeamInvite) throws {
        try run(
            """
            INSERT INTO team_invites
                (id, token, team_uri, team_title, owner_did, invited_handle, invited_did, role,
                 status, inviter_did, accepted_did, created_at, expires_at, updated_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            [
                invite.id, invite.token, invite.teamUri, invite.teamTitle, invite.ownerDid,
                invite.invitedHandle, invite.invitedDid, invite.role.rawValue, invite.status.rawValue,
                invite.inviterDid, invite.acceptedDid, invite.createdAt, invite.expiresAt, invite.updatedAt,
            ]
        )
    }

    public func teamInvite(token: String) throws -> TeamInvite? {
        try query(
            """
            SELECT id, token, team_uri, team_title, owner_did, invited_handle, invited_did, role,
                   status, inviter_did, accepted_did, created_at, expires_at, updated_at
            FROM team_invites WHERE token = ? LIMIT 1
            """,
            [token]
        ).first.flatMap(teamInvite(from:))
    }

    public func teamInvite(id: String, ownerDid: String) throws -> TeamInvite? {
        try query(
            """
            SELECT id, token, team_uri, team_title, owner_did, invited_handle, invited_did, role,
                   status, inviter_did, accepted_did, created_at, expires_at, updated_at
            FROM team_invites WHERE id = ? AND owner_did = ? LIMIT 1
            """,
            [id, ownerDid]
        ).first.flatMap(teamInvite(from:))
    }

    public func teamInvite(id: String) throws -> TeamInvite? {
        try query(
            """
            SELECT id, token, team_uri, team_title, owner_did, invited_handle, invited_did, role,
                   status, inviter_did, accepted_did, created_at, expires_at, updated_at
            FROM team_invites WHERE id = ? LIMIT 1
            """,
            [id]
        ).first.flatMap(teamInvite(from:))
    }

    public func listTeamInvites(ownerDid: String, teamUri: String? = nil) throws -> [TeamInvite] {
        let sql: String
        let values: [String?]
        if let teamUri {
            sql = """
                SELECT id, token, team_uri, team_title, owner_did, invited_handle, invited_did, role,
                       status, inviter_did, accepted_did, created_at, expires_at, updated_at
                FROM team_invites WHERE owner_did = ? AND team_uri = ? ORDER BY created_at DESC
                """
            values = [ownerDid, teamUri]
        } else {
            sql = """
                SELECT id, token, team_uri, team_title, owner_did, invited_handle, invited_did, role,
                       status, inviter_did, accepted_did, created_at, expires_at, updated_at
                FROM team_invites WHERE owner_did = ? ORDER BY created_at DESC
                """
            values = [ownerDid]
        }
        return try query(sql, values).compactMap(teamInvite(from:))
    }

    public func updateTeamInvite(
        id: String,
        ownerDid: String,
        status: TeamInviteStatus,
        acceptedDid: String? = nil,
        now: String
    ) throws {
        try run(
            """
            UPDATE team_invites
            SET status = ?, accepted_did = COALESCE(?, accepted_did), updated_at = ?
            WHERE id = ? AND owner_did = ?
            """,
            [status.rawValue, acceptedDid, now, id, ownerDid]
        )
    }

    public func upsertProEntitlement(_ entitlement: ProEntitlement) throws {
        try run(
            """
            INSERT INTO pro_entitlements
                (viewer_did, scope_type, scope_value, status, source, granted_by_did, expires_at, created_at, updated_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(viewer_did) DO UPDATE SET
                scope_type = excluded.scope_type,
                scope_value = excluded.scope_value,
                status = excluded.status,
                source = excluded.source,
                granted_by_did = excluded.granted_by_did,
                expires_at = excluded.expires_at,
                updated_at = excluded.updated_at
            """,
            [
                entitlement.subject, entitlement.scope.rawValue, entitlement.subject,
                entitlement.status.rawValue, entitlement.source,
                entitlement.grantedByDid, entitlement.expiresAt, entitlement.createdAt, entitlement.updatedAt,
            ]
        )
    }

    public func proEntitlement(scope: ProEntitlementScope = .actor, subject: String) throws -> ProEntitlement? {
        try query(
            """
            SELECT viewer_did, scope_type, scope_value, status, source, granted_by_did, expires_at, created_at, updated_at
            FROM pro_entitlements
            WHERE scope_type = ? AND COALESCE(scope_value, viewer_did) = ?
            LIMIT 1
            """,
            [scope.rawValue, subject]
        ).first.flatMap(proEntitlement(from:))
    }

    public func listProEntitlements() throws -> [ProEntitlement] {
        try query(
            """
            SELECT viewer_did, scope_type, scope_value, status, source, granted_by_did, expires_at, created_at, updated_at
            FROM pro_entitlements ORDER BY updated_at DESC, viewer_did ASC
            """,
            []
        ).compactMap(proEntitlement(from:))
    }

    /// Claim after signature verification and before any authenticated operation.
    /// SQLite uniqueness makes replay protection survive restarts and concurrent requests.
    public func claimServiceAuth(issuer: String, jti: String, expiresAt: Int, now: Int) throws -> Bool {
        try run("DELETE FROM service_auth_replays WHERE expires_at <= ?", [String(now)])
        try run("INSERT OR IGNORE INTO service_auth_replays (issuer, jti, expires_at) VALUES (?, ?, ?)",
                    [issuer, jti, String(expiresAt)])
        return sqlite3_changes(db) == 1
    }

    public func oauthSession(did: String) throws -> OAuthSessionRecord? {
        let rows = try query(
            "SELECT did, handle, token_json, dpop_key_json FROM oauth_sessions WHERE did = ? LIMIT 1",
            [did]
        )
        guard let row = rows.first,
              let did = row["did"],
              let tokenJSON = row["token_json"],
              let dpopKeyJSON = row["dpop_key_json"]
        else { return nil }
        return OAuthSessionRecord(
            did: did,
            handle: row["handle"],
            tokenJSON: try decryptAuthMaterial(tokenJSON),
            dpopKeyJSON: try decryptAuthMaterial(dpopKeyJSON)
        )
    }

    public func writeScheduleRecord(
        did: String,
        rkey: String,
        record: SkejScheduleRecord,
        now: String
    ) throws {
        try writeProtocolRecord(did: did, collection: "at.skej.schedule", rkey: rkey, record: record, now: now)
        let data = try encoder.encode(record)
        guard let json = String(data: data, encoding: .utf8) else {
            throw SQLiteStoreError.statement(message: "Could not encode schedule record")
        }
        try run(
            """
            INSERT INTO pds_schedule_records (did, rkey, record_json, updated_at)
            VALUES (?, ?, ?, ?)
            ON CONFLICT(did, rkey) DO UPDATE SET
                record_json = excluded.record_json,
                updated_at = excluded.updated_at
            """,
            [did, rkey, json, now]
        )
    }

    public func scheduleRecord(did: String, rkey: String) throws -> SkejScheduleRecord? {
        if let record = try protocolRecord(did: did, collection: "at.skej.schedule", rkey: rkey, as: SkejScheduleRecord.self) {
            return record
        }
        let rows = try query(
            """
            SELECT record_json
            FROM pds_schedule_records
            WHERE did = ? AND rkey = ?
            LIMIT 1
            """,
            [did, rkey]
        )
        guard let json = rows.first?["record_json"],
              let data = json.data(using: .utf8)
        else { return nil }
        return try decoder.decode(SkejScheduleRecord.self, from: data)
    }

    public func listScheduleRecords(did: String) throws -> [String: SkejScheduleRecord] {
        let protocolRecords = try listProtocolRecords(did: did, collection: "at.skej.schedule", as: SkejScheduleRecord.self)
        if !protocolRecords.isEmpty { return protocolRecords }
        let rows = try query(
            """
            SELECT rkey, record_json
            FROM pds_schedule_records
            WHERE did = ?
            """,
            [did]
        )
        var records: [String: SkejScheduleRecord] = [:]
        for row in rows {
            guard let rkey = row["rkey"],
                  let json = row["record_json"],
                  let data = json.data(using: .utf8)
            else { continue }
            records[rkey] = try decoder.decode(SkejScheduleRecord.self, from: data)
        }
        return records
    }

    public func deleteScheduleRecord(did: String, rkey: String) throws {
        try run("DELETE FROM pds_protocol_records WHERE did = ? AND collection = ? AND rkey = ?", [did, "at.skej.schedule", rkey])
        try run("DELETE FROM pds_schedule_records WHERE did = ? AND rkey = ?", [did, rkey])
    }

    public func writeProtocolRecord<Value: Codable>(
        did: String,
        collection: String,
        rkey: String,
        record: Value,
        now: String
    ) throws {
        let data = try encoder.encode(record)
        guard let json = String(data: data, encoding: .utf8) else {
            throw SQLiteStoreError.statement(message: "Could not encode protocol record")
        }
        try run(
            """
            INSERT INTO pds_protocol_records (did, collection, rkey, record_json, updated_at)
            VALUES (?, ?, ?, ?, ?)
            ON CONFLICT(did, collection, rkey) DO UPDATE SET
                record_json = excluded.record_json,
                updated_at = excluded.updated_at
            """,
            [did, collection, rkey, json, now]
        )
    }

    public func protocolRecord<Value: Codable>(
        did: String,
        collection: String,
        rkey: String,
        as type: Value.Type
    ) throws -> Value? {
        let rows = try query(
            """
            SELECT record_json
            FROM pds_protocol_records
            WHERE did = ? AND collection = ? AND rkey = ?
            LIMIT 1
            """,
            [did, collection, rkey]
        )
        guard let json = rows.first?["record_json"],
              let data = json.data(using: .utf8)
        else { return nil }
        return try decoder.decode(type, from: data)
    }

    public func listProtocolRecords<Value: Codable>(
        did: String,
        collection: String,
        as type: Value.Type
    ) throws -> [String: Value] {
        let rows = try query(
            """
            SELECT rkey, record_json
            FROM pds_protocol_records
            WHERE did = ? AND collection = ?
            """,
            [did, collection]
        )
        var records: [String: Value] = [:]
        for row in rows {
            guard let rkey = row["rkey"],
                  let json = row["record_json"],
                  let data = json.data(using: .utf8)
            else { continue }
            records[rkey] = try decoder.decode(type, from: data)
        }
        return records
    }

    public func listProtocolRecords<Value: Codable & Sendable>(
        collection: String,
        as type: Value.Type
    ) throws -> [StoredProtocolRecord<Value>] {
        let rows = try query(
            """
            SELECT did, rkey, record_json
            FROM pds_protocol_records
            WHERE collection = ?
            ORDER BY did ASC, rkey ASC
            """,
            [collection]
        )
        return try rows.compactMap { row in
            guard let did = row["did"],
                  let rkey = row["rkey"],
                  let json = row["record_json"],
                  let data = json.data(using: .utf8)
            else { return nil }
            return StoredProtocolRecord(did: did, rkey: rkey, value: try decoder.decode(type, from: data))
        }
    }

    public func rememberScheduleSubmission(did: String, rkey: String, now: String) throws {
        try run("INSERT OR IGNORE INTO pending_schedule_submissions (did, rkey, next_check_at, created_at) VALUES (?, ?, ?, ?)",
            [did, rkey, now, now])
    }

    public func nextPendingScheduleSubmission(now: String, retryAt: String, expireBefore: String) throws -> (did: String, rkey: String)? {
        try run("DELETE FROM pending_schedule_submissions WHERE created_at < ?", [expireBefore])
        guard let row = try query("""
            SELECT did, rkey FROM pending_schedule_submissions
            WHERE next_check_at <= ? ORDER BY next_check_at, did, rkey LIMIT 1
            """, [now]).first, let did = row["did"], let rkey = row["rkey"] else { return nil }
        try run("UPDATE pending_schedule_submissions SET next_check_at = ? WHERE did = ? AND rkey = ?",
            [retryAt, did, rkey])
        return (did, rkey)
    }

    public func hasImportedLegacyScheduleQueue(did: String) throws -> Bool {
        try !query("SELECT did FROM imported_schedule_queues WHERE did = ?", [did]).isEmpty
    }

    public func importLegacyScheduleQueue(did: String, jobs: [ScheduledJob], now: String) throws {
        try exec("BEGIN IMMEDIATE")
        do {
            if try !hasImportedLegacyScheduleQueue(did: did) {
                for job in jobs where job.did == did {
                    if try scheduleJob(did: did, rkey: job.rkey) == nil {
                        try upsertScheduleJob(job, now: now)
                    }
                }
                try run("INSERT INTO imported_schedule_queues (did) VALUES (?)", [did])
            }
            try exec("COMMIT")
        } catch {
            try? exec("ROLLBACK")
            throw error
        }
    }

    public func insertScheduleJobIfMissing(_ job: ScheduledJob, now: String) throws {
        try exec("BEGIN IMMEDIATE")
        do {
            if try scheduleJob(did: job.did, rkey: job.rkey) == nil {
                try upsertScheduleJob(job, now: now)
            }
            try run("DELETE FROM pending_schedule_submissions WHERE did = ? AND rkey = ?", [job.did, job.rkey])
            try exec("COMMIT")
        } catch {
            try? exec("ROLLBACK")
            throw error
        }
    }

    public func upsertScheduleJob(_ job: ScheduledJob, now: String) throws {
        let errorJSON = try job.lastError.map(encodeJSON)
        try run(
            """
            INSERT INTO scheduled_jobs
                (
                    did, rkey, scheduled_for, scheduled_at, status, attempts,
                    last_error, next_attempt_at, last_attempt_at, publish_rkey, record_type,
                    published_uri, published_cid, depends_on_schedule_uri, parent_published_uri, updated_at
                )
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(did, rkey) DO UPDATE SET
                scheduled_for = excluded.scheduled_for,
                scheduled_at = excluded.scheduled_at,
                status = excluded.status,
                attempts = excluded.attempts,
                last_error = excluded.last_error,
                next_attempt_at = excluded.next_attempt_at,
                last_attempt_at = excluded.last_attempt_at,
                publish_rkey = excluded.publish_rkey,
                record_type = excluded.record_type,
                published_uri = excluded.published_uri,
                published_cid = excluded.published_cid,
                depends_on_schedule_uri = excluded.depends_on_schedule_uri,
                parent_published_uri = excluded.parent_published_uri,
                updated_at = excluded.updated_at
            """,
            [
                job.did,
                job.rkey,
                job.scheduledAt,
                job.scheduledAt,
                job.status.rawValue,
                String(job.attempts),
                errorJSON,
                job.nextAttemptAt,
                job.lastAttemptAt,
                job.publishRkey,
                job.recordType,
                job.publishedUri,
                job.publishedCid,
                job.dependsOnScheduleUri,
                job.parentPublishedUri,
                now,
            ]
        )
    }

    public func listScheduleJobs(did: String) throws -> [ScheduledJob] {
        try query(
            """
            SELECT did, rkey, COALESCE(scheduled_at, scheduled_for) AS scheduled_at, status, attempts,
                   last_error, next_attempt_at, last_attempt_at, publish_rkey, record_type,
                   published_uri, published_cid, depends_on_schedule_uri, parent_published_uri
            FROM scheduled_jobs
            WHERE did = ?
            ORDER BY scheduled_at ASC
            """,
            [did]
        ).compactMap(job(from:))
    }

    public func scheduleJob(did: String, rkey: String) throws -> ScheduledJob? {
        try query(
            """
            SELECT did, rkey, COALESCE(scheduled_at, scheduled_for) AS scheduled_at, status, attempts,
                   last_error, next_attempt_at, last_attempt_at, publish_rkey, record_type,
                   published_uri, published_cid, depends_on_schedule_uri, parent_published_uri
            FROM scheduled_jobs
            WHERE did = ? AND rkey = ?
            LIMIT 1
            """,
            [did, rkey]
        ).first.flatMap(job(from:))
    }

    public func deleteScheduleJob(did: String, rkey: String) throws {
        try run("DELETE FROM scheduled_jobs WHERE did = ? AND rkey = ?", [did, rkey])
    }

    /// Serializes API edits with the worker claim without holding SQLite transactions across HTTP.
    public func claimScheduleEdit(did: String, rkey: String, leaseID: String, now: String, expiresAt: String) throws -> Bool {
        try run("DELETE FROM schedule_edit_leases WHERE expires_at <= ?", [now])
        try run("""
            INSERT OR IGNORE INTO schedule_edit_leases (did, rkey, lease_id, expires_at)
            SELECT did, rkey, ?, ? FROM scheduled_jobs
            WHERE did = ? AND rkey = ? AND status IN ('draft', 'scheduled', 'blocked', 'failed')
                AND published_uri IS NULL
            """, [leaseID, expiresAt, did, rkey])
        return sqlite3_changes(db) == 1
    }

    public func releaseScheduleEdit(did: String, rkey: String, leaseID: String) throws {
        try run("DELETE FROM schedule_edit_leases WHERE did = ? AND rkey = ? AND lease_id = ?", [did, rkey, leaseID])
    }

    public func claimDueJobs(now: String, limit: Int = 10) throws -> [ScheduledJob] {
        let jobs = try query(
            """
            SELECT did, rkey, COALESCE(scheduled_at, scheduled_for) AS scheduled_at, status, attempts,
                   last_error, next_attempt_at, last_attempt_at, publish_rkey, record_type,
                   published_uri, published_cid, depends_on_schedule_uri, parent_published_uri
            FROM scheduled_jobs
            WHERE status = 'scheduled'
                AND NOT EXISTS (SELECT 1 FROM schedule_edit_leases e WHERE e.did = scheduled_jobs.did AND e.rkey = scheduled_jobs.rkey AND e.expires_at > ?)
                AND COALESCE(scheduled_at, scheduled_for) <= ?
                AND (next_attempt_at IS NULL OR next_attempt_at <= ?)
            ORDER BY scheduled_at ASC
            LIMIT ?
            """,
            [now, now, now, String(limit)]
        ).compactMap(job(from:))

        for job in jobs {
            try run(
                """
                UPDATE scheduled_jobs
                SET status = 'publishing',
                    attempts = attempts + 1,
                    last_attempt_at = ?,
                    updated_at = ?
                WHERE did = ? AND rkey = ?
                """,
                [now, now, job.did, job.rkey]
            )
        }

        return jobs
    }

    public func blockedJobs() throws -> [ScheduledJob] {
        try query(
            """
            SELECT did, rkey, COALESCE(scheduled_at, scheduled_for) AS scheduled_at, status, attempts,
                   last_error, next_attempt_at, last_attempt_at, publish_rkey, record_type,
                   published_uri, published_cid, depends_on_schedule_uri, parent_published_uri
            FROM scheduled_jobs
            WHERE status = 'blocked'
            ORDER BY scheduled_at ASC
            """,
            []
        ).compactMap(job(from:))
    }

    public func markJobScheduled(
        did: String,
        rkey: String,
        parentPublishedUri: String?,
        now: String
    ) throws {
        try run(
            """
            UPDATE scheduled_jobs
            SET status = 'scheduled',
                parent_published_uri = ?,
                last_error = NULL,
                updated_at = ?
            WHERE did = ? AND rkey = ?
            """,
            [parentPublishedUri, now, did, rkey]
        )
    }

    public func markJobPublished(
        did: String,
        rkey: String,
        published: PublishedPost,
        now: String
    ) throws {
        try run(
            """
            UPDATE scheduled_jobs
            SET status = 'published',
                published_uri = ?,
                published_cid = ?,
                last_error = NULL,
                next_attempt_at = NULL,
                updated_at = ?
            WHERE did = ? AND rkey = ?
            """,
            [published.uri, published.cid, now, did, rkey]
        )
    }

    public func updatePublishRkey(did: String, rkey: String, publishRkey: String, now: String) throws {
        try run(
            """
            UPDATE scheduled_jobs
            SET publish_rkey = ?, updated_at = ?
            WHERE did = ? AND rkey = ?
            """,
            [publishRkey, now, did, rkey]
        )
    }

    public func markJobFailed(did: String, rkey: String, error: ScheduleError, now: String) throws {
        try run(
            """
            UPDATE scheduled_jobs
            SET status = 'failed', last_error = ?, updated_at = ?
            WHERE did = ? AND rkey = ?
            """,
            [try encodeJSON(error), now, did, rkey]
        )
    }

    public func markJobRetry(
        did: String,
        rkey: String,
        error: ScheduleError,
        nextAttemptAt: String,
        now: String
    ) throws {
        try run(
            """
            UPDATE scheduled_jobs
            SET status = 'scheduled',
                last_error = ?,
                next_attempt_at = ?,
                updated_at = ?
            WHERE did = ? AND rkey = ?
            """,
            [try encodeJSON(error), nextAttemptAt, now, did, rkey]
        )
    }

    public func markJobBlocked(
        did: String,
        rkey: String,
        error: ScheduleError,
        now: String
    ) throws {
        try run(
            """
            UPDATE scheduled_jobs
            SET status = 'blocked',
                last_error = ?,
                updated_at = ?
            WHERE did = ? AND rkey = ?
            """,
            [try encodeJSON(error), now, did, rkey]
        )
    }

    public func insertAuditEvent(
        did: String,
        scheduleRkey: String?,
        action: String,
        message: String,
        now: String
    ) throws {
        try run(
            """
            INSERT INTO audit_events (id, did, schedule_rkey, action, message, created_at)
            VALUES (?, ?, ?, ?, ?, ?)
            """,
            [UUID().uuidString, did, scheduleRkey, action, message, now]
        )
    }

    public func listAuditEvents(did: String, limit: Int = 100) throws -> [AuditEvent] {
        try query(
            """
            SELECT id, did, schedule_rkey, action, message, created_at
            FROM audit_events
            WHERE did = ?
            ORDER BY created_at DESC
            LIMIT ?
            """,
            [did, String(limit)]
        ).compactMap(auditEvent(from:))
    }

    public func findPublishedSchedule(scheduleUri: String) throws -> (did: String, rkey: String, record: SkejScheduleRecord)? {
        guard let parsed = parseScheduleURI(scheduleUri) else { return nil }
        guard let record = try scheduleRecord(did: parsed.did, rkey: parsed.rkey),
              record.status == .published
        else { return nil }
        return (parsed.did, parsed.rkey, record)
    }

    public func updateParentPublishedUri(
        did: String,
        rkey: String,
        parentPublishedUri: String,
        now: String
    ) throws {
        try run(
            """
            UPDATE scheduled_jobs
            SET parent_published_uri = ?, updated_at = ?
            WHERE did = ? AND rkey = ?
            """,
            [parentPublishedUri, now, did, rkey]
        )
    }

    private func job(from row: [String: String]) -> ScheduledJob? {
        guard let did = row["did"],
              let rkey = row["rkey"],
              let scheduledAt = row["scheduled_at"],
              let rawStatus = row["status"],
              let status = scheduleStatus(from: rawStatus)
        else { return nil }
        return ScheduledJob(
            did: did,
            rkey: rkey,
            scheduledAt: scheduledAt,
            status: status,
            attempts: Int(row["attempts"] ?? "0") ?? 0,
            lastError: decodeJSON(ScheduleError.self, from: row["last_error"]),
            nextAttemptAt: row["next_attempt_at"],
            lastAttemptAt: row["last_attempt_at"],
            publishRkey: row["publish_rkey"] ?? rkey,
            recordType: row["record_type"] ?? "app.bsky.feed.post",
            publishedUri: row["published_uri"],
            publishedCid: row["published_cid"],
            dependsOnScheduleUri: row["depends_on_schedule_uri"],
            parentPublishedUri: row["parent_published_uri"]
        )
    }

    private func account(from row: [String: String]) -> ManagedAccount? {
        guard let did = row["did"],
              let rawStatus = row["status"],
              let status = ManagedAccountStatus(rawValue: rawStatus)
        else { return nil }
        return ManagedAccount(
            did: did,
            handle: row["handle"],
            displayName: row["display_name"],
            avatar: row["avatar"],
            pdsEndpoint: row["pds_endpoint"],
            status: status,
            isDefault: row["is_default"] == "1"
        )
    }

    private func viewerAccount(from row: [String: String]) -> ViewerAccountAccess? {
        guard let viewerDid = row["viewer_did"],
              let accountDid = row["account_did"],
              let rawKind = row["access_kind"],
              let accessKind = ViewerAccountAccessKind(rawValue: rawKind),
              let createdAt = row["created_at"],
              let updatedAt = row["updated_at"]
        else { return nil }
        return ViewerAccountAccess(
            viewerDid: viewerDid,
            accountDid: accountDid,
            accessKind: accessKind,
            teamUri: row["team_uri"],
            capabilities: decodeJSON([BrandCapability].self, from: row["capabilities_json"]) ?? [],
            isDefault: row["is_default"] == "1",
            createdAt: createdAt,
            updatedAt: updatedAt
        )
    }

    private func teamInvite(from row: [String: String]) -> TeamInvite? {
        guard let id = row["id"],
              let token = row["token"],
              let teamUri = row["team_uri"],
              let teamTitle = row["team_title"],
              let ownerDid = row["owner_did"],
              let rawRole = row["role"],
              let role = TeamRole(rawValue: rawRole),
              let rawStatus = row["status"],
              let status = TeamInviteStatus(rawValue: rawStatus),
              let inviterDid = row["inviter_did"],
              let createdAt = row["created_at"],
              let expiresAt = row["expires_at"],
              let updatedAt = row["updated_at"]
        else { return nil }
        return TeamInvite(
            id: id,
            token: token,
            teamUri: teamUri,
            teamTitle: teamTitle,
            ownerDid: ownerDid,
            invitedHandle: row["invited_handle"],
            invitedDid: row["invited_did"],
            role: role,
            status: status,
            inviterDid: inviterDid,
            acceptedDid: row["accepted_did"],
            createdAt: createdAt,
            expiresAt: expiresAt,
            updatedAt: updatedAt
        )
    }

    private func proEntitlement(from row: [String: String]) -> ProEntitlement? {
        guard let subject = row["scope_value"] ?? row["viewer_did"],
              let rawScope = row["scope_type"],
              let scope = ProEntitlementScope(rawValue: rawScope),
              let rawStatus = row["status"],
              let status = ProEntitlementStatus(rawValue: rawStatus),
              let source = row["source"],
              let createdAt = row["created_at"],
              let updatedAt = row["updated_at"]
        else { return nil }
        return ProEntitlement(
            scope: scope,
            subject: subject,
            status: status,
            source: source,
            grantedByDid: row["granted_by_did"],
            expiresAt: row["expires_at"],
            createdAt: createdAt,
            updatedAt: updatedAt
        )
    }

    private func auditEvent(from row: [String: String]) -> AuditEvent? {
        guard let id = row["id"],
              let did = row["did"],
              let action = row["action"],
              let message = row["message"],
              let createdAt = row["created_at"]
        else { return nil }
        return AuditEvent(
            id: id,
            did: did,
            scheduleRkey: row["schedule_rkey"],
            action: action,
            message: message,
            createdAt: createdAt
        )
    }

    func exec(_ sql: String) throws {
        var error: UnsafeMutablePointer<CChar>?
        if sqlite3_exec(db, sql, nil, nil, &error) != SQLITE_OK {
            let message = error.map { String(cString: $0) } ?? "SQLite exec failed"
            sqlite3_free(error)
            throw SQLiteStoreError.statement(message: message)
        }
    }

    func run(_ sql: String, _ values: [String?]) throws {
        let statement = try prepare(sql)
        defer { sqlite3_finalize(statement) }
        try bind(values, to: statement)
        guard sqlite3_step(statement) == SQLITE_DONE else {
            throw SQLiteStoreError.statement(message: lastError())
        }
    }

    func query(_ sql: String, _ values: [String?]) throws -> [[String: String]] {
        let statement = try prepare(sql)
        defer { sqlite3_finalize(statement) }
        try bind(values, to: statement)
        var rows: [[String: String]] = []
        while sqlite3_step(statement) == SQLITE_ROW {
            var row: [String: String] = [:]
            for index in 0..<sqlite3_column_count(statement) {
                let name = String(cString: sqlite3_column_name(statement, index))
                if let text = sqlite3_column_text(statement, index) {
                    row[name] = String(cString: text)
                }
            }
            rows.append(row)
        }
        return rows
    }

    private func prepare(_ sql: String) throws -> OpaquePointer? {
        var statement: OpaquePointer?
        guard sqlite3_prepare_v2(db, sql, -1, &statement, nil) == SQLITE_OK else {
            throw SQLiteStoreError.statement(message: lastError())
        }
        return statement
    }

    private func bind(_ values: [String?], to statement: OpaquePointer?) throws {
        for (offset, value) in values.enumerated() {
            let index = Int32(offset + 1)
            if let value {
                sqlite3_bind_text(statement, index, value, -1, sqliteTransient)
            } else {
                sqlite3_bind_null(statement, index)
            }
        }
    }

    private func lastError() -> String {
        guard let message = sqlite3_errmsg(db) else { return "SQLite error" }
        return String(cString: message)
    }

    private func addColumnIfMissing(table: String, column: String, definition: String) throws {
        let columns = try query("PRAGMA table_info(\(table))", [])
        guard !columns.contains(where: { $0["name"] == column }) else { return }
        try exec("ALTER TABLE \(table) ADD COLUMN \(column) \(definition)")
    }

    private func encodeJSON<T: Encodable>(_ value: T) throws -> String {
        guard let string = String(data: try encoder.encode(value), encoding: .utf8) else {
            throw SQLiteStoreError.statement(message: "Could not encode JSON")
        }
        return string
    }

    private func decodeJSON<T: Decodable>(_ type: T.Type, from string: String?) -> T? {
        guard let string,
              let data = string.data(using: .utf8)
        else { return nil }
        return try? decoder.decode(type, from: data)
    }

    private func scheduleStatus(from raw: String) -> ScheduleStatus? {
        raw == "cancelled" ? .canceled : ScheduleStatus(rawValue: raw)
    }

    private func parseScheduleURI(_ uri: String) -> (did: String, rkey: String)? {
        guard uri.starts(with: "at://") else { return nil }
        let parts = uri.dropFirst("at://".count).split(separator: "/").map(String.init)
        guard parts.count == 3,
              parts[1] == "at.skej.schedule"
        else { return nil }
        return (parts[0], parts[2])
    }

    private func encryptAuthMaterial(_ plaintext: String) throws -> String {
        guard let key = authEncryptionKey() else { return plaintext }
        let sealed = try AES.GCM.seal(Data(plaintext.utf8), using: key)
        guard let combined = sealed.combined else {
            throw SQLiteStoreError.statement(message: "Could not encrypt auth material")
        }
        return "aesgcm:v1:\(combined.base64EncodedString())"
    }

    private func decryptAuthMaterial(_ stored: String) throws -> String {
        guard stored.starts(with: "aesgcm:v1:") else { return stored }
        guard let key = authEncryptionKey() else {
            throw SQLiteStoreError.statement(message: "SKEJ_AUTH_ENCRYPTION_KEY is required to decrypt auth material")
        }
        let encoded = String(stored.dropFirst("aesgcm:v1:".count))
        guard let data = Data(base64Encoded: encoded) else {
            throw SQLiteStoreError.statement(message: "Encrypted auth material is malformed")
        }
        let sealed = try AES.GCM.SealedBox(combined: data)
        let opened = try AES.GCM.open(sealed, using: key)
        guard let plaintext = String(data: opened, encoding: .utf8) else {
            throw SQLiteStoreError.statement(message: "Could not decode auth material")
        }
        return plaintext
    }

    private func authEncryptionKey() -> SymmetricKey? { authKey }
}

public struct OAuthStateRecord: Equatable, Sendable {
    public let state: String
    public let handle: String
    public let pkceVerifier: String
    public let nonce: String
    public let authServer: String?
    public let tokenEndpoint: String?
    public let pdsEndpoint: String?
    public let dpopKeyJSON: String?
    public let purpose: OAuthPurpose
    public let initiatorDid: String?
    public let inviteToken: String?
    public let returnTo: String?
    public let expiresAt: String
}

public struct OAuthSessionRecord: Equatable, Sendable {
    public let did: String
    public let handle: String?
    public let tokenJSON: String
    public let dpopKeyJSON: String

    public init(did: String, handle: String?, tokenJSON: String, dpopKeyJSON: String) {
        self.did = did
        self.handle = handle
        self.tokenJSON = tokenJSON
        self.dpopKeyJSON = dpopKeyJSON
    }
}

public enum SQLiteStoreError: Error, Equatable {
    case open(message: String)
    case statement(message: String)
}
