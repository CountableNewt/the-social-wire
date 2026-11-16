import Foundation

public protocol PDSClient: Sendable {
    func writeRecord<Value: Codable & Sendable>(did: String, collection: String, rkey: String, record: Value) async throws
    func getRecord<Value: Codable & Sendable>(did: String, collection: String, rkey: String, as type: Value.Type) async throws -> Value?
    func listRecords<Value: Codable & Sendable>(did: String, collection: String, as type: Value.Type) async throws -> [String: Value]
    func writeSchedule(did: String, rkey: String, record: SkejScheduleRecord) async throws
    func verifySchedulingConnection(did: String) async throws
    func getScheduleForMutation(did: String, rkey: String) async throws -> SkejScheduleRecord?
    func getSchedule(did: String, rkey: String) async throws -> SkejScheduleRecord?
    func listSchedules(did: String) async throws -> [String: SkejScheduleRecord]
    func deleteSchedule(did: String, rkey: String) async throws
    func writeCalendarEvent(
        did: String,
        rkey: String,
        record: CommunityCalendarEventRecord
    ) async throws -> CalendarEventReference
    func uploadBlob(
        did: String,
        data: Data,
        mimeType: String
    ) async throws -> ATProtoBlobReference
    func publishThread(did: String, record: SkejScheduleRecord) async throws -> [PublishedPostReference]
    func getBrandProfile(did: String) async throws -> BrandProfile
    func updateBrandProfile(did: String, profile: UpdateBrandProfileRequest) async throws -> BrandProfile
}

public struct SQLitePDSClient: PDSClient {
    private let store: SQLiteStore

    public init(store: SQLiteStore) {
        self.store = store
    }

    public func writeSchedule(did: String, rkey: String, record: SkejScheduleRecord) async throws {
        try await writeRecord(did: did, collection: "at.skej.schedule", rkey: rkey, record: record)
    }

    public func getSchedule(did: String, rkey: String) async throws -> SkejScheduleRecord? {
        try await getRecord(did: did, collection: "at.skej.schedule", rkey: rkey, as: SkejScheduleRecord.self)
    }

    public func listSchedules(did: String) async throws -> [String: SkejScheduleRecord] {
        try await listRecords(did: did, collection: "at.skej.schedule", as: SkejScheduleRecord.self)
    }

    public func writeRecord<Value: Codable & Sendable>(did: String, collection: String, rkey: String, record: Value) async throws {
        try await store.writeProtocolRecord(
            did: did,
            collection: collection,
            rkey: rkey,
            record: record,
            now: Timestamp.iso8601()
        )
    }

    public func getRecord<Value: Codable & Sendable>(did: String, collection: String, rkey: String, as type: Value.Type) async throws -> Value? {
        try await store.protocolRecord(did: did, collection: collection, rkey: rkey, as: type)
    }

    public func listRecords<Value: Codable & Sendable>(did: String, collection: String, as type: Value.Type) async throws -> [String: Value] {
        try await store.listProtocolRecords(did: did, collection: collection, as: type)
    }

    public func deleteSchedule(did: String, rkey: String) async throws {
        try await store.deleteScheduleRecord(did: did, rkey: rkey)
    }

    public func writeCalendarEvent(
        did: String,
        rkey: String,
        record: CommunityCalendarEventRecord
    ) async throws -> CalendarEventReference {
        try await writeRecord(
            did: did,
            collection: CalendarEventProjection.collection,
            rkey: rkey,
            record: record
        )
        return CalendarEventReference(
            uri: ATURI.record(did: did, collection: CalendarEventProjection.collection, rkey: rkey),
            cid: "bafycalendar\(rkey)"
        )
    }

    public func uploadBlob(
        did: String,
        data: Data,
        mimeType: String
    ) async throws -> ATProtoBlobReference {
        ATProtoBlobReference(
            ref: ATProtoCIDLink(link: "bafylocal\(data.count)"),
            mimeType: mimeType,
            size: data.count
        )
    }

    public func publishThread(did: String, record: SkejScheduleRecord) async throws -> [PublishedPostReference] {
        try publishedReferences(did: did, record: record)
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
}

public actor InMemoryPDSClient: PDSClient {
    private var schedules: [String: [String: SkejScheduleRecord]] = [:]
    private var genericRecords: [String: [String: [String: Data]]] = [:]
    private var profiles: [String: BrandProfile] = [:]
    private var shouldFailPublish = false

    public init() {}

    public func setShouldFailPublish(_ value: Bool) {
        shouldFailPublish = value
    }

    public func writeRecord<Value: Codable & Sendable>(did: String, collection: String, rkey: String, record: Value) async throws {
        if collection == "at.skej.schedule", let schedule = record as? SkejScheduleRecord {
            try await writeSchedule(did: did, rkey: rkey, record: schedule)
            return
        }
        let data = try JSONEncoder().encode(record)
        var user = genericRecords[did] ?? [:]
        var collectionRecords = user[collection] ?? [:]
        collectionRecords[rkey] = data
        user[collection] = collectionRecords
        genericRecords[did] = user
    }

    public func getRecord<Value: Codable & Sendable>(did: String, collection: String, rkey: String, as type: Value.Type) async throws -> Value? {
        if collection == "at.skej.schedule" {
            return try await getSchedule(did: did, rkey: rkey) as? Value
        }
        guard let data = genericRecords[did]?[collection]?[rkey] else { return nil }
        return try JSONDecoder().decode(type, from: data)
    }

    public func listRecords<Value: Codable & Sendable>(did: String, collection: String, as type: Value.Type) async throws -> [String: Value] {
        if collection == "at.skej.schedule" {
            return (try await listSchedules(did: did)).compactMapValues { $0 as? Value }
        }
        var result: [String: Value] = [:]
        for (rkey, data) in genericRecords[did]?[collection] ?? [:] {
            result[rkey] = try JSONDecoder().decode(type, from: data)
        }
        return result
    }

    public func writeSchedule(did: String, rkey: String, record: SkejScheduleRecord) async throws {
        var user = schedules[did] ?? [:]
        user[rkey] = record
        schedules[did] = user
    }

    public func getSchedule(did: String, rkey: String) async throws -> SkejScheduleRecord? {
        schedules[did]?[rkey]
    }

    public func listSchedules(did: String) async throws -> [String: SkejScheduleRecord] {
        schedules[did] ?? [:]
    }

    public func deleteSchedule(did: String, rkey: String) async throws {
        schedules[did]?[rkey] = nil
    }

    public func writeCalendarEvent(
        did: String,
        rkey: String,
        record: CommunityCalendarEventRecord
    ) async throws -> CalendarEventReference {
        try await writeRecord(
            did: did,
            collection: CalendarEventProjection.collection,
            rkey: rkey,
            record: record
        )
        return CalendarEventReference(
            uri: ATURI.record(did: did, collection: CalendarEventProjection.collection, rkey: rkey),
            cid: "bafycalendar\(rkey)"
        )
    }

    public func uploadBlob(
        did: String,
        data: Data,
        mimeType: String
    ) async throws -> ATProtoBlobReference {
        ATProtoBlobReference(
            ref: ATProtoCIDLink(link: "bafyinmemory\(data.count)"),
            mimeType: mimeType,
            size: data.count
        )
    }

    public func publishThread(did: String, record: SkejScheduleRecord) async throws -> [PublishedPostReference] {
        if shouldFailPublish {
            throw PDSClientError.publishFailed("PDS rejected scheduled record")
        }
        return try publishedReferences(did: did, record: record)
    }

    public func getBrandProfile(did: String) async throws -> BrandProfile {
        profiles[did] ?? BrandProfile(did: did)
    }

    public func updateBrandProfile(did: String, profile: UpdateBrandProfileRequest) async throws -> BrandProfile {
        let existing = profiles[did] ?? BrandProfile(did: did)
        let updated = BrandProfile(
            did: did,
            handle: existing.handle,
            displayName: profile.displayName ?? existing.displayName,
            description: profile.description ?? existing.description,
            avatar: profile.avatar ?? existing.avatar
        )
        profiles[did] = updated
        return updated
    }
}

private func publishedReferences(
    did: String,
    record: SkejScheduleRecord
) throws -> [PublishedPostReference] {
    if record.shadowRecord != nil {
        return [PublishedPostReference(
            rkey: record.publishRkey,
            uri: ATURI.published(did: did, recordType: record.recordType, publishRkey: record.publishRkey),
            cid: "bafy\(record.publishRkey)"
        )]
    }
    guard !record.posts.isEmpty else {
        throw PDSClientError.publishFailed("No record payload to publish")
    }
    return try record.posts.enumerated().map { index, post in
        guard let rkey = post.publishRkey ?? (index == 0 ? record.publishRkey : nil),
              !rkey.isEmpty
        else {
            throw PDSClientError.publishFailed("Post \(index + 1) has no stable publish rkey")
        }
        return PublishedPostReference(
            rkey: rkey,
            uri: ATURI.published(did: did, recordType: record.recordType, publishRkey: rkey),
            cid: "bafy\(rkey)"
        )
    }
}

public enum PDSClientError: Error, Equatable {
    case publishFailed(String)
    case notConfigured

    /// True when `error` means the account's credentials are gone or rejected,
    /// as opposed to the PDS being unreachable. Only these warrant sending the
    /// account back through OAuth; a transient failure resolves on its own.
    public static func isAuthFailure(_ error: Error) -> Bool {
        if case PDSClientError.notConfigured = error { return true }
        guard case HTTPClientError.badStatus(let status, let body, _) = error else { return false }
        if status == 401 || status == 403 { return true }
        guard status == 400,
              let oauthError = try? JSONDecoder().decode(OAuthErrorResponse.self, from: Data(body.utf8))
        else { return false }
        return oauthError.error == "invalid_grant"
    }
}

public extension PDSClient {
    func verifySchedulingConnection(did: String) async throws {}
    func getScheduleForMutation(did: String, rkey: String) async throws -> SkejScheduleRecord? {
        try await getSchedule(did: did, rkey: rkey)
    }
}
