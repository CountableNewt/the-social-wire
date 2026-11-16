import Foundation

public struct ATProtoPDSClient: PDSClient {
    private let store: SQLiteStore
    private let http: HTTPClient
    private let clientID: String?
    private let dpopNonces: DPoPNonceStore
    private let tokenRefresh = SchedulingTokenRefresh()

    public init(store: SQLiteStore, clientID: String? = nil, http: HTTPClient = URLSessionHTTPClient()) {
        self.store = store
        self.clientID = clientID
        self.http = http
        self.dpopNonces = DPoPNonceStore()
    }

    public func writeRecord<Value: Codable & Sendable>(did: String, collection: String, rkey: String, record: Value) async throws {
        let session = try await authenticatedSession(did: did)
        try await xrpc(
            session: session,
            method: "POST",
            path: "com.atproto.repo.putRecord",
            body: [
                "repo": .string(did),
                "collection": .string(collection),
                "rkey": .string(rkey),
                "validate": .bool(false),
                "record": try record.skejJSONValue(),
            ]
        )
        try await store.writeProtocolRecord(
            did: did,
            collection: collection,
            rkey: rkey,
            record: record,
            now: Timestamp.iso8601()
        )
    }

    public func getRecord<Value: Codable & Sendable>(did: String, collection: String, rkey: String, as type: Value.Type) async throws -> Value? {
        do {
            let session = try await authenticatedSession(did: did)
            let url = "\(session.token.pdsEndpoint)/xrpc/com.atproto.repo.getRecord?repo=\(urlEncode(did))&collection=\(urlEncode(collection))&rkey=\(urlEncode(rkey))"
            let data = try await xrpcData(session: session, method: "GET", url: url, body: nil)
            let response = try JSONDecoder().decode(GetRecordResponse<Value>.self, from: data)
            try await store.writeProtocolRecord(
                did: did,
                collection: collection,
                rkey: rkey,
                record: response.value,
                now: Timestamp.iso8601()
            )
            return response.value
        } catch {
            // Same fallback as listRecords: an unreachable or unauthenticated PDS
            // reads through to the local cache rather than failing the request.
            // Deletes made through Skej clear the cache, so this returns nil for
            // them; a record deleted directly on the PDS reads stale until a
            // successful fetch replaces it.
            await flagReauthIfNeeded(did: did, error: error)
            return try await store.protocolRecord(did: did, collection: collection, rkey: rkey, as: type)
        }
    }

    public func listRecords<Value: Codable & Sendable>(did: String, collection: String, as type: Value.Type) async throws -> [String: Value] {
        do {
            // An account with no OAuth session is a normal state here — reads span
            // every managed account, not just the signed-in one — so it falls back
            // to the local cache like any other failure to reach the PDS.
            let session = try await authenticatedSession(did: did)
            let url = "\(session.token.pdsEndpoint)/xrpc/com.atproto.repo.listRecords?repo=\(urlEncode(did))&collection=\(urlEncode(collection))&limit=100"
            let data = try await xrpcData(session: session, method: "GET", url: url, body: nil)
            let response = try JSONDecoder().decode(ListRecordsResponse<Value>.self, from: data)
            var records: [String: Value] = [:]
            for record in response.records {
                guard let rkey = record.uri.split(separator: "/").last.map(String.init) else { continue }
                records[rkey] = record.value
                try await store.writeProtocolRecord(
                    did: did,
                    collection: collection,
                    rkey: rkey,
                    record: record.value,
                    now: Timestamp.iso8601()
                )
            }
            return records
        } catch {
            await flagReauthIfNeeded(did: did, error: error)
            return try await store.listProtocolRecords(did: did, collection: collection, as: type)
        }
    }

    public func writeSchedule(did: String, rkey: String, record: SkejScheduleRecord) async throws {
        try await writeRecord(did: did, collection: "at.skej.schedule", rkey: rkey, record: record)
    }

    /// Authenticated probe; unlike display reads, connection readiness never uses a cache fallback.
    public func verifySchedulingConnection(did: String) async throws {
        do {
            let session = try await authenticatedSession(did: did)
            let url = "\(session.token.pdsEndpoint)/xrpc/com.atproto.repo.listRecords?repo=\(urlEncode(did))&collection=at.skej.schedule&limit=1"
            _ = try await xrpcData(session: session, method: "GET", url: url, body: nil)
        } catch {
            await flagReauthIfNeeded(did: did, error: error)
            throw error
        }
    }

    /// A stale cached record must never authorize overwriting a finished PDS schedule.
    public func getScheduleForMutation(did: String, rkey: String) async throws -> SkejScheduleRecord? {
        let session = try await authenticatedSession(did: did)
        let url = "\(session.token.pdsEndpoint)/xrpc/com.atproto.repo.getRecord?repo=\(urlEncode(did))&collection=at.skej.schedule&rkey=\(urlEncode(rkey))"
        let data = try await xrpcData(session: session, method: "GET", url: url, body: nil)
        return try JSONDecoder().decode(GetRecordResponse<SkejScheduleRecord>.self, from: data).value
    }

    public func getSchedule(did: String, rkey: String) async throws -> SkejScheduleRecord? {
        try await getRecord(did: did, collection: "at.skej.schedule", rkey: rkey, as: SkejScheduleRecord.self)
    }

    /// Import and edit recovery must never fall back to an incomplete local cache.
    public func listAuthoritativeSchedules(did: String) async throws -> [String: SkejScheduleRecord] {
        let session = try await authenticatedSession(did: did)
        let base = "\(session.token.pdsEndpoint)/xrpc/com.atproto.repo.listRecords?repo=\(urlEncode(did))&collection=at.skej.schedule&limit=100"
        let prefix = "at://\(did)/at.skej.schedule/"
        var records: [String: SkejScheduleRecord] = [:]
        var cursor: String?
        var seenCursors = Set<String>()
        repeat {
            let url = base + (cursor.map { "&cursor=" + urlEncode($0) } ?? "")
            let data = try await xrpcData(session: session, method: "GET", url: url, body: nil)
            let page = try JSONDecoder().decode(ListRecordsResponse<SkejScheduleRecord>.self, from: data)
            for record in page.records {
                guard record.uri.hasPrefix(prefix), !record.uri.dropFirst(prefix.count).contains("/") else {
                    throw PDSClientError.publishFailed("Unexpected schedule URI in PDS response")
                }
                records[String(record.uri.dropFirst(prefix.count))] = record.value
            }
            cursor = page.cursor
            if let cursor {
                guard seenCursors.count < 100, seenCursors.insert(cursor).inserted else {
                    throw PDSClientError.publishFailed("PDS schedule pagination did not finish")
                }
            }
        } while cursor != nil
        return records
    }

    public func listSchedules(did: String) async throws -> [String: SkejScheduleRecord] {
        try await listRecords(did: did, collection: "at.skej.schedule", as: SkejScheduleRecord.self)
    }

    public func deleteSchedule(did: String, rkey: String) async throws {
        let session = try await authenticatedSession(did: did)
        try await xrpc(
            session: session,
            method: "POST",
            path: "com.atproto.repo.deleteRecord",
            body: [
                "repo": .string(did),
                "collection": .string("at.skej.schedule"),
                "rkey": .string(rkey),
            ]
        )
        try await store.deleteScheduleRecord(did: did, rkey: rkey)
    }

    public func writeCalendarEvent(
        did: String,
        rkey: String,
        record: CommunityCalendarEventRecord
    ) async throws -> CalendarEventReference {
        let session = try await authenticatedSession(did: did)
        let responseData = try await xrpc(
            session: session,
            method: "POST",
            path: "com.atproto.repo.putRecord",
            body: [
                "repo": .string(did),
                "collection": .string(CalendarEventProjection.collection),
                "rkey": .string(rkey),
                "validate": .bool(false),
                "record": try record.skejJSONValue(),
            ]
        )
        let created = try JSONDecoder().decode(CreateRecordResponse.self, from: responseData)
        try await store.writeProtocolRecord(
            did: did,
            collection: CalendarEventProjection.collection,
            rkey: rkey,
            record: record,
            now: Timestamp.iso8601()
        )
        return CalendarEventReference(uri: created.uri, cid: created.cid)
    }

    public func uploadBlob(
        did: String,
        data: Data,
        mimeType: String
    ) async throws -> ATProtoBlobReference {
        let session = try await authenticatedSession(did: did)
        // PDSes can reject a nonce-less request before consuming its body. Prime the
        // current nonce with a small request so a large upload is accepted immediately
        // instead of leaving URLSession waiting to send a body the server will not read.
        try await primeDPoPNonce(session: session)
        let responseData = try await xrpcData(
            session: session,
            method: "POST",
            url: "\(session.token.pdsEndpoint)/xrpc/com.atproto.repo.uploadBlob",
            body: data,
            contentType: mimeType
        )
        return try JSONDecoder().decode(UploadBlobResponse.self, from: responseData).blob
    }

    public func publishThread(did: String, record: SkejScheduleRecord) async throws -> [PublishedPostReference] {
        let session = try await authenticatedSession(did: did)
        if let shadowRecord = record.shadowRecord {
            let published = try await createPublishedRecord(
                session: session,
                did: did,
                collection: record.recordType,
                rkey: record.publishRkey,
                value: PostRecordCanonicalizer.canonicalizeFeedPost(shadowRecord)
            )
            return [PublishedPostReference(
                rkey: record.publishRkey,
                uri: published.uri,
                cid: published.cid
            )]
        }
        guard !record.posts.isEmpty else {
            throw PDSClientError.publishFailed("No record payload to publish")
        }

        var publishedPosts: [PublishedPostReference] = []
        let createdAt = Timestamp.iso8601()
        for (index, plan) in record.posts.enumerated() {
            guard let publishRkey = plan.publishRkey ?? (index == 0 ? record.publishRkey : nil),
                  !publishRkey.isEmpty
            else {
                throw PDSClientError.publishFailed("Post \(index + 1) has no stable publish rkey")
            }
            var value = try PostRecordCanonicalizer.feedPostValue(plan, createdAt: createdAt)
            if index == 0 {
                try applyExternalRelationship(record.dependency, to: &value)
            } else if let root = publishedPosts.first, let parent = publishedPosts.last {
                value["reply"] = replyValue(root: root, parent: parent)
            }
            let published = try await createPublishedRecord(
                session: session,
                did: did,
                collection: record.recordType,
                rkey: publishRkey,
                value: .object(value)
            )
            publishedPosts.append(PublishedPostReference(
                rkey: publishRkey,
                uri: published.uri,
                cid: published.cid
            ))
        }
        return publishedPosts
    }

    private func createPublishedRecord(
        session: AuthenticatedATProtoSession,
        did: String,
        collection: String,
        rkey: String,
        value: JSONValue
    ) async throws -> PublishedPost {
        let expectedURI = "at://\(did)/\(collection)/\(rkey)"
        do {
            let data = try await xrpc(
                session: session, method: "POST", path: "com.atproto.repo.createRecord",
                body: ["repo": .string(did), "collection": .string(collection), "rkey": .string(rkey),
                       "validate": .bool(true), "record": value]
            )
            let created = try JSONDecoder().decode(CreateRecordResponse.self, from: data)
            guard created.uri == expectedURI, !created.cid.isEmpty else {
                throw PDSClientError.publishFailed("PDS returned an unexpected publication record")
            }
            return PublishedPost(uri: created.uri, cid: created.cid)
        } catch {
            // A timed-out create or a retry of a partial thread can already be visible.
            // Accept only matching content at the intended key and never replace a record.
            let url = "\(session.token.pdsEndpoint)/xrpc/com.atproto.repo.getRecord?repo=\(urlEncode(did))&collection=\(urlEncode(collection))&rkey=\(urlEncode(rkey))"
            if let data = try? await xrpcData(session: session, method: "GET", url: url, body: nil),
               let existing = try? JSONDecoder().decode(GetRecordResponse<JSONValue>.self, from: data),
               existing.uri == expectedURI, let cid = existing.cid, !cid.isEmpty,
               matchingPublication(existing.value, value, collection: collection) {
                return PublishedPost(uri: existing.uri, cid: cid)
            }
            throw error
        }
    }

    private func matchingPublication(_ actual: JSONValue, _ expected: JSONValue, collection: String) -> Bool {
        if collection == "app.bsky.feed.post",
           case .object(var actualPost) = actual, case .object(var expectedPost) = expected {
            // A retry uses the actual time for posts it has yet to create.
            actualPost.removeValue(forKey: "createdAt")
            expectedPost.removeValue(forKey: "createdAt")
            return actualPost == expectedPost
        }
        return actual == expected
    }

    private func applyExternalRelationship(
        _ dependency: ScheduleDependency?,
        to post: inout [String: JSONValue]
    ) throws {
        guard let dependency, dependency.relationship != .after else { return }
        guard let uri = dependency.parentPublishedUri,
              let cid = dependency.parentPublishedCid
        else {
            throw PDSClientError.publishFailed("Parent URI and CID are required for \(dependency.relationship.rawValue)")
        }
        let parent = PublishedPostReference(rkey: "", uri: uri, cid: cid)
        switch dependency.relationship {
        case .after:
            break
        case .reply:
            post["reply"] = replyValue(root: parent, parent: parent)
        case .quote:
            let quote = recordEmbedValue(reference: parent)
            if let media = post["embed"], isMediaEmbed(media) {
                post["embed"] = .object([
                    "$type": .string("app.bsky.embed.recordWithMedia"),
                    "record": quote,
                    "media": media,
                ])
            } else {
                post["embed"] = quote
            }
        }
    }

    private func replyValue(
        root: PublishedPostReference,
        parent: PublishedPostReference
    ) -> JSONValue {
        .object([
            "root": strongReferenceValue(root),
            "parent": strongReferenceValue(parent),
        ])
    }

    private func recordEmbedValue(reference: PublishedPostReference) -> JSONValue {
        .object([
            "$type": .string("app.bsky.embed.record"),
            "record": strongReferenceValue(reference),
        ])
    }

    private func strongReferenceValue(_ reference: PublishedPostReference) -> JSONValue {
        .object([
            "uri": .string(reference.uri),
            "cid": .string(reference.cid),
        ])
    }

    private func isMediaEmbed(_ value: JSONValue) -> Bool {
        guard case let .object(embed) = value,
              case let .string(type)? = embed["$type"]
        else { return false }
        return type != "app.bsky.embed.record" && type != "app.bsky.embed.recordWithMedia"
    }

    public func getBrandProfile(did: String) async throws -> BrandProfile {
        if let account = try await store.managedAccount(did: did) {
            return BrandProfile(
                did: did,
                handle: account.handle,
                displayName: account.displayName,
                avatar: account.avatar
            )
        }
        return BrandProfile(did: did)
    }

    public func updateBrandProfile(did: String, profile: UpdateBrandProfileRequest) async throws -> BrandProfile {
        var record: [String: JSONValue] = [
            "$type": .string("app.bsky.actor.profile")
        ]
        if let displayName = profile.displayName {
            record["displayName"] = .string(displayName)
        }
        if let description = profile.description {
            record["description"] = .string(description)
        }
        if let avatar = profile.avatar {
            record["avatar"] = .string(avatar)
        }
        let session = try await authenticatedSession(did: did)
        try await xrpc(
            session: session,
            method: "POST",
            path: "com.atproto.repo.putRecord",
            body: [
                "repo": .string(did),
                "collection": .string("app.bsky.actor.profile"),
                "rkey": .string("self"),
                "validate": .bool(false),
                "record": .object(record),
            ]
        )
        let existing = try await store.managedAccount(did: did)
        let updated = ManagedAccount(
            did: did,
            handle: existing?.handle,
            displayName: profile.displayName ?? existing?.displayName,
            avatar: profile.avatar ?? existing?.avatar,
            pdsEndpoint: existing?.pdsEndpoint,
            status: existing?.status ?? .active,
            isDefault: existing?.isDefault ?? false
        )
        try await store.upsertManagedAccount(updated, now: Timestamp.iso8601())
        return BrandProfile(
            did: did,
            handle: updated.handle,
            displayName: updated.displayName,
            description: profile.description,
            avatar: updated.avatar
        )
    }

    @discardableResult
    private func xrpc(
        session: AuthenticatedATProtoSession,
        method: String,
        path: String,
        body: [String: JSONValue]
    ) async throws -> Data {
        let data = try JSONEncoder().encode(body)
        return try await xrpcData(
            session: session,
            method: method,
            url: "\(session.token.pdsEndpoint)/xrpc/\(path)",
            body: data
        )
    }

    private func xrpcData(
        session: AuthenticatedATProtoSession,
        method: String,
        url: String,
        body: Data?,
        contentType: String = "application/json"
    ) async throws -> Data {
        try await xrpcDataWithNonceAndRefresh(
            session: session,
            method: method,
            url: url,
            body: body,
            contentType: contentType,
            allowRefresh: true
        )
    }

    private func xrpcDataWithNonceAndRefresh(
        session: AuthenticatedATProtoSession,
        method: String,
        url: String,
        body: Data?,
        contentType: String,
        allowRefresh: Bool
    ) async throws -> Data {
        let cachedNonce = await dpopNonces.nonce(for: session.did)
        do {
            return try await xrpcData(
                session: session,
                method: method,
                url: url,
                body: body,
                contentType: contentType,
                dpopNonce: cachedNonce
            )
        } catch {
            if let nonce = dpopNonce(from: error) {
                await dpopNonces.set(nonce, for: session.did)
                do {
                    return try await xrpcData(
                        session: session,
                        method: method,
                        url: url,
                        body: body,
                        contentType: contentType,
                        dpopNonce: nonce
                    )
                } catch {
                    if allowRefresh, isUnauthorized(error) {
                        let refreshed = try await refreshSession(session)
                        return try await xrpcDataWithNonceAndRefresh(
                            session: refreshed,
                            method: method,
                            url: url,
                            body: body,
                            contentType: contentType,
                            allowRefresh: false
                        )
                    }
                    throw error
                }
            }
            if allowRefresh, isUnauthorized(error) {
                let refreshed = try await refreshSession(session)
                return try await xrpcDataWithNonceAndRefresh(
                    session: refreshed,
                    method: method,
                    url: url,
                    body: body,
                    contentType: contentType,
                    allowRefresh: false
                )
            }
            throw error
        }
    }

    private func xrpcData(
        session: AuthenticatedATProtoSession,
        method: String,
        url: String,
        body: Data?,
        contentType: String,
        dpopNonce nonce: String?
    ) async throws -> Data {
        let dpop = try session.dpopKey.proof(
            httpMethod: method,
            url: url,
            accessToken: session.token.accessToken,
            nonce: nonce
        )
        do {
            let response = try await http.data(HTTPRequest(
                url: url,
                method: method,
                headers: [
                    "Authorization": "DPoP \(session.token.accessToken)",
                    "DPoP": dpop,
                    "Content-Type": contentType,
                ],
                body: body
            ))
            if let nonce = response.headers["dpop-nonce"] {
                await dpopNonces.set(nonce, for: session.did)
            }
            return response.body
        } catch {
            if let nonce = dpopNonce(from: error) {
                await dpopNonces.set(nonce, for: session.did)
            }
            throw error
        }
    }

    private func primeDPoPNonce(session: AuthenticatedATProtoSession) async throws {
        _ = try await xrpcData(
            session: session,
            method: "GET",
            url: "\(session.token.pdsEndpoint)/xrpc/com.atproto.server.getSession",
            body: nil
        )
    }

    /// Reads serve stale cache when the PDS rejects them, so without this an
    /// account whose token died between publishes would look healthy until the
    /// next publish attempt. Marking it here is what puts the reconnect prompt
    /// in front of the user while they are still looking at the app.
    private func flagReauthIfNeeded(did: String, error: Error) async {
        guard PDSClientError.isAuthFailure(error) else { return }
        // Best-effort: a read already degraded to cache should not additionally
        // fail because the status write did.
        try? await store.markAccountNeedsReauth(did: did, now: Timestamp.iso8601())
    }

    private func authenticatedSession(did: String) async throws -> AuthenticatedATProtoSession {
        guard let record = try await store.oauthSession(did: did) else {
            throw PDSClientError.notConfigured
        }
        let token = try JSONDecoder().decode(ATProtoTokenPayload.self, from: Data(record.tokenJSON.utf8))
        guard token.sub == did, token.tokenType.lowercased() == "dpop" else {
            throw PDSClientError.notConfigured
        }
        let dpopKey = try DPoPKey(json: record.dpopKeyJSON)
        return AuthenticatedATProtoSession(did: did, handle: record.handle, token: token, dpopKey: dpopKey)
    }

    private func refreshSession(_ session: AuthenticatedATProtoSession) async throws -> AuthenticatedATProtoSession {
        let refreshed = try await tokenRefresh.run(did: session.did) {
            let current = try await authenticatedSession(did: session.did)
            guard current.token.accessToken == session.token.accessToken else { return current }
            return try await refreshCurrentSession(current)
        }
        guard refreshed.token.pdsEndpoint == session.token.pdsEndpoint else {
            throw PDSClientError.publishFailed("The account's PDS changed; reopen scheduling")
        }
        return refreshed
    }

    private func refreshCurrentSession(_ session: AuthenticatedATProtoSession) async throws -> AuthenticatedATProtoSession {
        guard let clientID else {
            throw PDSClientError.publishFailed("OAuth client ID is not configured")
        }
        guard let refreshToken = session.token.refreshToken else {
            throw PDSClientError.publishFailed("OAuth session cannot be refreshed")
        }
        let body = formEncoded([
            "grant_type": "refresh_token",
            "refresh_token": refreshToken,
            "client_id": clientID,
        ])
        let response = try await refreshTokenRequest(
            endpoint: session.token.tokenEndpoint,
            dpopKey: session.dpopKey,
            body: body,
            nonce: nil
        )
        let refreshed = try JSONDecoder().decode(PDSRefreshTokenResponse.self, from: response.body)
        guard refreshed.sub == session.did, refreshed.tokenType.lowercased() == "dpop",
              !refreshed.accessToken.isEmpty else {
            throw PDSClientError.publishFailed("OAuth refresh returned an invalid actor session")
        }
        let token = ATProtoTokenPayload(
            accessToken: refreshed.accessToken,
            refreshToken: refreshed.refreshToken ?? refreshToken,
            tokenType: refreshed.tokenType,
            expiresIn: refreshed.expiresIn,
            scope: refreshed.scope,
            sub: refreshed.sub,
            pdsEndpoint: session.token.pdsEndpoint,
            tokenEndpoint: session.token.tokenEndpoint
        )
        let tokenJSON = try JSONEncoder().encodeString(token)
        try await store.replaceOAuthSessionAfterRefresh(
            OAuthSessionRecord(did: session.did, handle: session.handle, tokenJSON: tokenJSON,
                               dpopKeyJSON: try session.dpopKey.exportJSON()),
            previousAccessToken: session.token.accessToken,
            now: Timestamp.iso8601()
        )
        return try await authenticatedSession(did: session.did)
    }

    private func refreshTokenRequest(
        endpoint: String,
        dpopKey: DPoPKey,
        body: Data,
        nonce: String?
    ) async throws -> HTTPResponseData {
        let proof = try dpopKey.proof(httpMethod: "POST", url: endpoint, nonce: nonce)
        do {
            return try await http.data(HTTPRequest(
                url: endpoint,
                method: "POST",
                headers: [
                    "Content-Type": "application/x-www-form-urlencoded",
                    "DPoP": proof,
                ],
                body: body
            ))
        } catch {
            guard nonce == nil, let nonce = dpopNonce(from: error) else { throw error }
            return try await refreshTokenRequest(
                endpoint: endpoint,
                dpopKey: dpopKey,
                body: body,
                nonce: nonce
            )
        }
    }
}

private actor DPoPNonceStore {
    private var nonces: [String: String] = [:]

    func nonce(for endpoint: String) -> String? {
        nonces[endpoint]
    }

    func set(_ nonce: String, for endpoint: String) {
        nonces[endpoint] = nonce
    }
}

/// Token rotation is single-flight across the worker and requests for the same actor.
private actor SchedulingTokenRefresh {
    private var pending: [String: Task<AuthenticatedATProtoSession, Error>] = [:]

    func run(
        did: String,
        operation: @escaping @Sendable () async throws -> AuthenticatedATProtoSession
    ) async throws -> AuthenticatedATProtoSession {
        if let task = pending[did] { return try await task.value }
        let task = Task { try await operation() }
        pending[did] = task
        defer { pending.removeValue(forKey: did) }
        return try await task.value
    }
}

private struct AuthenticatedATProtoSession: Sendable {
    let did: String
    let handle: String?
    let token: ATProtoTokenPayload
    let dpopKey: DPoPKey
}

private struct PDSRefreshTokenResponse: Codable {
    let accessToken: String
    let refreshToken: String?
    let tokenType: String
    let expiresIn: Int?
    let scope: String
    let sub: String

    enum CodingKeys: String, CodingKey {
        case accessToken = "access_token"
        case refreshToken = "refresh_token"
        case tokenType = "token_type"
        case expiresIn = "expires_in"
        case scope
        case sub
    }
}

private func isUnauthorized(_ error: Error) -> Bool {
    guard case HTTPClientError.badStatus(401, _, _) = error else { return false }
    return true
}

private struct GetRecordResponse<Value: Codable>: Codable {
    let uri: String
    let cid: String?
    let value: Value
}

private struct ListRecordsResponse<Value: Codable>: Codable {
    let records: [GetRecordResponse<Value>]
    let cursor: String?
}

private struct CreateRecordResponse: Codable {
    let uri: String
    let cid: String
}

private struct UploadBlobResponse: Codable {
    let blob: ATProtoBlobReference
}

public extension JSONEncoder {
    func encodeString<T: Encodable>(_ value: T) throws -> String {
        guard let string = String(data: try encode(value), encoding: .utf8) else {
            throw PDSClientError.publishFailed("Could not encode JSON string")
        }
        return string
    }
}
