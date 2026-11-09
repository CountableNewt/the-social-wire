import Foundation
import Logging
import Testing
@testable import ThinAppViewCore

@Suite("Authoritative entry verification")
struct AuthoritativeContentVerifierTests {
  private let uri = "at://did:plc:aaaaaaaaaaaaaaaaaaaaaaaa/site.standard.document/one"

  @Test func refreshesPresentRecordAndBindsFreshnessToLocalCID() async throws {
    let f = try Fixture()
    defer { f.cleanup() }
    try await f.seed(uri)
    let transport = AuthoritativeContentTestTransport()
    let verifier = f.verifier(transport)
    #expect(try await verifier.verify(entryURI: uri))
    #expect(try await f.store.fetchContentItem(uri: uri)?.title == "Verified article")
    #expect(try await verifier.verify(entryURI: uri))
    #expect(await transport.recordCalls == 1)
    try await f.seed(uri, cid: "new-local-cid")
    #expect(try await verifier.verify(entryURI: uri))
    #expect(await transport.recordCalls == 2)
    await verifier.shutdown()
  }

  @Test func onlyExplicitMissingDeletesAndInvalidatesOldPublicationCache() async throws {
    let f = try Fixture(); defer { f.cleanup() }
    try await f.seed(uri)
    let publication = "at://did:plc:aaaaaaaaaaaaaaaaaaaaaaaa/site.standard.publication/main"
    try await f.cache.storeFirstPageJSON(viewerDid: "viewer", publicationId: publication,
      jsonBody: "cached deleted entry", expiresAt: Date().addingTimeInterval(60))
    let verifier = f.verifier(AuthoritativeContentTestTransport(.deleted))
    #expect(try await verifier.verify(entryURI: uri) == false)
    #expect(try await f.store.fetchContentItem(uri: uri) == nil)
    #expect(try await f.cache.cachedFirstPageJSON(viewerDid: "viewer", publicationId: publication) == nil)
    await verifier.shutdown()
  }

  @Test(arguments: [AuthoritativeContentTestTransport.Mode.unavailable, .empty404, .wrongError,
    .tampered, .wrongURI, .wrongType, .wrongDID, .unsafePDS, .movedPDS, .redirect, .ambiguousError])
  func preservesProjectionWithoutExactCurrentAuthority(mode: AuthoritativeContentTestTransport.Mode) async throws {
    let f = try Fixture(); defer { f.cleanup() }
    try await f.seed(uri)
    let verifier = f.verifier(AuthoritativeContentTestTransport(mode))
    await #expect(throws: (any Error).self) { try await verifier.verify(entryURI: uri) }
    #expect(try await f.store.fetchContentItem(uri: uri)?.title == "Original article")
    await verifier.shutdown()
  }

  @Test func missingResponseCannotDeleteConcurrentNewCID() async throws {
    let f = try Fixture(); defer { f.cleanup() }
    try await f.seed(uri)
    let transport = AuthoritativeContentTestTransport(.deleted)
    await transport.setBeforeSecondIdentity { try await f.seed(uri, cid: "newer", title: "Newer PDS version") }
    let verifier = f.verifier(transport)
    await #expect(throws: AuthoritativeContentVerificationError.unavailable) { try await verifier.verify(entryURI: uri) }
    #expect(try await f.store.fetchContentItem(uri: uri)?.title == "Newer PDS version")
    await verifier.shutdown()
  }

  @Test func cacheExpiryAndDifferentURIAreNotRefreshedByHits() async throws {
    let f = try Fixture(); defer { f.cleanup() }
    try await f.seed(uri)
    let transport = AuthoritativeContentTestTransport()
    let verifier = f.verifier(transport, cacheTTL: 0.03)
    #expect(try await verifier.verify(entryURI: uri))
    #expect(try await verifier.verify(entryURI: uri))
    #expect(await transport.recordCalls == 1)
    try await Task.sleep(for: .milliseconds(40))
    #expect(try await verifier.verify(entryURI: uri))
    #expect(await transport.recordCalls == 2)
    let other = uri.replacingOccurrences(of: "/one", with: "/two")
    try await f.seed(other)
    #expect(try await verifier.verify(entryURI: other))
    #expect(await transport.recordCalls == 3)
    await verifier.shutdown()
  }

  @Test func boundsCoalescedWaitersAndJoinsCancellationOnShutdown() async throws {
    let f = try Fixture(); defer { f.cleanup() }
    try await f.seed(uri)
    let transport = AuthoritativeContentTestTransport(.stalled)
    let verifier = f.verifier(transport, maximumWaiters: 1)
    let pending = Task { try await verifier.verify(entryURI: uri) }
    for _ in 0..<100 where await transport.recordCalls == 0 { try await Task.sleep(for: .milliseconds(5)) }
    await #expect(throws: AuthoritativeContentVerificationError.capacityExceeded) { try await verifier.verify(entryURI: uri) }
    await verifier.shutdown()
    await #expect(throws: CancellationError.self) { try await pending.value }
    await #expect(throws: CancellationError.self) { try await verifier.verify(entryURI: uri) }
    #expect(try await f.store.fetchContentItem(uri: uri)?.title == "Original article")
  }

  @Test func coalescesSameEntryAndBoundsIndependentChecks() async throws {
    let f = try Fixture(); defer { f.cleanup() }
    try await f.seed(uri)
    let transport = AuthoritativeContentTestTransport(.delayed)
    let verifier = f.verifier(transport)
    async let one = verifier.verify(entryURI: uri)
    async let two = verifier.verify(entryURI: uri)
    let results = try await [one, two]
    #expect(results == [true, true])
    #expect(await transport.recordCalls == 1)
    await verifier.shutdown()
    let blocked = f.verifier(AuthoritativeContentTestTransport(.stalled), maximumConcurrent: 1)
    let pending = Task { try await blocked.verify(entryURI: uri) }
    try await Task.sleep(for: .milliseconds(20))
    await #expect(throws: AuthoritativeContentVerificationError.capacityExceeded) {
      try await blocked.verify(entryURI: uri.replacingOccurrences(of: "/one", with: "/two"))
    }
    pending.cancel()
    await #expect(throws: CancellationError.self) { try await pending.value }
    await blocked.shutdown()
  }

  @Test func absoluteDeadlineCancelsHTTPBeforeAnyProjectionWrite() async throws {
    let f = try Fixture(); defer { f.cleanup() }
    try await f.seed(uri)
    let verifier = f.verifier(AuthoritativeContentTestTransport(.stalled), timeout: .milliseconds(40))
    await #expect(throws: AuthoritativeContentVerificationError.unavailable) { try await verifier.verify(entryURI: uri) }
    #expect(try await f.store.fetchContentItem(uri: uri)?.title == "Original article")
    await verifier.shutdown()
  }

  @Test(arguments: ["rss://one", "at://did:plc:aaaaaaaaaaaaaaaaaaaaaaaa/app.bsky.feed.post/one",
    "at://did:plc:aaaaaaaaaaaaaaaaaaaaaaaa/site.standard.document/one?bad", "at://did:plc:bad/site.standard.document/one"])
  func rejectsOtherScopes(uri: String) async throws {
    let f = try Fixture(); defer { f.cleanup() }
    let transport = AuthoritativeContentTestTransport()
    let verifier = f.verifier(transport)
    await #expect(throws: AuthoritativeContentVerificationError.invalidScope) { try await verifier.verify(entryURI: uri) }
    #expect(await transport.identityCalls == 0)
    await verifier.shutdown()
  }

  private struct Fixture: Sendable {
    let path: String
    let store: SQLiteThinAppViewStore
    let cache: SQLiteAppViewProjectionCacheStore
    let indexer: ThinAppViewIndexer
    init() throws {
      path = FileManager.default.temporaryDirectory.appendingPathComponent("content-proof-\(UUID()).sqlite").path
      let logger = Logger(label: "content-proof.test")
      store = try SQLiteThinAppViewStore(path: path, logger: logger)
      cache = try SQLiteAppViewProjectionCacheStore(path: path, logger: logger)
      indexer = ThinAppViewIndexer(store: store, config: .fromEnvironment([:]), logger: logger, projectionCache: cache)
    }
    func cleanup() { try? FileManager.default.removeItem(atPath: path) }
    func seed(_ uri: String, cid: String = "old", title: String = "Original article") async throws {
      try await store.upsertContentItem(IndexedContentItem(uri: uri, cid: cid,
        authorDid: "did:plc:aaaaaaaaaaaaaaaaaaaaaaaa", collection: "site.standard.document", createdAt: Date(), indexedAt: Date(),
        publicationSite: "at://did:plc:aaaaaaaaaaaaaaaaaaaaaaaa/site.standard.publication/main",
        render: ContentRenderFields(title: title, publishedAt: "2026-11-05T00:00:00Z"), expiresAt: Date().addingTimeInterval(3600)))
    }
    func verifier(_ transport: AuthoritativeContentTestTransport, cacheTTL: TimeInterval = 60,
      maximumConcurrent: Int = 4, maximumWaiters: Int = 8, timeout: Duration = .seconds(8)) -> AuthoritativeContentVerifier {
      AuthoritativeContentVerifier(check: AuthoritativeContentCheck(store: store, indexer: indexer,
        transport: transport, plcURL: "https://plc.directory", timeout: timeout), cacheTTL: cacheTTL,
        maximumConcurrentChecks: maximumConcurrent, maximumWaitersPerEntry: maximumWaiters)
    }
  }
}
