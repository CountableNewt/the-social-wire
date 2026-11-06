import Foundation
import GatewayCore
import Hummingbird
import Logging
import Testing
import ThinAppViewCore

@testable import AppView

@Suite("Entry detail authority")
struct ThinAppViewEntryVerificationTests {
  private let uri = "at://did:plc:aaaaaaaaaaaaaaaaaaaaaaaa/site.standard.document/one"
  private let auth = AuthContext(did: "did:plc:viewer", authorizationForwardingValue: "DPoP token", dpopProof: "proof")

  @Test func verifiedDeletionReturns404WithoutServingCachedBody() async throws {
    let f = try Fixture(); defer { f.cleanup() }
    try await f.seed(uri)
    let verifier = Verifier { _ in false }
    let service = ThinAppViewReadService(store: f.store, contentVerifier: verifier, logger: f.logger)
    do { _ = try await service.entryDetail(auth: auth, entryId: uri); Issue.record("Deleted record served") }
    catch let error as HTTPError { #expect(error.status == .notFound) }
    #expect(await verifier.calls == 1)
  }

  @Test func transientFailurePreservesIndexedBodyAndPresentRefreshUsesNewBody() async throws {
    let f = try Fixture(); defer { f.cleanup() }
    try await f.seed(uri)
    let unavailable = Verifier { _ in throw Failure.unavailable }
    let old = try await ThinAppViewReadService(store: f.store, contentVerifier: unavailable, logger: f.logger)
      .entryDetail(auth: auth, entryId: uri)
    #expect(old.title == "Original")
    let refreshed = Verifier { uri in try await f.seed(uri, title: "Refreshed"); return true }
    let current = try await ThinAppViewReadService(store: f.store, contentVerifier: refreshed, logger: f.logger)
      .entryDetail(auth: auth, entryId: uri)
    #expect(current.title == "Refreshed")
  }

  @Test func cancellationPropagatesAndRssDoesNotEnterPDSVerifier() async throws {
    let f = try Fixture(); defer { f.cleanup() }
    try await f.seed(uri)
    let cancelled = Verifier { _ in throw CancellationError() }
    // CancellationError is propagated even when a transport reports it before the enclosing task's flag.
    let service = ThinAppViewReadService(store: f.store, contentVerifier: cancelled, logger: f.logger)
    let pending = Task { try await service.entryDetail(auth: auth, entryId: uri) }
    pending.cancel()
    await #expect(throws: CancellationError.self) { try await pending.value }
    let rss = "rss://publisher/entry"
    try await f.seed(rss)
    let detail = try await service.entryDetail(auth: auth, entryId: rss)
    #expect(detail.entryId == rss)
  }

  private enum Failure: Error { case unavailable }
  private actor Verifier: AuthoritativeContentVerifying {
    var calls = 0
    let action: @Sendable (String) async throws -> Bool
    init(_ action: @escaping @Sendable (String) async throws -> Bool) { self.action = action }
    func verify(entryURI: String) async throws -> Bool { calls += 1; return try await action(entryURI) }
  }
  private struct Fixture: Sendable {
    let path: String
    let store: SQLiteThinAppViewStore
    let logger = Logger(label: "entry-verification.test")
    init() throws {
      path = FileManager.default.temporaryDirectory.appendingPathComponent("entry-verification-\(UUID()).sqlite").path
      store = try SQLiteThinAppViewStore(path: path, logger: Logger(label: "entry-verification.store"))
    }
    func cleanup() { try? FileManager.default.removeItem(atPath: path) }
    func seed(_ uri: String, title: String = "Original") async throws {
      try await store.upsertContentItem(IndexedContentItem(uri: uri, cid: "fixture",
        authorDid: "did:plc:aaaaaaaaaaaaaaaaaaaaaaaa", collection: "site.standard.document", createdAt: Date(), indexedAt: Date(),
        publicationSite: nil, render: ContentRenderFields(title: title, publishedAt: "2026-11-05T00:00:00Z"),
        expiresAt: Date().addingTimeInterval(3600)))
    }
  }
}
