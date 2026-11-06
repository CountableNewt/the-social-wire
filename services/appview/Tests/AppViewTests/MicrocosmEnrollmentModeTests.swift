import AsyncHTTPClient
import Foundation
import GatewayCore
import Logging
import Testing
import ThinAppViewCore
@testable import AppView

@Suite struct MicrocosmEnrollmentModeTests {
  private actor Probe {
    var provider = 0
    var recent = 0
    var history = 0
    func discover() { provider += 1 }
    func enumerate(recentOnly: Bool) -> Int {
      if recentOnly { recent += 1 } else { history += 1 }
      return 3
    }
  }
  private struct Provider: PublicContentDiscovering {
    let probe: Probe
    var fails = false
    func discover(scope: PublicContentDiscoveryScope) async throws -> Int {
      await probe.discover()
      if fails { throw PublicContentDiscoveryError.unavailable }
      return 2
    }
  }

  @Test(arguments: [false, true])
  func microcosmEnrollmentUsesKnownScopesWithoutAnyPDSContentJobs(providerFails: Bool) async throws {
    let probe = Probe(), logger = Logger(label: "microcosm-mode.test")
    let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).path
    defer { try? FileManager.default.removeItem(atPath: path) }
    let store = try SQLiteThinAppViewStore(path: path, logger: logger)
    let config = ThinAppViewConfig.fromEnvironment(["ENABLE_THIN_APPVIEW": "true"])
    let indexer = ThinAppViewIndexer(store: store, config: config, logger: logger)
    let http = HTTPClient(eventLoopGroupProvider: .singleton)
    let author = "did:plc:aaaaaaaaaaaaaaaaaaaaaaaa"
    let scope = try PublicContentDiscoveryScope(authorDid: author, publicationKeys: ["https://publisher.example"])
    let service = ThinAppViewEnrollService(store: store, indexer: indexer, httpClient: http,
      plcURL: "https://plc.directory", config: config, logger: logger,
      publicContentProvider: Provider(probe: probe, fails: providerFails), publicContentDiscoveryMode: .microcosm,
      knownPublicContentScopes: { _, authors in authors.contains(author) ? [scope] : [] },
      publicContentPDS: { _, recent in await probe.enumerate(recentOnly: recent) })
    let auth = AuthContext(did: author, authorizationForwardingValue: "DPoP test", dpopProof: nil)
    // Both enrollment shapes return promptly and only schedule known publication backlinks.
    #expect(try await service.enroll(auth: auth, authorDids: [author]) == 0)
    #expect(try await service.enroll(auth: auth, authorDids: [author], recentOnly: false) == 0)
    let count = try await service.refreshPublicContent(authorDid: author, publicationAtUri: nil,
      publicationSiteUrls: ["https://publisher.example"])
    #expect(count == 0 || count == 2)
    // Missing publication keys never become an author-wide PDS scan.
    #expect(try await service.refreshPublicContent(authorDid: author, publicationAtUri: nil) == 0)
    #expect(try await service.enroll(auth: auth, authorDids: ["did:plc:bbbbbbbbbbbbbbbbbbbbbbbb"]) == 0)
    await service.shutdown()
    #expect(await probe.provider == 1)
    #expect(await probe.recent == 0)
    #expect(await probe.history == 0)
    try await http.shutdown()
  }

  private struct ControlledProvider: PublicContentDiscovering {
    let publish: @Sendable () async throws -> Int
    func discover(scope: PublicContentDiscoveryScope) async throws -> Int { try await publish() }
  }

  @Test(arguments: [false, true])
  func coldBootstrapKeepsCachedOrEmptyPageUntilUserRefreshAfterProviderFinishes(hasCachedHistory: Bool) async throws {
    let probe = Probe(), logger = Logger(label: "provider-page.test")
    let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).path
    defer { try? FileManager.default.removeItem(atPath: path) }
    let store = try SQLiteThinAppViewStore(path: path, logger: logger)
    let author = "did:plc:aaaaaaaaaaaaaaaaaaaaaaaa"
    let publication = "at://\(author)/site.standard.publication/main"
    let now = Date()
    let previous = IndexedContentItem(uri: "at://\(author)/site.standard.document/previous", cid: "previous",
      authorDid: author, collection: "site.standard.document", createdAt: now, indexedAt: now,
      publicationSite: publication, render: ContentRenderFields(title: "Retained history", publishedAt: ISO8601DateFormatter().string(from: now)),
      expiresAt: now.addingTimeInterval(3600))
    if hasCachedHistory { try await store.upsertContentItem(previous) }
    let incoming = IndexedContentItem(uri: "at://\(author)/site.standard.document/new", cid: "new",
      authorDid: author, collection: "site.standard.document", createdAt: now, indexedAt: now,
      publicationSite: publication, render: ContentRenderFields(title: "Provider discovery", publishedAt: ISO8601DateFormatter().string(from: now)),
      expiresAt: now.addingTimeInterval(3600))
    let (gate, release) = AsyncStream<Void>.makeStream()
    defer { release.finish() }
    let provider = ControlledProvider {
      await probe.discover()
      for await _ in gate {}
      try Task.checkCancellation()
      try await store.upsertContentItem(incoming)
      return 1
    }
    let config = ThinAppViewConfig.fromEnvironment(["ENABLE_THIN_APPVIEW": "true"])
    let http = HTTPClient(eventLoopGroupProvider: .singleton)
    let service = ThinAppViewEnrollService(store: store,
      indexer: ThinAppViewIndexer(store: store, config: config, logger: logger), httpClient: http,
      plcURL: "https://plc.directory", config: config, logger: logger,
      publicContentProvider: provider, publicContentDiscoveryMode: .microcosm,
      publicContentPDS: { _, recent in await probe.enumerate(recentOnly: recent) })
    let read = ThinAppViewReadService(store: store, projectionCache: nil, logger: logger)
    let auth = AuthContext(did: author, authorizationForwardingValue: "DPoP test", dpopProof: nil)
    let enrollment = BootstrapEnrollment.start {
      _ = try? await service.refreshPublicContentFast(authorDid: author, publicationAtUri: publication)
    }
    let until = ContinuousClock.now.advanced(by: .seconds(2))
    while !(await enrollment.isFinished), ContinuousClock.now < until { try await Task.sleep(for: .milliseconds(5)) }
    #expect(await enrollment.isFinished)
    let first = try await BootstrapEntriesPageSelection.load(enrollment: enrollment, livePage: {
      try await read.listEntries(auth: auth, authorDid: author, publicationAtUri: publication,
        publicationScopeAtUris: [], publicationSiteUrls: [], filter: .all, cursor: nil, limit: 50)
    }, cachedPage: { nil })
    #expect(first.page.entries.count == (hasCachedHistory ? 1 : 0))
    release.finish()
    // Explicit subsequent refresh joins the provider work, then queries the same server projection.
    _ = try await service.refreshPublicContent(authorDid: author, publicationAtUri: publication)
    let refreshed = try await read.listEntries(auth: auth, authorDid: author, publicationAtUri: publication,
      publicationScopeAtUris: [], publicationSiteUrls: [], filter: .all, cursor: nil, limit: 50)
    #expect(refreshed.entries.count == (hasCachedHistory ? 2 : 1))
    await service.shutdown()
    #expect(await probe.recent == 0)
    #expect(await probe.history == 0)
    try await http.shutdown()
  }

  @Test func explicitOffModeRetainsLegacyPDSRefreshAndHistory() async throws {
    let probe = Probe(), logger = Logger(label: "legacy-mode.test")
    let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).path
    defer { try? FileManager.default.removeItem(atPath: path) }
    let store = try SQLiteThinAppViewStore(path: path, logger: logger)
    let config = ThinAppViewConfig.fromEnvironment(["ENABLE_THIN_APPVIEW": "true"])
    let http = HTTPClient(eventLoopGroupProvider: .singleton)
    let service = ThinAppViewEnrollService(store: store,
      indexer: ThinAppViewIndexer(store: store, config: config, logger: logger), httpClient: http,
      plcURL: "https://plc.directory", config: config, logger: logger,
      publicContentProvider: Provider(probe: probe), publicContentDiscoveryMode: .off,
      publicContentPDS: { _, recent in await probe.enumerate(recentOnly: recent) })
    #expect(try await service.refreshPublicContent(authorDid: "did:plc:aaaaaaaaaaaaaaaaaaaaaaaa",
      publicationAtUri: nil, publicationSiteUrls: ["https://publisher.example"]) == 3)
    let until = ContinuousClock.now.advanced(by: .seconds(2))
    while await probe.history == 0, ContinuousClock.now < until { try await Task.sleep(for: .milliseconds(5)) }
    await service.shutdown()
    #expect(await probe.provider == 0)
    #expect(await probe.recent == 1)
    #expect(await probe.history == 1)
    try await http.shutdown()
  }
}
