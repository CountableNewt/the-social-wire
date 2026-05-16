import CryptoKit
import Foundation

actor PDSClient {
    private let session: AuthSession
    private var pdsOriginByRepoDid: [String: String] = [:]

    init(session: AuthSession) {
        self.session = session
    }

    static let collectionFolder = "com.thesocialwire.folder"
    static let collectionPubPrefs = "com.thesocialwire.publicationPrefs"
    static let collectionPreferences = "com.thesocialwire.preferences"
    static let collectionPublicationSubscription = "site.standard.graph.subscription"
    static let collectionSkyreaderFeedSubscription = "app.skyreader.feed.subscription"
    static let collectionLatrSavedExternal = "com.latr.saved.external"
    static let collectionLatrSavedItem = "com.latr.saved.item"
    static let collectionEntryReadState = "com.thesocialwire.entryReadState"
    static let collectionEntry = "site.standard.entry"

    private static let publicAppView = URL(string: "https://public.api.bsky.app")!
    private static let plcDirectoryRoot = URL(
        string: ProcessInfo.processInfo.environment["ATPROTO_PLC_URL"] ?? "https://plc.directory"
    )!

    private static let maxFollows = 500
    private static let followPageLimit = 100
    private static let discoveryBatchSize = 25

    func listFolders() async throws -> [FolderModel] {
        let records: ListRecordsResponse<FolderRecord> = try await listAllRecords(collection: Self.collectionFolder)
        return records.records.map {
            FolderModel(
                id: rkeyFromURI($0.uri),
                name: $0.value.name,
                icon: $0.value.icon,
                iconImageURL: $0.value.iconImage.flatMap(URL.init(string:)),
                sortOrder: $0.value.sortOrder ?? 0
            )
        }
        .sorted { $0.sortOrder < $1.sortOrder }
    }

    func createFolder(name: String, icon: String? = nil) async throws {
        let record = FolderRecord(
            type: Self.collectionFolder,
            name: name,
            sortOrder: 0,
            icon: icon,
            iconImage: nil,
            createdAt: ISO8601DateFormatter().string(from: Date())
        )
        try await createRecord(collection: Self.collectionFolder, record: record)
    }

    func deleteFolder(rkey: String) async throws {
        try await deleteRecord(collection: Self.collectionFolder, rkey: rkey)
    }

    func listPublicationPrefs() async throws -> [RepoRecord<PublicationPrefsRecord>] {
        let records: ListRecordsResponse<PublicationPrefsRecord> = try await listAllRecords(
            collection: Self.collectionPubPrefs
        )
        return records.records.map { RepoRecord(uri: $0.uri, cid: $0.cid, value: $0.value) }
    }

    func upsertPublicationPrefs(
        publicationId: String,
        folderId: String? = nil,
        hidden: Bool? = nil,
        existing: RepoRecord<PublicationPrefsRecord>? = nil
    ) async throws {
        let record = PublicationPrefsRecord(
            type: Self.collectionPubPrefs,
            publicationId: publicationId,
            folderId: folderId ?? existing?.value.folderId,
            sortOrder: existing?.value.sortOrder ?? 0,
            hidden: hidden ?? existing?.value.hidden ?? false,
            createdAt: existing?.value.createdAt ?? ISO8601DateFormatter().string(from: Date())
        )
        try await putRecord(
            collection: Self.collectionPubPrefs,
            rkey: existing.map { rkeyFromURI($0.uri) } ?? generateTID(),
            record: record
        )
    }

    func listPublicationSubscriptions() async throws -> [RepoRecord<PublicationSubscriptionRecord>] {
        let records: ListRecordsResponse<PublicationSubscriptionRecord> = try await listAllRecords(
            collection: Self.collectionPublicationSubscription
        )
        return records.records.map { RepoRecord(uri: $0.uri, cid: $0.cid, value: $0.value) }
    }

    func listSkyreaderFeedSubscriptions() async throws -> [RepoRecord<SkyreaderFeedSubscriptionRecord>] {
        let records: ListRecordsResponse<SkyreaderFeedSubscriptionRecord> = try await listAllRecords(
            collection: Self.collectionSkyreaderFeedSubscription
        )
        return records.records.map { RepoRecord(uri: $0.uri, cid: $0.cid, value: $0.value) }
    }

    func createPublicationSubscription(publication: String) async throws {
        let trimmed = publication.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmed.hasPrefix("did:") || trimmed.hasPrefix("at://") else { throw PDSError.invalidPublication }
        try await createRecord(
            collection: Self.collectionPublicationSubscription,
            record: PublicationSubscriptionRecord(type: Self.collectionPublicationSubscription, publication: trimmed)
        )
    }

    func createSkyreaderFeedSubscription(feedURL: String, title: String?) async throws {
        let now = ISO8601DateFormatter().string(from: Date())
        let record = SkyreaderFeedSubscriptionRecord(
            type: Self.collectionSkyreaderFeedSubscription,
            createdAt: now,
            updatedAt: now,
            feedUrl: feedURL,
            title: title,
            siteUrl: nil,
            customIconUrl: nil,
            source: "the-social-wire",
            sourceType: "rss"
        )
        try await createRecord(collection: Self.collectionSkyreaderFeedSubscription, record: record)
    }

    func preferences() async throws -> PreferencesRecord? {
        do {
            let record: GetRecordResponse<PreferencesRecord> = try await getRecord(
                collection: Self.collectionPreferences,
                rkey: "self"
            )
            return record.value
        } catch {
            return nil
        }
    }

    func discoveredPublications(for did: String) async throws -> [PublicationModel] {
        var follows: [FollowProfile] = []
        var cursor: String?

        repeat {
            let page = try await getFollows(actor: did, cursor: cursor)
            follows.append(contentsOf: page.follows)
            cursor = page.cursor
        } while cursor != nil && follows.count < Self.maxFollows

        let discoveredFollows = Array(follows.prefix(Self.maxFollows))
        var publications: [PublicationModel] = []

        for batchStart in stride(from: 0, to: discoveredFollows.count, by: Self.discoveryBatchSize) {
            let batchEnd = min(batchStart + Self.discoveryBatchSize, discoveredFollows.count)
            let batch = Array(discoveredFollows[batchStart..<batchEnd])

            let batchResults = await withTaskGroup(of: PublicationModel?.self) { group in
                for follow in batch {
                    group.addTask {
                        do {
                            let records: ListRecordsResponse<EntryRecordValue> = try await self.listPublicRecords(
                                repo: follow.did,
                                collection: Self.collectionEntry,
                                limit: 1,
                                reverse: false
                            )
                            guard !records.records.isEmpty else { return nil }
                            return PublicationModel(
                                publicationId: follow.did,
                                authorDID: follow.did,
                                title: follow.displayName ?? follow.handle,
                                avatarURL: follow.avatar.flatMap(URL.init(string:)),
                                iconURL: follow.avatar.flatMap(URL.init(string:)),
                                isOwnedByViewer: follow.did == did,
                                source: .standardSite,
                                folderId: nil
                            )
                        } catch {
                            return nil
                        }
                    }
                }

                var results: [PublicationModel] = []
                for await publication in group {
                    if let publication { results.append(publication) }
                }
                return results
            }

            publications.append(contentsOf: batchResults)
        }

        return publications
    }

    func entries(for pubId: String) async throws -> [EntryModel] {
        let records: ListRecordsResponse<EntryRecordValue> = try await listPublicRecords(
            repo: pubId,
            collection: Self.collectionEntry,
            limit: 50,
            reverse: true
        )
        let iso = ISO8601DateFormatter()

        return records.records.map { record in
            let fields = parseEntryValue(record.value)
            return EntryModel(
                entryId: record.uri,
                title: fields.title,
                summary: fields.summary,
                publishedAt: iso.date(from: fields.publishedAt) ?? Date(),
                originalURL: fields.originalURL.flatMap(URL.init(string:)),
                imageURL: fields.image.flatMap(URL.init(string:))
            )
        }
    }

    func entryDetail(id: String) async throws -> EntryDetailModel {
        guard let parsed = parseATURI(id) else { throw PDSError.invalidATURI }

        let record: GetRecordResponse<EntryRecordValue> = try await getPublicRecord(
            repo: parsed.did,
            collection: parsed.collection,
            rkey: parsed.rkey
        )
        let fields = parseEntryValue(record.value)
        let iso = ISO8601DateFormatter()

        return EntryDetailModel(
            entryId: id,
            title: fields.title,
            publishedAt: iso.date(from: fields.publishedAt) ?? Date(),
            contentHTML: fields.contentHTML,
            originalURL: fields.originalURL.flatMap(URL.init(string:)),
            summary: fields.summary,
            imageURL: fields.image.flatMap(URL.init(string:))
        )
    }

    func listEntryReadStates() async throws -> [String: Date] {
        let records: ListRecordsResponse<EntryReadStateRecord> = try await listAllRecords(
            collection: Self.collectionEntryReadState
        )
        let iso = ISO8601DateFormatter()
        var output: [String: Date] = [:]
        for record in records.records {
            guard let date = iso.date(from: record.value.readAt) else { continue }
            output[record.value.subjectUri] = min(output[record.value.subjectUri] ?? date, date)
        }
        return output
    }

    func putEntryReadState(subjectURI: String, readAt: Date) async throws {
        let record = EntryReadStateRecord(
            type: Self.collectionEntryReadState,
            subjectUri: subjectURI,
            readAt: ISO8601DateFormatter().string(from: readAt),
            updatedAt: ISO8601DateFormatter().string(from: Date())
        )
        try await putRecord(collection: Self.collectionEntryReadState, rkey: deterministicRKey(for: subjectURI), record: record)
    }

    func deleteEntryReadState(subjectURI: String) async throws {
        try await deleteRecord(collection: Self.collectionEntryReadState, rkey: deterministicRKey(for: subjectURI))
    }

    func listMergedLatrSaves() async throws -> [SavedLinkModel] {
        let externals: ListRecordsResponse<LatrSavedExternalRecord> = try await listAllRecords(
            collection: Self.collectionLatrSavedExternal
        )
        let items: ListRecordsResponse<LatrSavedItemRecord> = try await listAllRecords(
            collection: Self.collectionLatrSavedItem
        )
        let externalByRkey = Dictionary(uniqueKeysWithValues: externals.records.map { (rkeyFromURI($0.uri), $0) })
        let marker = "/\(Self.collectionLatrSavedExternal)/"
        let iso = ISO8601DateFormatter()

        var rows: [SavedLinkModel] = []
        for item in items.records where item.value.state != "archived" {
            let savedAt = iso.date(from: item.value.savedAt) ?? Date()
            if let range = item.value.subjectUri.range(of: marker) {
                let externalRkey = String(item.value.subjectUri[range.upperBound...])
                guard let external = externalByRkey[externalRkey] else { continue }
                rows.append(SavedLinkModel(
                    id: "external:\(external.value.normalizedUrl)",
                    kind: .external,
                    title: external.value.title.nilIfBlank ?? hostnamePreview(external.value.url),
                    subtitle: hostnamePreview(external.value.url),
                    url: URL(string: external.value.url),
                    excerpt: external.value.excerpt,
                    imageURL: external.value.image.flatMap(URL.init(string:)),
                    normalizedURL: external.value.normalizedUrl,
                    itemRkey: rkeyFromURI(item.uri),
                    externalRkey: externalRkey,
                    subjectURI: item.value.subjectUri,
                    savedAt: savedAt
                ))
            } else {
                rows.append(SavedLinkModel(
                    id: "native:\(item.value.subjectUri)",
                    kind: .native,
                    title: item.value.subjectUri,
                    subtitle: "ATProto item",
                    url: nil,
                    excerpt: nil,
                    imageURL: nil,
                    normalizedURL: nil,
                    itemRkey: rkeyFromURI(item.uri),
                    externalRkey: nil,
                    subjectURI: item.value.subjectUri,
                    savedAt: savedAt
                ))
            }
        }
        return rows.sorted { $0.savedAt > $1.savedAt }
    }

    func saveReadLater(url: URL, title: String?, excerpt: String?) async throws {
        guard let normalized = normalizeHTTPURLToHTTPS(url.absoluteString) else { throw PDSError.invalidURL }
        let externalRkey = deterministicRKey(for: normalized)
        let now = ISO8601DateFormatter().string(from: Date())
        let external = LatrSavedExternalRecord(
            type: Self.collectionLatrSavedExternal,
            url: normalized,
            normalizedUrl: normalized,
            fingerprint: deterministicRKey(for: normalized),
            createdAt: now,
            title: title.nilIfBlank,
            excerpt: excerpt.nilIfBlank,
            site: URL(string: normalized)?.host,
            image: nil,
            language: nil,
            publishedAt: nil,
            author: nil
        )
        try await putRecord(collection: Self.collectionLatrSavedExternal, rkey: externalRkey, record: external)

        let externalURI = "at://\(session.did)/\(Self.collectionLatrSavedExternal)/\(externalRkey)"
        let item = LatrSavedItemRecord(
            type: Self.collectionLatrSavedItem,
            subjectUri: externalURI,
            savedAt: now,
            state: "unread",
            tags: nil,
            note: nil,
            lastOpenedAt: nil
        )
        try await putRecord(collection: Self.collectionLatrSavedItem, rkey: deterministicRKey(for: externalURI), record: item)
    }

    func archiveSavedLink(_ link: SavedLinkModel) async throws {
        let record = LatrSavedItemRecord(
            type: Self.collectionLatrSavedItem,
            subjectUri: link.subjectURI,
            savedAt: ISO8601DateFormatter().string(from: link.savedAt),
            state: "archived",
            tags: nil,
            note: nil,
            lastOpenedAt: nil
        )
        try await putRecord(collection: Self.collectionLatrSavedItem, rkey: link.itemRkey, record: record)
    }

    func deleteSavedLink(_ link: SavedLinkModel) async {
        try? await deleteRecord(collection: Self.collectionLatrSavedItem, rkey: link.itemRkey)
        if let externalRkey = link.externalRkey {
            try? await deleteRecord(collection: Self.collectionLatrSavedExternal, rkey: externalRkey)
        }
    }

    private func listAllRecords<T: Decodable & Sendable>(collection: String) async throws -> ListRecordsResponse<T> {
        var all: [ListRecordsResponse<T>.Record<T>] = []
        var cursor: String?
        repeat {
            let page: ListRecordsResponse<T> = try await listRecords(collection: collection, cursor: cursor)
            all.append(contentsOf: page.records)
            cursor = page.cursor
        } while cursor != nil
        return ListRecordsResponse(records: all, cursor: nil)
    }

    private func listRecords<T: Decodable & Sendable>(
        collection: String,
        cursor: String? = nil
    ) async throws -> ListRecordsResponse<T> {
        let url = session.pdsURL.appendingPathComponent("/xrpc/com.atproto.repo.listRecords")
        var components = URLComponents(url: url, resolvingAgainstBaseURL: false)!
        components.queryItems = [
            URLQueryItem(name: "repo", value: session.did),
            URLQueryItem(name: "collection", value: collection),
            URLQueryItem(name: "limit", value: "100"),
        ]
        if let cursor {
            components.queryItems?.append(URLQueryItem(name: "cursor", value: cursor))
        }
        let request = authenticatedRequest(url: components.url!)
        let (data, response) = try await URLSession.shared.data(for: request)
        guard let http = response as? HTTPURLResponse, (200...299).contains(http.statusCode) else {
            throw PDSError.requestFailed
        }
        return try JSONDecoder().decode(ListRecordsResponse<T>.self, from: data)
    }

    private func getRecord<T: Decodable & Sendable>(collection: String, rkey: String) async throws -> GetRecordResponse<T> {
        let url = session.pdsURL.appendingPathComponent("/xrpc/com.atproto.repo.getRecord")
        var components = URLComponents(url: url, resolvingAgainstBaseURL: false)!
        components.queryItems = [
            URLQueryItem(name: "repo", value: session.did),
            URLQueryItem(name: "collection", value: collection),
            URLQueryItem(name: "rkey", value: rkey),
        ]
        let request = authenticatedRequest(url: components.url!)
        let (data, response) = try await URLSession.shared.data(for: request)
        guard let http = response as? HTTPURLResponse, (200...299).contains(http.statusCode) else {
            throw PDSError.requestFailed
        }
        return try JSONDecoder().decode(GetRecordResponse<T>.self, from: data)
    }

    private func getFollows(actor: String, cursor: String?) async throws -> FollowsResponse {
        let url = Self.publicAppView.appendingPathComponent("/xrpc/app.bsky.graph.getFollows")
        var components = URLComponents(url: url, resolvingAgainstBaseURL: false)!
        components.queryItems = [
            URLQueryItem(name: "actor", value: actor),
            URLQueryItem(name: "limit", value: String(Self.followPageLimit)),
        ]
        if let cursor {
            components.queryItems?.append(URLQueryItem(name: "cursor", value: cursor))
        }
        return try await getPublicJSON(url: components.url!)
    }

    private static func relayHostOmitsListRecordsReverse(pdsOrigin: String) -> Bool {
        guard let host = URL(string: pdsOrigin)?.host?.lowercased() else { return false }
        return host == "atproto.brid.gy" || host.hasSuffix(".brid.gy")
    }

    private func normalizePdsOrigin(_ serviceEndpoint: String) -> String? {
        var output = serviceEndpoint.trimmingCharacters(in: .whitespacesAndNewlines)
        while output.hasSuffix("/") { output.removeLast() }
        return output.isEmpty ? nil : output
    }

    private func resolveRepoDidForPublicRead(_ handleOrDid: String) async throws -> String {
        var target = handleOrDid.trimmingCharacters(in: .whitespacesAndNewlines)
        if target.hasPrefix("@") { target.removeFirst() }
        if target.hasPrefix("did:") { return target }

        var components = URLComponents()
        components.scheme = Self.publicAppView.scheme
        components.host = Self.publicAppView.host
        components.path = "/xrpc/com.atproto.identity.resolveHandle"
        components.queryItems = [URLQueryItem(name: "handle", value: target)]
        guard let url = components.url else { throw PDSError.requestFailed }

        let (data, response) = try await URLSession.shared.data(from: url)
        guard let http = response as? HTTPURLResponse, (200...299).contains(http.statusCode) else {
            throw PDSError.requestFailed
        }
        struct ResolveBody: Decodable { let did: String? }
        let body = try JSONDecoder().decode(ResolveBody.self, from: data)
        guard let did = body.did else { throw PDSError.requestFailed }
        return did
    }

    private func plcDocumentURL(forDid did: String) -> URL? {
        let encoded = did.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? did
        var root = Self.plcDirectoryRoot.absoluteString.trimmingCharacters(in: .whitespacesAndNewlines)
        while root.hasSuffix("/") { root.removeLast() }
        return URL(string: "\(root)/\(encoded)")
    }

    private func pdsOrigin(forRepoDid did: String) async throws -> String {
        if let cached = pdsOriginByRepoDid[did] { return cached }
        guard let plcURL = plcDocumentURL(forDid: did) else { throw PDSError.requestFailed }

        let (data, response) = try await URLSession.shared.data(from: plcURL)
        guard let http = response as? HTTPURLResponse, (200...299).contains(http.statusCode) else {
            throw PDSError.requestFailed
        }
        let obj = try JSONSerialization.jsonObject(with: data) as? [String: Any]
        let services = obj?["service"] as? [[String: Any]]
        let endpoint = services?.compactMap { service -> String? in
            let id = service["id"] as? String
            let type = service["type"] as? String
            guard id == "#atproto_pds" || type == "AtprotoPersonalDataServer" else { return nil }
            return service["serviceEndpoint"] as? String
        }.first
        guard let raw = endpoint, let origin = normalizePdsOrigin(raw) else { throw PDSError.requestFailed }
        pdsOriginByRepoDid[did] = origin
        return origin
    }

    private func sortPublicEntryRecordsNewestFirst(
        _ records: [ListRecordsResponse<EntryRecordValue>.Record<EntryRecordValue>]
    ) -> [ListRecordsResponse<EntryRecordValue>.Record<EntryRecordValue>] {
        records.sorted {
            let lhs = $0.value.publishedAt ?? $0.value.createdAt ?? ""
            let rhs = $1.value.publishedAt ?? $1.value.createdAt ?? ""
            if lhs != rhs { return lhs > rhs }
            return $0.uri < $1.uri
        }
    }

    private func listPublicRecords(
        repo: String,
        collection: String,
        limit: Int,
        cursor: String? = nil,
        reverse: Bool = false
    ) async throws -> ListRecordsResponse<EntryRecordValue> {
        let repoDid = try await resolveRepoDidForPublicRead(repo)
        let pds = try await pdsOrigin(forRepoDid: repoDid)
        let serverReverse = reverse && !Self.relayHostOmitsListRecordsReverse(pdsOrigin: pds)

        var components = URLComponents(string: "\(pds)/xrpc/com.atproto.repo.listRecords")!
        components.queryItems = [
            URLQueryItem(name: "repo", value: repoDid),
            URLQueryItem(name: "collection", value: collection),
            URLQueryItem(name: "limit", value: String(limit)),
            URLQueryItem(name: "reverse", value: serverReverse ? "true" : "false"),
        ]
        if let cursor {
            components.queryItems?.append(URLQueryItem(name: "cursor", value: cursor))
        }
        guard let url = components.url else { throw PDSError.requestFailed }

        var decoded: ListRecordsResponse<EntryRecordValue> = try await getPublicJSON(url: url)
        if reverse && !serverReverse {
            decoded = ListRecordsResponse(records: sortPublicEntryRecordsNewestFirst(decoded.records), cursor: decoded.cursor)
        }
        return decoded
    }

    private func getPublicRecord(repo: String, collection: String, rkey: String) async throws -> GetRecordResponse<EntryRecordValue> {
        let repoDid = try await resolveRepoDidForPublicRead(repo)
        let pds = try await pdsOrigin(forRepoDid: repoDid)

        var components = URLComponents(string: "\(pds)/xrpc/com.atproto.repo.getRecord")!
        components.queryItems = [
            URLQueryItem(name: "repo", value: repoDid),
            URLQueryItem(name: "collection", value: collection),
            URLQueryItem(name: "rkey", value: rkey),
        ]
        guard let url = components.url else { throw PDSError.requestFailed }
        return try await getPublicJSON(url: url)
    }

    private func getPublicJSON<T: Decodable & Sendable>(url: URL) async throws -> T {
        let (data, response) = try await URLSession.shared.data(from: url)
        guard let http = response as? HTTPURLResponse, (200...299).contains(http.statusCode) else {
            throw PDSError.requestFailed
        }
        return try JSONDecoder().decode(T.self, from: data)
    }

    private func createRecord<T: Encodable>(collection: String, record: T) async throws {
        let url = session.pdsURL.appendingPathComponent("/xrpc/com.atproto.repo.createRecord")
        var request = authenticatedRequest(url: url, method: "POST")
        request.httpBody = try JSONEncoder().encode(CreateRecordRequest(repo: session.did, collection: collection, record: record))
        let (_, response) = try await URLSession.shared.data(for: request)
        guard let http = response as? HTTPURLResponse, (200...299).contains(http.statusCode) else {
            throw PDSError.requestFailed
        }
    }

    private func putRecord<T: Encodable>(collection: String, rkey: String, record: T) async throws {
        let url = session.pdsURL.appendingPathComponent("/xrpc/com.atproto.repo.putRecord")
        var request = authenticatedRequest(url: url, method: "POST")
        request.httpBody = try JSONEncoder().encode(PutRecordRequest(repo: session.did, collection: collection, rkey: rkey, record: record))
        let (_, response) = try await URLSession.shared.data(for: request)
        guard let http = response as? HTTPURLResponse, (200...299).contains(http.statusCode) else {
            throw PDSError.requestFailed
        }
    }

    private func deleteRecord(collection: String, rkey: String) async throws {
        let url = session.pdsURL.appendingPathComponent("/xrpc/com.atproto.repo.deleteRecord")
        var request = authenticatedRequest(url: url, method: "POST")
        request.httpBody = try JSONEncoder().encode(DeleteRecordRequest(repo: session.did, collection: collection, rkey: rkey))
        let (_, response) = try await URLSession.shared.data(for: request)
        guard let http = response as? HTTPURLResponse, (200...299).contains(http.statusCode) else {
            throw PDSError.requestFailed
        }
    }

    private func authenticatedRequest(url: URL, method: String = "GET") -> URLRequest {
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.setValue("Bearer \(session.accessToken)", forHTTPHeaderField: "Authorization")
        if method == "POST" {
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        return request
    }

    private func rkeyFromURI(_ uri: String) -> String {
        uri.split(separator: "/").last.map(String.init) ?? uri
    }

    private func parseATURI(_ uri: String) -> (did: String, collection: String, rkey: String)? {
        guard uri.hasPrefix("at://") else { return nil }
        let path = uri.dropFirst("at://".count)
        let parts = path.split(separator: "/", maxSplits: 2).map(String.init)
        guard parts.count == 3 else { return nil }
        return (parts[0], parts[1], parts[2])
    }

    private func parseEntryValue(_ value: EntryRecordValue) -> (
        title: String,
        publishedAt: String,
        contentHTML: String,
        originalURL: String?,
        summary: String?,
        image: String?
    ) {
        (
            title: value.title ?? value.name ?? "Untitled",
            publishedAt: value.publishedAt ?? value.createdAt ?? ISO8601DateFormatter().string(from: Date()),
            contentHTML: value.content ?? value.contentHTML ?? value.text ?? value.body ?? "",
            originalURL: value.url ?? value.externalURL,
            summary: value.summary ?? value.description,
            image: value.image ?? value.thumbnail
        )
    }

    private func generateTID() -> String {
        let timestamp = UInt64(Date().timeIntervalSince1970 * 1_000_000)
        let chars = Array("234567abcdefghijklmnopqrstuvwxyz")
        var value = timestamp
        var result = ""
        for _ in 0..<13 {
            result = String(chars[Int(value & 31)]) + result
            value >>= 5
        }
        return result
    }

    private func deterministicRKey(for value: String) -> String {
        let digest = SHA256.hash(data: Data(value.utf8))
        let alphabet = Array("abcdefghijklmnopqrstuvwxyz234567")
        var output = ""
        var buffer = 0
        var bits = 0

        for byte in digest {
            buffer = (buffer << 8) | Int(byte)
            bits += 8
            while bits >= 5 {
                output.append(alphabet[(buffer >> (bits - 5)) & 31])
                bits -= 5
            }
        }

        if bits > 0 {
            output.append(alphabet[(buffer << (5 - bits)) & 31])
        }

        return String(output.prefix(52))
    }

    private func normalizeHTTPURLToHTTPS(_ value: String) -> String? {
        guard var components = URLComponents(string: value.trimmingCharacters(in: .whitespacesAndNewlines)),
              let scheme = components.scheme?.lowercased(),
              scheme == "http" || scheme == "https",
              components.host != nil
        else { return nil }
        components.scheme = "https"
        components.fragment = nil
        return components.url?.absoluteString
    }

    private func hostnamePreview(_ value: String) -> String {
        URL(string: value)?.host ?? value
    }
}

struct FolderModel: Identifiable, Sendable {
    let id: String
    let name: String
    let icon: String?
    let iconImageURL: URL?
    let sortOrder: Int
}

struct PublicationModel: Identifiable, Sendable {
    let publicationId: String
    let authorDID: String
    let title: String
    let avatarURL: URL?
    var iconURL: URL?
    var isOwnedByViewer: Bool = false
    var source: PublicationSource = .standardSite
    var folderId: String?

    var id: String { publicationId }
}

enum PublicationSource: Sendable {
    case standardSite
    case rss
}

struct EntryModel: Identifiable, Sendable {
    let entryId: String
    let title: String
    let summary: String?
    let publishedAt: Date
    let originalURL: URL?
    let imageURL: URL?

    var id: String { entryId }
}

struct EntryDetailModel: Sendable {
    let entryId: String
    let title: String
    let publishedAt: Date
    let contentHTML: String
    let originalURL: URL?
    let summary: String?
    let imageURL: URL?
}

enum SavedLinkKind: Sendable {
    case external
    case native
}

struct SavedLinkModel: Identifiable, Sendable {
    let id: String
    let kind: SavedLinkKind
    let title: String
    let subtitle: String
    let url: URL?
    let excerpt: String?
    let imageURL: URL?
    let normalizedURL: String?
    let itemRkey: String
    let externalRkey: String?
    let subjectURI: String
    let savedAt: Date
}

struct RepoRecord<T: Sendable>: Sendable {
    let uri: String
    let cid: String
    let value: T
}

private struct ListRecordsResponse<T: Decodable & Sendable>: Decodable, Sendable {
    struct Record<V: Decodable & Sendable>: Decodable, Sendable {
        let uri: String
        let cid: String
        let value: V
    }
    let records: [Record<T>]
    let cursor: String?

    init(records: [Record<T>], cursor: String? = nil) {
        self.records = records
        self.cursor = cursor
    }
}

private struct GetRecordResponse<T: Decodable & Sendable>: Decodable, Sendable {
    let uri: String
    let cid: String
    let value: T
}

private struct FollowsResponse: Decodable, Sendable {
    let follows: [FollowProfile]
    let cursor: String?
}

private struct FollowProfile: Decodable, Sendable {
    let did: String
    let handle: String
    let displayName: String?
    let avatar: String?
}

private struct EntryRecordValue: Decodable, Sendable {
    let title: String?
    let name: String?
    let publishedAt: String?
    let createdAt: String?
    let content: String?
    let contentHTML: String?
    let text: String?
    let body: String?
    let url: String?
    let externalURL: String?
    let summary: String?
    let description: String?
    let image: String?
    let thumbnail: String?

    enum CodingKeys: String, CodingKey {
        case title, name, publishedAt, createdAt, content, text, body, url, summary, description, image, thumbnail
        case contentHTML = "contentHtml"
        case externalURL = "externalUrl"
    }
}

private struct FolderRecord: Codable, Sendable {
    let type: String
    let name: String
    let sortOrder: Int?
    let icon: String?
    let iconImage: String?
    let createdAt: String

    enum CodingKeys: String, CodingKey {
        case type = "$type", name, sortOrder, icon, iconImage, createdAt
    }
}

struct PublicationPrefsRecord: Codable, Sendable {
    let type: String
    let publicationId: String
    let folderId: String?
    let sortOrder: Int?
    let hidden: Bool?
    let createdAt: String

    enum CodingKeys: String, CodingKey {
        case type = "$type", publicationId, folderId, sortOrder, hidden, createdAt
    }
}

struct PublicationSubscriptionRecord: Codable, Sendable {
    let type: String
    let publication: String

    enum CodingKeys: String, CodingKey {
        case type = "$type", publication
    }
}

struct SkyreaderFeedSubscriptionRecord: Codable, Sendable {
    let type: String
    let createdAt: String
    let updatedAt: String?
    let feedUrl: String?
    let title: String?
    let siteUrl: String?
    let customIconUrl: String?
    let source: String?
    let sourceType: String?

    enum CodingKeys: String, CodingKey {
        case type = "$type", createdAt, updatedAt, feedUrl, title, siteUrl, customIconUrl, source, sourceType
    }
}

struct PreferencesRecord: Codable, Sendable {
    let type: String
    let readLaterService: String?
    let createdAt: String
    let updatedAt: String

    enum CodingKeys: String, CodingKey {
        case type = "$type", readLaterService, createdAt, updatedAt
    }
}

private struct EntryReadStateRecord: Codable, Sendable {
    let type: String
    let subjectUri: String
    let readAt: String
    let updatedAt: String?

    enum CodingKeys: String, CodingKey {
        case type = "$type", subjectUri, readAt, updatedAt
    }
}

private struct LatrSavedExternalRecord: Codable, Sendable {
    let type: String
    let url: String
    let normalizedUrl: String
    let fingerprint: String
    let createdAt: String
    let title: String?
    let excerpt: String?
    let site: String?
    let image: String?
    let language: String?
    let publishedAt: String?
    let author: String?

    enum CodingKeys: String, CodingKey {
        case type = "$type", url, normalizedUrl, fingerprint, createdAt, title, excerpt, site, image, language, publishedAt, author
    }
}

private struct LatrSavedItemRecord: Codable, Sendable {
    let type: String
    let subjectUri: String
    let savedAt: String
    let state: String?
    let tags: [String]?
    let note: String?
    let lastOpenedAt: String?

    enum CodingKeys: String, CodingKey {
        case type = "$type", subjectUri, savedAt, state, tags, note, lastOpenedAt
    }
}

private struct CreateRecordRequest<T: Encodable>: Encodable {
    let repo: String
    let collection: String
    let record: T
}

private struct PutRecordRequest<T: Encodable>: Encodable {
    let repo: String
    let collection: String
    let rkey: String
    let record: T
}

private struct DeleteRecordRequest: Encodable {
    let repo: String
    let collection: String
    let rkey: String
}

enum PDSError: Error {
    case requestFailed
    case invalidATURI
    case invalidURL
    case invalidPublication
}

private extension Optional where Wrapped == String {
    var nilIfBlank: String? {
        guard let value = self else { return nil }
        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }
}
