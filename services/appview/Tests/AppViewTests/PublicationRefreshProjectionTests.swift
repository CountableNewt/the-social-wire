import AsyncHTTPClient
import Foundation
import GatewayCore
import Hummingbird
import HummingbirdTesting
import Logging
import Testing
import ThinAppViewCore

@testable import AppView

@Suite("Publication refresh projection")
struct PublicationRefreshProjectionTests {
  enum EnrollmentOutcome: String, Sendable {
    case success, publisherUnavailable, pollTimedOut
  }

  private let auth = AuthContext(
    did: "did:plc:viewer", authorizationForwardingValue: "DPoP test", dpopProof: "test"
  )

  @Test("manual refresh returns folder membership and retains cached Following without replacing the full projection")
  func completeManualProjection() async throws {
    try await withProjection { store, cache, projection, _, _ in
      let followed = publication(id: "at://did:plc:folded/site.standard.publication/other", authorDid: "did:plc:folded")
      let formerFollowing = publication(id: "did:plc:folded", authorDid: "did:plc:folded")
      try await seedSidebar([followed, formerFollowing], cache: cache)
      let folder = PublicationFolderRecord(uri: "at://\(auth.did)/app.thesocialwire.folder/news", rkey: "news", value: [:])
      let pref = PublicationPrefsRecordDTO(uri: "at://\(auth.did)/app.thesocialwire.publicationPrefs/news",
        publicationId: formerFollowing.publicationId, value: ["folderId": AnyCodable(folder.uri)])
      let folded = ProjectionDiscoveredRow(publicationId: formerFollowing.publicationId,
        authorDid: formerFollowing.authorDid, title: "Folded", discoveredAt: Date())
      let context = SidebarDiscoveryContext(viewerDid: auth.did, folders: [folder], prefs: [pref],
        subscribed: [folded], myPublications: [], unfoldered: [], following: [], uniqueRows: [folded],
        enrollAuthorDids: [], prefsByPublicationId: [folded.publicationId: pref])
      let uncachedFollowing = "did:plc:uncached"
      try await store.upsertPublicationScopes([
        AppViewUnreadCounterSupport.publicationScope(viewerDid: auth.did, publicationId: uncachedFollowing,
          authorDid: uncachedFollowing, publicationAtUri: nil, publicationScopeAtUris: [],
          publicationSiteUrls: [], sectionKeys: ["following"], updatedAt: Date())
      ])

      let response = try await projection.buildSidebarResponse(context: context, auth: auth,
        phase: .manualRefresh, refreshedAt: Date())

      #expect(response.folderSections.first?.publications.map(\.publicationId) == [folded.publicationId])
      #expect(response.followingTabPublications == [followed])
      #expect(Set(response.allPublicationRows.map(\.publicationId)) == [folded.publicationId, followed.publicationId])
      let subscribed = try await store.publicationScopes(viewerDid: auth.did, sectionKey: "subscribed")
      #expect(subscribed.map(\.publicationId) == [folded.publicationId])
      let following = try await store.publicationScopes(viewerDid: auth.did, sectionKey: "following")
      #expect(Set(following.map(\.publicationId)) == [followed.publicationId, uncachedFollowing])
    }
  }

  @Test("enrollment retains sidebar scopes and reconciles after completed and partial work",
    arguments: [EnrollmentOutcome.success, .publisherUnavailable, .pollTimedOut])
  func enrollmentPreservesSidebar(outcome: EnrollmentOutcome) async throws {
    try await withProjection { store, cache, projection, http, logger in
      let row = publication(id: "at://did:plc:followed/site.standard.publication/blog", authorDid: "did:plc:followed")
      try await seedSidebar([row], cache: cache)
      _ = await projection.knownPublicContentScopes(viewerDid: auth.did, authorDids: [row.authorDid])
      #expect(await projection.sidebarRow(for: auth.did, publicationId: row.publicationId) != nil)
      let expiry = Date().addingTimeInterval(300)
      try await cache.storeUnreadCounts(viewerDid: auth.did, counts: [row.publicationId: 7], expiresAt: expiry)
      try await cache.storeFirstPageJSON(viewerDid: auth.did, publicationId: row.publicationId,
        jsonBody: "{}", expiresAt: expiry)
      let feed = "https://publisher.example/feed"
      let ingestion = ThinAppViewSkyreaderIngestionService(
        viewerFeedUrls: { _ in [feed] },
        ingestFeed: { _ in 0 },
        refreshFeed: { url in
          switch outcome {
          case .success: return 0
          case .publisherUnavailable: throw AppViewRSSRefreshFailure(feedUrl: url)
          case .pollTimedOut: throw AppViewRSSRefreshJoinTimeout()
          }
        },
        logger: logger
      )
      let config = ThinAppViewConfig.fromEnvironment(["PUBLIC_CONTENT_DISCOVERY_MODE": "off"])
      let enroll = ThinAppViewEnrollService(store: store,
        indexer: ThinAppViewIndexer(store: store, config: config, logger: logger), httpClient: http,
        plcURL: "http://127.0.0.1:1", config: config, logger: logger, skyreaderIngestion: ingestion)
      let router = Router(context: GatewayRequestContext.self)
      router.add(middleware: PublicationRefreshTestAuth(auth: auth))
      ThinAppViewRoutes(readService: ThinAppViewReadService(store: store, projectionCache: cache, logger: logger),
        enrollService: enroll, projectionService: projection).register(on: router.group())

      try await Application(router: router).test(.router) { client in
        let response = try await client.execute(uri: "/xrpc/app.thesocialwire.appview.enrollSources", method: .post,
          headers: [.contentType: "application/json"], body: .init(string: #"{"feedUrls":["https://publisher.example/feed"]}"#))
        #expect(response.status == (outcome == .success ? .ok : .serviceUnavailable))
      }

      #expect(await projection.sidebarRow(for: auth.did, publicationId: row.publicationId) == row)
      #expect(await projection.cachedSidebarResponse(viewerDid: auth.did)?.followingTabPublications == [row])
      #expect(try await cache.unreadCountsCacheEntry(viewerDid: auth.did) == nil)
      #expect(try await cache.firstPageCacheEntry(viewerDid: auth.did, publicationId: row.publicationId) == nil)
    }
  }

  private func withProjection(
    _ body: (SQLiteThinAppViewStore, SQLiteAppViewProjectionCacheStore, PublicationProjectionService, HTTPClient, Logger) async throws -> Void
  ) async throws {
    let dir = FileManager.default.temporaryDirectory.appendingPathComponent("publication-refresh-\(UUID().uuidString)")
    try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: dir) }
    let logger = Logger(label: "publication-refresh.test")
    let store = try SQLiteThinAppViewStore(path: dir.appendingPathComponent("store.sqlite").path, logger: logger)
    let cache = try SQLiteAppViewProjectionCacheStore(path: dir.appendingPathComponent("cache.sqlite").path, logger: logger)
    let http = HTTPClient(eventLoopGroupProvider: .singleton)
    let projection = PublicationProjectionService(httpClient: http, plcURL: "http://127.0.0.1:1",
      logger: logger, thinStore: store, projectionCache: cache)
    do {
      try await body(store, cache, projection, http, logger)
    } catch {
      try? await http.shutdown()
      throw error
    }
    try await http.shutdown()
  }

  private func seedSidebar(_ following: [SidebarPublicationRow], cache: SQLiteAppViewProjectionCacheStore) async throws {
    let snapshot = BootstrapSidebarCacheSnapshot(priority: PublicationSidebarResponse(viewerDid: auth.did, folders: [],
      publicationPrefs: [], folderSections: [], allPublicationRows: following, myPublications: [],
      subscribedUnfoldered: [], followingTabPublications: following, enrollAuthorDids: [],
      totalUnreadCount: 0, refreshedAt: Date()), folderPayload: nil)
    try await cache.storeSidebarProjectionJSON(viewerDid: auth.did,
      jsonBody: String(decoding: try JSONEncoder().encode(snapshot), as: UTF8.self),
      expiresAt: Date().addingTimeInterval(300))
  }

  private func publication(id: String, authorDid: String) -> SidebarPublicationRow {
    SidebarPublicationRow(publicationId: id, subscriptionPublicationId: nil, authorDid: authorDid,
      authorHandle: nil, title: id, iconUrl: nil, avatarUrl: nil, discoveredAt: Date(),
      appViewScope: PublicationAppViewScope(authorDid: authorDid, publicationAtUri: id,
        publicationScopeAtUris: [id], publicationSiteUrls: []))
  }
}

private struct PublicationRefreshTestAuth: RouterMiddleware {
  let auth: AuthContext
  func handle(_ request: Request, context: GatewayRequestContext,
    next: (Request, GatewayRequestContext) async throws -> Response) async throws -> Response {
    var context = context
    context.authContext = auth
    return try await next(request, context)
  }
}
