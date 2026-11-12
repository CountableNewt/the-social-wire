import Crypto
import Foundation
import Logging
import SkejKit
import Testing
@testable import SkejBackend

private let did = "did:plc:example"
private let other = "did:plc:other"

@Test func authenticatedActorCanOnlyAccessItsOwnQueue() throws {
    #expect(try scheduleActor(did, requested: nil) == did)
    #expect(try scheduleActor(did, requested: did) == did)
    #expect(throws: APIError.self) { try scheduleActor(did, requested: other) }
    #expect(throws: (any Error).self) { try scheduleActor(nil, requested: did) }
}

@Test func importDoesNotOverwriteLocalJobsOrClaimAnUncertainPublication() async throws {
    let store = try SQLiteStore(path: ":memory:")
    try await store.migrate()
    let timestamp = "2026-08-01T00:00:00.000Z"
    let record = SkejScheduleRecord(scheduledAt: timestamp, createdAt: timestamp, updatedAt: timestamp,
        status: .scheduled, publishRkey: "3npgloz4nm72k", posts: [PostPlan(text: "Scheduled")])
    let scheduled = try #require(legacyJob(did: did, rkey: "original", record: record))
    var attempting = record
    attempting.retry.attemptCount = 1
    let uncertain = try #require(legacyJob(did: did, rkey: "attempted", record: attempting))
    var canceled = scheduled
    canceled.status = .canceled
    try await store.upsertScheduleJob(canceled, now: timestamp)

    try await store.importLegacyScheduleQueue(did: did, jobs: [scheduled, uncertain], now: timestamp)
    try await store.importLegacyScheduleQueue(did: did, jobs: [scheduled], now: timestamp)

    #expect(try await store.hasImportedLegacyScheduleQueue(did: did))
    #expect(try await store.scheduleJob(did: did, rkey: "original")?.status == .canceled)
    #expect(try await store.scheduleJob(did: did, rkey: "attempted")?.status == .publishing)
    #expect(try await store.claimDueJobs(now: timestamp).isEmpty)
}

@Test func retryUsesTheSameScheduleIdentityAndResubmissionAfterCancelGetsAnother() {
    let first = scheduleRecordKey("article-rkey", previousCancellation: nil)
    #expect(scheduleRecordKey("article-rkey", previousCancellation: nil) == first)
    #expect(scheduleRecordKey("article-rkey", previousCancellation: first) != first)
}

@Test func lookupRecoversAStoredSubmissionWhoseQueueWriteWasInterrupted() async throws {
    let store = try SQLiteStore(path: ":memory:")
    try await store.migrate()
    let pds = SQLitePDSClient(store: store)
    let config = AppConfig(publicOrigin: "https://api.example.org", network: .init())
    let logger = Logger(label: "scheduling-tests")
    let backend = EmbeddedSkejBackend(config: config, store: store, pdsClient: pds,
        oauthClient: ATProtoOAuthClient(config: config), readLegacyQueue: nil,
        worker: ScheduleWorker(store: store, pdsClient: pds, logger: logger),
        submissionKey: SymmetricKey(size: .bits256), logger: logger)
    let future = Timestamp.iso8601(Date().addingTimeInterval(600))
    let input = SkejScheduleRecord(scheduledAt: future, createdAt: future, updatedAt: future,
        status: .scheduled, publishRkey: "client-submission", posts: [PostPlan(text: "Hello")])
    let created = try await backend.create(did: did, input: input)

    // Model the state after the PDS accepted a request and before SQLite projected it.
    try await store.deleteScheduleJob(did: did, rkey: created.rkey)
    try await store.rememberScheduleSubmission(did: did, rkey: created.rkey, now: Timestamp.iso8601())
    await backend.recoverPendingSubmission()

    let readback = try await backend.schedules(did: did)
    #expect(readback.map(\.rkey) == [created.rkey])
    #expect(readback.first?.record.publishRkey == created.record.publishRkey)
    #expect(try await backend.create(did: did, input: input).rkey == created.rkey)
    #expect(try await backend.schedules(did: other).isEmpty)

    let publishTime = try #require(Timestamp.date(from: future))
    await backend.worker.runTick(now: publishTime)
    let published = try #require(try await backend.schedules(did: did).first)
    #expect(published.status == .published)
    #expect(published.publishedUri == ATURI.published(did: did, recordType: "app.bsky.feed.post", publishRkey: created.record.publishRkey))
}
