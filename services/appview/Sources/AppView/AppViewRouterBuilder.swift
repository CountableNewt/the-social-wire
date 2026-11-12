import AsyncHTTPClient
import Foundation
import GatewayCore
import Hummingbird
import Logging
import OperationsCore
import SkejBackend
import ThinAppViewCore
import WireCore

enum AppViewRouterBuilder {
  static func router(
    config: AppViewServiceConfig,
    httpClient: HTTPClient,
    backgroundTasks: AppViewBackgroundTasks? = nil,
    thinAppViewStore: any ThinAppViewStore,
    wireFeedStore: (any WireFeedStore)? = nil,
    wireModerationService: WireViewerModerationService? = nil,
    financeFeedStore: PostgresFinanceFeedStore? = nil,
    sportsFeedStore: PostgresSportsFeedStore? = nil,
    podcastStore: (any PodcastStoring)? = nil,
    circleDiscoveryService: CircleDiscoveryService? = nil,
    circlePrivateState: (any CirclePrivateStateStoring)? = nil,
    projectionCache: (any AppViewProjectionCacheStore)?,
    operationsStore: (any OperationsStore)? = nil,
    telemetry: OperationsTelemetryBuffer? = nil,
    telemetryEnvironment: String = "unknown",
    telemetryInstanceId: String = "unknown",
    socialDraftService: (any SocialDraftStoring)? = nil,
    standaloneEdge: StandaloneEdgeRuntime? = nil,
    analyticsStore: (any AnalyticsStoring)? = nil,
    mediaReadiness: StandaloneMediaReadinessProbe? = nil,
    schedulingBackend: EmbeddedSkejBackend? = nil,
    logger: Logger
  ) -> Router<GatewayRequestContext> {
    let router = Router(context: GatewayRequestContext.self)
    router.add(
      middleware: RequestTraceMiddleware(
        service: "appview", environment: telemetryEnvironment, instanceId: telemetryInstanceId,
        telemetry: telemetry))
    if config.standaloneEdge != nil { router.add(middleware: GatewayCORSPolicy.middleware(config: config.core)) }
    router.get("/health") { _, _ in ["status": "ok", "service": "appview"] }
    router.get("/xrpc/app.thesocialwire.teapot") { _, _ in
      Response(status: .init(code: 418, reasonPhrase: "I'm a teapot"), headers: [.contentType: "application/json"],
        body: .init(byteBuffer: .init(string: #"{"error":"Teapot","message":"I'm a teapot"}"#)))
    }
    router.get("/livez") { _, _ in ["status": "live", "service": "appview"] }
    router.get("/readyz") { _, _ async throws -> [String: String] in
      try await thinAppViewStore.ping()
      try await operationsStore?.ping()
      if let edge = config.standaloneEdge, config.podcastsEnabled {
        let probe = mediaReadiness ?? StandaloneMediaReadinessProbe(baseURL: edge.mediaWorkerURL, httpClient: httpClient)
        try await probe.run()
      }
      return ["status": "ready", "service": "appview"]
    }
    router.get("/freshness") { _, _ async throws -> ServiceFreshnessResponse in
      try await ServiceFreshnessResponse.evaluate(service: "appview", store: config.globalIngestionEnabled ? operationsStore : nil)
    }

    let internalTrustMiddleware = GatewayInternalTrustAuthMiddleware(
      sharedSecret: config.core.gatewayAppViewInternalSecret,
      logger: logger
    )
    let authMiddleware = ATProtoAuthMiddleware(
      httpClient: httpClient,
      plcURL: config.core.atprotoPLCURL,
      gatewayClientPolicy: config.core.oauthGateway,
      attestationReceipt: config.standaloneEdge?.attestationReceipt,
      supplementalJwksJSON: config.core.oauthAccessTokenSupplementalJwksJSON,
      logger: logger
    )
    let readStateRecovery: PDSReadStateRecoveryCoordinator?
    if let lifecycleStore = thinAppViewStore as? any PDSReadStateLifecycleStoring, let operationsStore {
      readStateRecovery = PDSReadStateRecoveryCoordinator(store: lifecycleStore, operations: operationsStore,
        projectionCache: projectionCache, plcURL: config.core.atprotoPLCURL, logger: logger)
    } else { readStateRecovery = nil }
    if let readStateRecovery { backgroundTasks?.register { await readStateRecovery.shutdown() } }
    var authenticated = router.group()
      .add(middleware: XRPCErrorMiddleware())
      .add(middleware: AppViewFeedErrorMiddleware())
    if config.standaloneEdge == nil {
      authenticated = authenticated.add(middleware: internalTrustMiddleware)
    }
    authenticated = authenticated.add(middleware: authMiddleware)
    if let standaloneEdge {
      authenticated = authenticated.add(middleware: StandaloneAdmissionMiddleware(runtime: standaloneEdge))
      standaloneEdge.register(on: authenticated, router: router, config: config, httpClient: httpClient,
        telemetry: telemetry, telemetryEnvironment: telemetryEnvironment, logger: logger)
    }
    if let schedulingBackend {
      let entitlements = standaloneEdge?.configuration.entitlements
      schedulingBackend.registerOAuth(on: router) { did in
        guard let entitlements, entitlements.gatingEnabled else { return true }
        return entitlements.hasPro(did: did)
      }
      schedulingBackend.registerScheduling(on: authenticated) { context in
        guard let did = context.authContext?.did,
          did != GatewayInternalTrustAuthMiddleware.anonymousDiscoveryDID
        else { return nil }
        return did
      }
    }
    let protected = authenticated
      .add(middleware: PDSReadStateReadinessMiddleware(store: thinAppViewStore as? any PDSReadStateLifecycleStoring, recovery: readStateRecovery))
      .add(middleware: LegacyReadStateMutationMiddleware())

    MediaProjectRoutes().register(on: protected)
    ArticleEmbedRoutes(resolver: ArticleEmbedResolver(reader: ArticleEmbedHTTPReader(http: httpClient,
      repo: ATProtoAuthenticatedRepoClient(httpClient: httpClient, plcURL: config.core.atprotoPLCURL, logger: logger))))
      .register(on: protected)
    SocialDraftRoutes(service: socialDraftService).register(on: protected)
    ArticleDraftRoutes(service: socialDraftService).register(on: protected)
    if config.toolsEnabled, let analyticsStore {
      let reader = AnalyticsHTTPReader(repo: ATProtoAuthenticatedRepoClient(httpClient: httpClient,
        plcURL: config.core.atprotoPLCURL, logger: logger), http: httpClient)
      let publicationCounts = PublicationAnalyticsCountReader(http: httpClient)
      AnalyticsRoutes(store: analyticsStore, provider: AnalyticsValidatedContent(base: AnalyticsProvider(reader: reader)),
        bluesky: BlueskyAnalyticsProvider(reader: BlueskyAnalyticsHTTPReader(http: httpClient)),
        publications: PublicationAnalyticsProvider(reader: reader, count: publicationCounts.count))
        .register(on: protected)
    }

    if config.podcastsEnabled, let podcastStore {
      var podcastService = PodcastService(store: podcastStore, http: httpClient,
        repo: ATProtoAuthenticatedRepoClient(httpClient: httpClient, plcURL: config.core.atprotoPLCURL, logger: logger))
      let resolver = podcastService
      let feedResolution = PodcastFeedResolutionQueue { key in _ = try await resolver.resolve(key) }
      backgroundTasks?.register { await feedResolution.shutdown() }
      podcastService.feedResolution = feedResolution
      let routes = PodcastRoutes(service: podcastService)
      let podcasts = protected.group().add(middleware: PodcastPrivateStorageMiddleware())
      routes.register(on: podcasts)
      PodcastPrivateRoutes(service: podcastService).register(on: podcasts)
      PodcastImageRoutes(service: podcastService).register(on: podcasts)
      routes.registerPublic(on: router)
      PodcastAssetRoutes(service: podcastService, workerURL: "", secret: "").registerMedia(on: podcasts)
      let env = ProcessInfo.processInfo.environment
      if let url = env["PODCAST_MEDIA_WORKER_URL"], let secret = env["PODCAST_MEDIA_INTERNAL_SECRET"], !secret.isEmpty {
        PodcastAssetRoutes(service: podcastService, workerURL: url, secret: secret).register(on: podcasts, router: router)
      }
    }

    if let wireFeedStore, let wireModerationService, (config.wire.mode.servesAPI || config.finance.mode.canServeAPI || config.sports.mode.canServeAPI) {
      let wireInternalTrustMiddleware = GatewayInternalTrustAuthMiddleware(
        sharedSecret: config.core.gatewayAppViewInternalSecret,
        allowsAnonymousDiscovery: true,
        logger: logger
      )
      let wire = router.group()
        .add(middleware: XRPCErrorMiddleware())
        .add(middleware: wireInternalTrustMiddleware)
        .add(middleware: authMiddleware)
      WireDiscoveryRoutes(
        store: wireFeedStore,
        moderation: wireModerationService,
        telemetry: telemetry,
        wireVisible: config.wire.mode.servesAPI,
        financeStore: financeFeedStore,
        sportsStore: sportsFeedStore
      ).register(on: wire)
      if let financeFeedStore, config.finance.mode.canServeAPI {
        FinanceDiscoveryRoutes(store: financeFeedStore, moderation: wireModerationService, telemetry: telemetry).register(on: wire)
      }
      if let sportsFeedStore, config.sports.mode.canServeAPI {
        SportsDiscoveryRoutes(store: sportsFeedStore, moderation: wireModerationService, telemetry: telemetry).register(on: wire)
      }
    }

    if let circleDiscoveryService, config.circle.mode.servesAPI {
      CircleDiscoveryRoutes(service: circleDiscoveryService).register(on: protected)
    }

    let sembleRepo = ATProtoAuthenticatedRepoClient(
      httpClient: httpClient,
      plcURL: config.core.atprotoPLCURL,
      logger: logger
    )
    SembleProjectionRoutes(
      service: SembleProjectionService(
        transport: HTTPSembleProjectionTransport(
          baseURL: config.semble.baseURL,
          httpClient: httpClient
        ),
        recordReader: ATProtoSemblePublicRecordReader(repo: sembleRepo)
      )
    ).register(on: protected)

    guard config.thinAppView.enabled else { return router }

    let projection = PublicationProjectionService(
      httpClient: httpClient,
      plcURL: config.core.atprotoPLCURL,
      logger: logger,
      thinStore: thinAppViewStore,
      projectionCache: projectionCache,
      telemetry: telemetry
    )
    let resolve = PublicationResolveService(
      httpClient: httpClient,
      plcURL: config.core.atprotoPLCURL,
      logger: logger
    )
    PublicationRoutes(projectionService: projection, resolveService: resolve).register(
      on: protected)

    let rssIngestion = ThinAppViewRssIngestion(
      store: thinAppViewStore,
      httpClient: httpClient,
      config: config.thinAppView,
      logger: logger,
      projectionCache: projectionCache
    )
    let indexer = ThinAppViewIndexer(
      store: thinAppViewStore,
      config: config.thinAppView,
      logger: logger,
      httpClient: httpClient,
      plcURL: config.core.atprotoPLCURL,
      rssIngestion: rssIngestion,
      projectionCache: projectionCache
    )
    let repo = ATProtoAuthenticatedRepoClient(
      httpClient: httpClient,
      plcURL: config.core.atprotoPLCURL,
      logger: logger
    )
    let listsService = StandardReaderListsService(
      reader: ATProtoStandardReaderListReader(repo: repo, httpClient: httpClient, plcURL: config.core.atprotoPLCURL),
      publicationProjection: projection
    )
    StandardReaderListRoutes(service: listsService).register(on: protected)
    let skyreaderIngestionService = ThinAppViewSkyreaderIngestionService(
      repo: repo,
      rssIngestion: rssIngestion,
      store: thinAppViewStore,
      logger: logger
    )
    let contentVerifier: AuthoritativeContentVerifier? = config.globalIngestionEnabled ? nil
      : AuthoritativeContentVerifier(store: thinAppViewStore, indexer: indexer,
        httpClient: httpClient, plcURL: config.core.atprotoPLCURL)
    if let contentVerifier { backgroundTasks?.register { await contentVerifier.shutdown() } }
    let readService = ThinAppViewReadService(
      store: thinAppViewStore,
      projectionCache: projectionCache,
      telemetry: telemetry,
      circlePrivateState: circlePrivateState,
      contentVerifier: contentVerifier,
      logger: logger
    )
    let enrollService = ThinAppViewEnrollService(
      store: thinAppViewStore,
      indexer: indexer,
      httpClient: httpClient,
      plcURL: config.core.atprotoPLCURL,
      config: config.thinAppView,
      logger: logger,
      skyreaderIngestion: skyreaderIngestionService,
      publicContentProvider: config.publicContentDiscoveryMode == .microcosm
        ? MicrocosmContentDiscovery(indexer: indexer, httpClient: httpClient) : nil,
      publicContentDiscoveryMode: config.publicContentDiscoveryMode,
      knownPublicContentScopes: { auth, authors in
        await projection.knownPublicContentScopes(viewerDid: auth.did, authorDids: authors)
      }
    )
    backgroundTasks?.register { await enrollService.shutdown() }
    ThinAppViewRoutes(
      readService: readService,
      enrollService: enrollService,
      projectionService: projection,
      listsService: listsService
    ).register(on: protected)

    AppViewExtendedRoutes(
      readService: readService,
      projectionService: projection
    ).register(on: protected)

    if let pdsReadStateStore = thinAppViewStore as? any PDSReadStateStoring {
      PDSReadStateRoutes(service: PDSReadStateService(store: pdsReadStateStore,
        thinStore: thinAppViewStore, repo: repo, publicationService: projection, projectionCache: projectionCache))
        .register(on: protected)
    }

    let bootstrapStream = BootstrapStreamService(
      projectionService: projection,
      readService: readService,
      enrollService: enrollService,
      skyreaderIngestionService: skyreaderIngestionService,
      projectionCache: projectionCache,
      telemetry: telemetry,
      logger: logger,
      listsService: listsService
    )
    BootstrapStreamRoutes(bootstrapStreamService: bootstrapStream).register(on: protected)

    return router
  }
}
