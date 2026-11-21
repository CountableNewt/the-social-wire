import Foundation
import ReadStateCore

/// Authenticated calls to **`SocialWireAPIEnvironment.baseURL`** (DPoP + access JWT), mirroring PDS **`XRPCClient`** semantics.
@MainActor
final class SocialWireGatewayClient {
    private struct EmptyXRPCInput: Encodable {}

    private struct CircleHiddenItemInput: Encodable {
        let storyId: String
        let hidden: Bool
    }

    private let auth: ATProtoOAuthService
    private let baseURL: URL
    private let urlSession: URLSession
    private let transport: (@Sendable (URLRequest) async throws -> (Data, URLResponse))?

    init(
        auth: ATProtoOAuthService,
        baseURL: URL = SocialWireAPIEnvironment.baseURL,
        urlSession: URLSession = .shared,
        transport: (@Sendable (URLRequest) async throws -> (Data, URLResponse))? = nil
    ) {
        self.auth = auth
        self.baseURL = baseURL
        self.urlSession = urlSession
        self.transport = transport
    }

    func fetchProEntitlement() async throws -> SocialProEntitlement {
        let result = try await authorizedGET(
            path: "/v1/subscription/entitlements",
            query: [:],
            ifNoneMatch: nil
        )
        guard (200 ..< 300).contains(result.statusCode) else {
            throw SocialWireError.badResponse("Pro access check failed (\(result.statusCode)).")
        }
        return try JSONDecoder().decode(SocialProEntitlement.self, from: result.body)
    }

    nonisolated static func preferencesSyncRequest(
        ifNoneMatch: String?,
        forceRefresh: Bool
    ) -> (query: [String: String], ifNoneMatch: String?) {
        // A post-write read must reach the PDS and replace the shared gateway snapshot.
        (forceRefresh ? ["fresh": "true"] : [:], forceRefresh ? nil : ifNoneMatch)
    }

    func fetchSyncPreferences(
        ifNoneMatch: String?,
        forceRefresh: Bool = false
    ) async throws -> GatewayHTTPResult {
        let request = Self.preferencesSyncRequest(ifNoneMatch: ifNoneMatch, forceRefresh: forceRefresh)
        return try await authorizedGET(
            path: SocialWireXRPCMethod.getPreferences,
            query: request.query,
            ifNoneMatch: request.ifNoneMatch
        )
    }

    func fetchSembleCollections(limit: Int = 100, cursor: String? = nil) async throws -> SembleCollectionPage {
        var query = ["limit": String(limit)]
        if let cursor, !cursor.isEmpty { query["cursor"] = cursor }
        let result = try await authorizedGET(
            path: "/v1/semble/collections",
            query: query,
            ifNoneMatch: nil
        )
        guard (200 ..< 300).contains(result.statusCode) else {
            throw SocialWireError.badResponse("Semble collections failed (\(result.statusCode)).")
        }
        return try JSONDecoder().decode(SembleCollectionPage.self, from: result.body)
    }

    func fetchSembleCollection(
        collectionURI: String,
        limit: Int = 100,
        cursor: String? = nil
    ) async throws -> SembleCollectionItemsPage {
        var query = ["collectionUri": collectionURI, "limit": String(limit)]
        if let cursor, !cursor.isEmpty { query["cursor"] = cursor }
        let result = try await authorizedGET(
            path: "/v1/semble/collection",
            query: query,
            ifNoneMatch: nil
        )
        if result.statusCode == 404 {
            throw SocialWireError.sembleCollectionUnavailable(
                "The selected Semble collection no longer exists."
            )
        }
        if result.statusCode == 403 {
            throw SocialWireError.sembleCollectionUnavailable(
                "You no longer own the selected Semble collection."
            )
        }
        guard (200 ..< 300).contains(result.statusCode) else {
            throw SocialWireError.badResponse("Semble collection failed (\(result.statusCode)).")
        }
        return try JSONDecoder().decode(SembleCollectionItemsPage.self, from: result.body)
    }

    func fetchSembleConnections(
        url: String,
        limit: Int = 100,
        cursor: String? = nil
    ) async throws -> SembleConnectionsPage {
        var query = ["url": url, "limit": String(limit)]
        if let cursor, !cursor.isEmpty { query["cursor"] = cursor }
        let result = try await authorizedGET(
            path: "/v1/semble/connections",
            query: query,
            ifNoneMatch: nil
        )
        guard (200 ..< 300).contains(result.statusCode) else {
            throw SocialWireError.badResponse("Semble connections failed (\(result.statusCode)).")
        }
        return try JSONDecoder().decode(SembleConnectionsPage.self, from: result.body)
    }

    func fetchCachedPdsRecord(collection: String, rkey: String, ifNoneMatch: String?) async throws -> GatewayHTTPResult {
        try await authorizedGET(
            path: "/v1/pds/cache/record",
            query: ["collection": collection, "rkey": rkey],
            ifNoneMatch: ifNoneMatch
        )
    }

    func fetchPublicationSidebar(
        phase: PublicationSidebarPhase = .full
    ) async throws -> PublicationSidebarResponseDTO {
        var query: [String: String] = [:]
        if phase != .full {
            query["phase"] = phase.rawValue
        }
        let result = try await authorizedGET(
            path: SocialWireXRPCMethod.getSidebar,
            query: query,
            ifNoneMatch: nil
        )
        guard (200 ..< 300).contains(result.statusCode) else {
            throw SocialWireError.badResponse("Publication sidebar failed (\(result.statusCode)).")
        }
        return try PublicationProjectionJSON.decoder.decode(
            PublicationSidebarResponseDTO.self,
            from: result.body
        )
    }

    func refreshPublicationSidebar() async throws -> PublicationSidebarResponseDTO {
        let payload = try JSONEncoder().encode(EmptyXRPCInput())
        let result = try await authorizedRequest(
            method: "POST",
            path: SocialWireXRPCMethod.refreshSidebar,
            query: [:],
            body: payload,
            contentType: "application/json"
        )
        guard (200 ..< 300).contains(result.statusCode) else {
            throw SocialWireError.badResponse("Publication refresh failed (\(result.statusCode)).")
        }
        return try PublicationProjectionJSON.decoder.decode(
            PublicationSidebarResponseDTO.self,
            from: result.body
        )
    }

    func resolveAddPublication(input: String) async throws -> ResolveAddPublicationResponseDTO {
        let payload = try JSONEncoder().encode(ResolveAddPublicationRequestBody(input: input))
        let result = try await authorizedRequest(
            method: "POST",
            path: SocialWireXRPCMethod.resolvePublication,
            query: [:],
            body: payload,
            contentType: "application/json"
        )
        guard (200 ..< 300).contains(result.statusCode) else {
            throw SocialWireError.badResponse("Publication resolve failed (\(result.statusCode)).")
        }
        return try JSONDecoder().decode(ResolveAddPublicationResponseDTO.self, from: result.body)
    }

    func fetchAppViewEntries(
        scope: PublicationAppViewScopeDTO,
        filter: ReaderFilter,
        cursor: String?,
        limit: Int = 50,
        maxEntries: Int? = nil
    ) async throws -> AppViewEntryListResponse {
        var query: [String: String] = [
            "authorDid": scope.authorDid,
            "filter": filter == .unread ? "unread" : "all",
            "limit": String(limit),
        ]
        if let maxEntries {
            query["maxEntries"] = String(maxEntries)
        } else if let cursor, !cursor.isEmpty {
            query["cursor"] = cursor
        }
        if let publicationAtUri = scope.publicationAtUri, !publicationAtUri.isEmpty {
            query["publicationAtUri"] = publicationAtUri
        }
        if !scope.publicationScopeAtUris.isEmpty {
            query["publicationScopeAtUris"] = scope.publicationScopeAtUris.joined(separator: ",")
        }
        if !scope.publicationSiteUrls.isEmpty {
            query["publicationSiteUrls"] = scope.publicationSiteUrls.joined(separator: ",")
        }

        let result = try await authorizedFeedGET(path: SocialWireXRPCMethod.listEntries, query: query)
        if result.statusCode == 404 {
            throw SocialWireError.appViewUnavailable
        }
        guard (200 ..< 300).contains(result.statusCode) else {
            throw appViewFeedError(result, fallback: "AppView entries failed")
        }
        return try JSONDecoder().decode(AppViewEntryListResponse.self, from: result.body)
    }

    func fetchAggregateAppViewFeed(
        kind: String,
        id: String? = nil,
        filter: ReaderFilter,
        cursor: String?,
        limit: Int = 50
    ) async throws -> AppViewEntryListResponse {
        var query: [String: String] = [
            "kind": kind,
            "filter": filter == .unread ? "unread" : "all",
            "limit": String(limit),
        ]
        if let id, !id.isEmpty { query["id"] = id }
        if let cursor, !cursor.isEmpty { query["cursor"] = cursor }
        let result = try await authorizedFeedGET(
            path: SocialWireXRPCMethod.getFeed,
            query: query
        )
        guard (200 ..< 300).contains(result.statusCode) else {
            throw appViewFeedError(result, fallback: "Aggregate AppView feed failed")
        }
        return try JSONDecoder().decode(AppViewEntryListResponse.self, from: result.body)
    }

    func fetchAppViewEntryDetail(entryId: String) async throws -> EntryDetail? {
        let result = try await authorizedGET(
            path: SocialWireXRPCMethod.getEntry,
            query: ["entryId": entryId],
            ifNoneMatch: nil
        )
        if result.statusCode == 404 {
            return nil
        }
        guard (200 ..< 300).contains(result.statusCode) else {
            throw SocialWireError.badResponse("AppView entry detail failed (\(result.statusCode)).")
        }
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        let dto = try decoder.decode(AppViewEntryDetailDTO.self, from: result.body)
        return dto.toEntryDetail()
    }

    func fetchAppViewUnreadCounts(publicationIds: [String]) async throws -> AppViewUnreadCountsResponse {
        guard !publicationIds.isEmpty else {
            return AppViewUnreadCountsResponse(
                counts: [:],
                generation: nil,
                accuracy: nil,
                countedAt: nil
            )
        }
        let result = try await authorizedGET(
            path: SocialWireXRPCMethod.getUnreadCounts,
            query: [:],
            repeatedQuery: ["publicationIds": publicationIds],
            ifNoneMatch: nil
        )
        if result.statusCode == 404 {
            return AppViewUnreadCountsResponse(
                counts: [:],
                generation: nil,
                accuracy: nil,
                countedAt: nil
            )
        }
        guard (200 ..< 300).contains(result.statusCode) else {
            throw SocialWireError.badResponse("AppView unread counts failed (\(result.statusCode)).")
        }
        return try JSONDecoder().decode(AppViewUnreadCountsResponse.self, from: result.body)
    }

    func upsertReadMark(subjectUri: String, readAt: Date, expectedViewer: String? = nil) async throws {
        let payload = try JSONEncoder().encode(
            AppViewReadMarkBody(subjectUri: subjectUri, readAt: DateFormatters.string(from: readAt))
        )
        let result = try await authorizedRequest(
            method: "POST",
            path: SocialWireXRPCMethod.putReadMark,
            query: [:],
            body: payload,
            contentType: "application/json", expectedViewer: expectedViewer
        )
        guard (200 ..< 300).contains(result.statusCode) else {
            throw SocialWireError.badResponse("AppView read-mark upsert failed (\(result.statusCode)).")
        }
    }

    func pdsReadStateRequest<T: Decodable>(_ method: String, body: Data? = nil, expectedViewer: String? = nil) async throws -> T {
        let methods = ["getReadStateStatus": SocialWireXRPCMethod.getReadStateStatus,
                       "exportReadState": SocialWireXRPCMethod.exportReadState,
                       "prepareReadState": SocialWireXRPCMethod.prepareReadState,
                       "confirmReadState": SocialWireXRPCMethod.confirmReadState]
        guard let path = methods[method] else { throw SocialWireError.unsupported }
        let result = try await authorizedRequest(method: body == nil ? "GET" : "POST",
            path: path, query: [:], body: body,
            contentType: body == nil ? nil : "application/json", expectedViewer: expectedViewer)
        try ReadStateHTTPFailure.checkGatewayCode(result.body, statusCode: result.statusCode)
        if result.statusCode == 404 { throw SocialWireError.appViewUnavailable }
        if result.statusCode == 409 { throw ReadStateSyncFailure.conflict }
        if [401, 403].contains(result.statusCode) { throw ReadStateSyncFailure.reauthorizationRequired }
        guard (200..<300).contains(result.statusCode) else {
            throw SocialWireError.badResponse("Read history sync failed (\(result.statusCode)). Pending changes remain on this device.")
        }
        return try JSONDecoder().decode(T.self, from: result.body)
    }

    func deleteReadMark(subjectUri: String, expectedViewer: String? = nil) async throws {
        let payload = try JSONEncoder().encode(AppViewReadMarkDeleteBody(subjectUri: subjectUri))
        let result = try await authorizedRequest(
            method: "POST",
            path: SocialWireXRPCMethod.deleteReadMark,
            query: [:],
            body: payload,
            contentType: "application/json", expectedViewer: expectedViewer
        )
        guard (200 ..< 300).contains(result.statusCode) else {
            throw SocialWireError.badResponse("AppView read-mark delete failed (\(result.statusCode)).")
        }
    }

    func fetchReadAgeOptions(
        scope: GatewayMarkAllReadScopeDTO,
        timeZone: TimeZone = .current,
        onOptions: @escaping @MainActor ([FeedReadAgeOption]) -> Void = { _ in }
    ) async throws -> FeedReadAgeResponse {
        var query = ["timeZone": timeZone.identifier]
        switch scope {
        case .subscribed: query["kind"] = "subscribed"
        case .following: query["kind"] = "following"
        case .publication(let id):
            query["kind"] = "publication"
            query["publicationId"] = id
        case .folder(let id):
            query["kind"] = "folder"
            query["folderRkey"] = id
        }
        guard var components = URLComponents(
            url: baseURL.appending(path: SocialWireXRPCMethod.getReadAgeOptions),
            resolvingAgainstBaseURL: false
        ) else { throw SocialWireError.invalidURL }
        components.queryItems = query.map { URLQueryItem(name: $0.key, value: $0.value) }
        guard let url = components.url else { throw SocialWireError.invalidURL }
        let session = try await auth.validSession()

        let (bytes, http) = try await authorizedStream(url: url, session: session)
        guard (200 ..< 300).contains(http.statusCode) else {
            bytes.task.cancel()
            throw SocialWireError.badResponse("Age-based read options are unavailable. Please try again.")
        }

        let streamBytes = bytes
        let isStream = http.value(forHTTPHeaderField: "Content-Type")?
            .lowercased().contains("application/x-ndjson") == true
        return try await withTaskCancellationHandler {
            defer { streamBytes.task.cancel() }
            if !isStream {
                var data = Data()
                for try await byte in streamBytes {
                    try Task.checkCancellation()
                    data.append(byte)
                }
                let result = try JSONDecoder().decode(FeedReadAgeResponse.self, from: data)
                onOptions(result.options)
                return result
            }

            return try await FeedReadAgeStreamEvent.consume(streamBytes.lines, onOptions: onOptions)
        } onCancel: {
            streamBytes.task.cancel()
        }
    }

    func markReadBefore(scope: GatewayMarkAllReadScopeDTO, before: String, expectedViewer: String? = nil) async throws -> MarkReadBeforeResponse {
        let payload = try JSONEncoder().encode(GatewayMarkReadBeforeBody(scope: scope, before: before))
        let result = try await authorizedRequest(
            method: "POST",
            path: SocialWireXRPCMethod.markReadBefore,
            query: [:],
            body: payload,
            contentType: "application/json", expectedViewer: expectedViewer
        )
        guard (200 ..< 300).contains(result.statusCode) else {
            throw SocialWireError.badResponse("Couldn't mark older stories as read. Please try again.")
        }
        return try JSONDecoder().decode(MarkReadBeforeResponse.self, from: result.body)
    }

    func markAllRead(scope: GatewayMarkAllReadScopeDTO, expectedViewer: String? = nil) async throws -> GatewayMarkAllReadResponseDTO {
        let payload = try JSONEncoder().encode(GatewayMarkAllReadBody(scope: scope))
        let result = try await authorizedRequest(
            method: "POST",
            path: SocialWireXRPCMethod.markAllRead,
            query: [:],
            body: payload,
            contentType: "application/json", expectedViewer: expectedViewer
        )
        if result.statusCode == 404 {
            throw SocialWireError.appViewUnavailable
        }
        guard (200 ..< 300).contains(result.statusCode) else {
            throw SocialWireError.badResponse("Mark all read failed (\(result.statusCode)).")
        }
        return try JSONDecoder().decode(GatewayMarkAllReadResponseDTO.self, from: result.body)
    }

    func enrollAuthors(dids: [String], feedUrls: [String] = []) async throws -> Int {
        let payload = try JSONEncoder().encode(AppViewEnrollBody(authorDids: dids, feedUrls: feedUrls))
        let result = try await authorizedRequest(
            method: "POST",
            path: SocialWireXRPCMethod.enrollSources,
            query: [:],
            body: payload,
            contentType: "application/json"
        )
        if result.statusCode == 404 {
            throw SocialWireError.appViewUnavailable
        }
        guard (200 ..< 300).contains(result.statusCode) else {
            throw SocialWireError.badResponse("AppView enroll failed (\(result.statusCode)).")
        }
        let decoded = try JSONDecoder().decode(AppViewEnrollResponse.self, from: result.body)
        return decoded.indexed
    }

    func purgeAppViewPrivacyData(expectedViewer: String? = nil) async throws {
        let payload = try JSONEncoder().encode(EmptyXRPCInput())
        let result = try await authorizedRequest(
            method: "POST",
            path: SocialWireXRPCMethod.purgeViewerData,
            query: [:],
            body: payload,
            contentType: "application/json", expectedViewer: expectedViewer
        )
        guard (200 ..< 300).contains(result.statusCode) else {
            throw SocialWireError.badResponse("AppView purge failed (\(result.statusCode)).")
        }
    }

    func consumeBootstrapStream(
        onEvent: @escaping @Sendable (BootstrapStreamEventDTO) async -> Void
    ) async throws {
        guard let url = URL(
            string: "/v1/appview/bootstrap-stream?includeLists=true",
            relativeTo: baseURL
        )?.absoluteURL else {
            throw SocialWireError.invalidURL
        }

        let session = try await auth.validSession()

        let (bytes, http) = try await authorizedStream(url: url, session: session)

        guard (200 ..< 300).contains(http.statusCode) else {
            throw SocialWireError.badResponse("Bootstrap stream failed (\(http.statusCode)).")
        }

        for try await line in bytes.lines {
            try await Self.consumeBootstrapLine(line, onEvent: onEvent)
        }
    }

    /// Explicit refresh of one feed; `onEvent` sees each publication as the server finishes it.
    func refreshFeed(
        kind: String,
        id: String? = nil,
        onEvent: (FeedRefreshStreamEvent) async -> Void
    ) async throws {
        let url = baseURL.appending(path: SocialWireXRPCMethod.refreshFeed)
        let payload = try JSONEncoder().encode(AppViewRefreshFeedBody(kind: kind, id: id))
        let session = try await auth.validSession()
        let (bytes, http) = try await authorizedStream(url: url, session: session, method: "POST", body: payload)
        guard (200 ..< 300).contains(http.statusCode) else {
            bytes.task.cancel()
            if http.statusCode == 404 { throw SocialWireError.appViewUnavailable }
            throw SocialWireError.badResponse("Refresh failed (\(http.statusCode)).")
        }
        try await withTaskCancellationHandler {
            defer { bytes.task.cancel() }
            try await FeedRefreshStreamEvent.consume(bytes.lines, onEvent: onEvent)
        } onCancel: {
            bytes.task.cancel()
        }
    }

    private func authorizedStream(
        url: URL,
        session: AuthSession,
        method: String = "GET",
        body: Data? = nil
    ) async throws -> (URLSession.AsyncBytes, HTTPURLResponse) {
        try await auth.dpop.withNonceSequence(for: session.pdsURL, exclusive: false) { [self] in
            for attempt in 0 ..< 2 {
                try Task.checkCancellation()
                guard auth.session?.did == session.did, auth.session?.accessToken == session.accessToken else {
                    throw ReadStateSyncFailure.accountChanged
                }
                var request = URLRequest(url: url)
                request.httpMethod = method
                request.setValue("application/x-ndjson", forHTTPHeaderField: "Accept")
                if let body {
                    request.httpBody = body
                    request.setValue("application/json", forHTTPHeaderField: "Content-Type")
                }
                try await authorize(&request, session: session)
                let (bytes, raw) = try await urlSession.bytes(for: request)
                guard let response = raw as? HTTPURLResponse else {
                    bytes.task.cancel()
                    throw SocialWireError.badResponse("Missing gateway response.")
                }
                await captureNonces(from: response, session: session)
                if attempt == 0, shouldRetryNonceChallenge(response) {
                    bytes.task.cancel()
                    continue
                }
                return (bytes, response)
            }
            throw SocialWireError.badResponse("Gateway stream exhausted nonce recovery.")
        }
    }

    private static func consumeBootstrapLine(
        _ rawLine: String,
        onEvent: @escaping @Sendable (BootstrapStreamEventDTO) async -> Void
    ) async throws {
        let line = rawLine.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !line.isEmpty else { return }
        let event = try BootstrapStreamNDJSON.decoder.decode(
            BootstrapStreamEventDTO.self,
            from: Data(line.utf8)
        )
        await onEvent(event)
    }

    // MARK: - Private

    private func authorizedGET(
        path: String,
        query: [String: String],
        repeatedQuery: [String: [String]] = [:],
        ifNoneMatch: String?
    ) async throws -> GatewayHTTPResult {
        try await authorizedRequest(
            method: "GET",
            path: path,
            query: query,
            repeatedQuery: repeatedQuery,
            body: nil,
            contentType: nil,
            ifNoneMatch: ifNoneMatch
        )
    }

    private func authorizedFeedGET(
        path: String,
        query: [String: String],
        expectedViewer: String? = nil
    ) async throws -> GatewayHTTPResult {
        let first = try await authorizedRequest(
            method: "GET", path: path, query: query, body: nil, contentType: nil,
            expectedViewer: expectedViewer
        )
        let retryable = !(200 ..< 300).contains(first.statusCode)
            && (try? JSONDecoder().decode(AppViewErrorEnvelopeDTO.self, from: first.body))?.retryable == true
        guard retryable else { return first }
        return try await authorizedRequest(
            method: "GET", path: path, query: query, body: nil, contentType: nil,
            expectedViewer: expectedViewer
        )
    }

    nonisolated static func requiresUpstreamProofs(_ response: GatewayHTTPResult) -> Bool {
        struct Challenge: Decodable { let error: String }
        return response.statusCode == 428
            && (try? JSONDecoder().decode(Challenge.self, from: response.body))?.error == "UpstreamProofRequired"
    }

    private func appViewFeedError(
        _ result: GatewayHTTPResult,
        fallback: String
    ) -> SocialWireError {
        if SocialWireError.isExpiredFeedCursor(statusCode: result.statusCode, body: result.body) {
            return .cursorExpired
        }
        if let envelope = try? JSONDecoder().decode(AppViewErrorEnvelopeDTO.self, from: result.body) {
            return .badResponse(
                "\(envelope.message) Request ID: \(envelope.requestId)"
            )
        }
        let requestId = result.requestIdHeader.map { " Request ID: \($0)" } ?? ""
        return .badResponse("\(fallback) (\(result.statusCode)).\(requestId)")
    }

    /// Classifies already moderated, viewer-scoped Bluesky post views; this does not discover content.
    /// Callers retain the upstream cursor, including when projection returns no matching URIs.
    func projectMedia(_ request: MediaProjectionRequest, expectedViewer: String) async throws -> MediaProjectionResponse {
        guard request.posts.count <= 30 else {
            throw SocialWireError.badResponse("Media projection accepts at most 30 posts.")
        }
        let body = try JSONEncoder().encode(request)
        guard body.count <= 512 * 1024 else {
            throw SocialWireError.badResponse("Media projection request is too large.")
        }
        let response = try await authorizedRequest(method: "POST", path: "/v1/media/project", query: [:],
            body: body, contentType: "application/json", expectedViewer: expectedViewer)
        guard response.statusCode == 200 else {
            throw SocialWireError.badResponse("Media could not load (\(response.statusCode)).")
        }
        let projection = try JSONDecoder().decode(MediaProjectionResponse.self, from: response.body)
        let expectedSelection = request.mode == .photos ? "images" : "portrait-video"
        guard projection.source == "bluesky-following", projection.selection == expectedSelection,
              !projection.durationVerified,
              Set(projection.uris).isSubset(of: Set(request.posts.map(\.uri))),
              Set(projection.uris).count == projection.uris.count else {
            throw SocialWireError.badResponse("Media projection response was invalid.")
        }
        return projection
    }

    func standardSiteArticleEmbed(url: URL, expectedViewer: String) async throws -> StandardSiteSocialEmbed? {
        let response = try await authorizedRequest(method: "GET",
            path: SocialWireXRPCMethod.getArticleEmbed,
            query: ["url": url.absoluteString], body: nil, contentType: nil, expectedViewer: expectedViewer)
        if response.statusCode == 404 { return nil }
        guard response.statusCode == 200 else {
            throw SocialWireError.badResponse("Article preview could not load (\(response.statusCode)).")
        }
        let metadata = try JSONDecoder().decode(StandardSiteSocialEmbed.self, from: response.body)
        guard metadata.isValid else { throw SocialWireError.invalidURL }
        return metadata
    }

    private struct ListsErrorEnvelope: Decodable { let message: String? }

    func standardReaderListsRequest<Value: Decodable>(method: String = "GET", path: String,
        query: [String: String] = [:], body: Data? = nil, expectedViewer: String) async throws -> Value {
        let response = try await authorizedRequest(method: method, path: path, query: query,
            body: body, contentType: body == nil ? nil : "application/json", expectedViewer: expectedViewer)
        guard (200..<300).contains(response.statusCode) else {
            let detail = try? JSONDecoder().decode(ListsErrorEnvelope.self, from: response.body).message
            throw SocialWireError.badResponse(detail ?? "Lists could not load (\(response.statusCode)).")
        }
        return try JSONDecoder().decode(Value.self, from: response.body)
    }

    private func authorizedRequest(
        method: String,
        path: String,
        query: [String: String],
        repeatedQuery: [String: [String]] = [:],
        body: Data?,
        contentType: String?,
        ifNoneMatch: String? = nil,
        expectedViewer: String? = nil
    ) async throws -> GatewayHTTPResult {
        guard var comps = URLComponents(url: baseURL.appending(path: path), resolvingAgainstBaseURL: false) else {
            throw SocialWireError.invalidURL
        }
        if !query.isEmpty || !repeatedQuery.isEmpty {
            comps.queryItems = query.map { URLQueryItem(name: $0.key, value: $0.value) }
                + repeatedQuery.flatMap { key, values in
                    values.map { URLQueryItem(name: key, value: $0) }
                }
        }
        guard let url = comps.url else {
            throw SocialWireError.invalidURL
        }

        let session = try await auth.validSession()
        if let expectedViewer, session.did != expectedViewer { throw ReadStateSyncFailure.accountChanged }
        return try await auth.dpop.withNonceSequence(for: session.pdsURL, exclusive: false, preemptsBackground: method != "GET") { [self] in
            try Task.checkCancellation()

            @MainActor func signedRequest() async throws -> URLRequest {
                try Task.checkCancellation()
                guard auth.session?.did == session.did,
                      auth.session?.accessToken == session.accessToken else {
                    throw ReadStateSyncFailure.accountChanged
                }
                var request = URLRequest(url: url)
                request.httpMethod = method
                request.setValue("application/json", forHTTPHeaderField: "Accept")
                request.httpBody = body
                if let contentType { request.setValue(contentType, forHTTPHeaderField: "Content-Type") }
                if let tag = trimmedEntityTag(ifNoneMatch) { request.setValue(tag, forHTTPHeaderField: "If-None-Match") }
                try await authorize(&request, session: session)
                return request
            }

            var data: Data = Data()
            var response: HTTPURLResponse?
            for nonceAttempt in 0 ..< 2 {
                let (received, raw) = try await send(try await signedRequest())
                guard let http = raw as? HTTPURLResponse else { throw SocialWireError.badResponse("Missing gateway response.") }
                data = received; response = http
                await captureNonces(from: http, session: session)
                if nonceAttempt == 0, shouldRetryNonceChallenge(http) { continue }
                break
            }
            guard let response else { throw SocialWireError.badResponse("Missing gateway response.") }
            return GatewayHTTPResult(statusCode: response.statusCode,
                etagHeader: response.value(forHTTPHeaderField: "ETag"),
                requestIdHeader: response.value(forHTTPHeaderField: "X-Request-ID"), body: data)
        }
    }

    private func send(_ request: URLRequest) async throws -> (Data, URLResponse) {
        if let transport { return try await transport(request) }
        return try await urlSession.data(for: request)
    }

    private func authorize(_ request: inout URLRequest, session: AuthSession) async throws {
        guard let url = request.url else { throw SocialWireError.invalidURL }
        let method = request.httpMethod ?? "GET"

        request.setValue("DPoP \(session.accessToken)", forHTTPHeaderField: "Authorization")
        request.setValue(
            try await auth.dpop.proof(method: method, url: url, accessToken: session.accessToken),
            forHTTPHeaderField: "DPoP"
        )
        let getSessionURL = ATProtoSessionDPoP.getSessionURL(for: session)
        request.setValue(
            try await auth.dpop.proof(
                method: "GET",
                url: getSessionURL,
                accessToken: session.accessToken
            ),
            forHTTPHeaderField: ATProtoSessionDPoP.headerName
        )
    }

    private func captureNonces(from response: HTTPURLResponse, session: AuthSession) async {
        await auth.dpop.updateNonce(from: response)
        await auth.dpop.updateSessionNonce(from: response, session: session)
    }

    private func shouldRetryNonceChallenge(_ response: HTTPURLResponse) -> Bool {
        guard [400, 401].contains(response.statusCode) else { return false }
        return response.value(forHTTPHeaderField: "DPoP-Nonce") != nil
            || ATProtoSessionDPoP.isNonceChallenge(response)
    }

    private func trimmedEntityTag(_ raw: String?) -> String? {
        guard let trimmed = raw?.trimmingCharacters(in: .whitespacesAndNewlines), !trimmed.isEmpty else {
            return nil
        }
        if trimmed.first == "\"" {
            let drop = trimmed.dropFirst().dropLast(trimmed.hasSuffix("\"") ? 1 : 0)
            return drop.isEmpty ? nil : String(drop)
        }
        return trimmed
    }
}
