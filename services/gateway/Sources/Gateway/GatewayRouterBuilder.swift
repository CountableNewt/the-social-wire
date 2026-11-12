import AsyncHTTPClient
import Foundation
import GatewayCore
import Hummingbird
import Logging
import OperationsCore

enum GatewayRouterBuilder {
  static func router(
    config: GatewayServiceConfig,
    httpClient: HTTPClient,
    cache: any PdsRepoRecordCacheStore,
    wireLimiter: WireRequestLimiter = WireRequestLimiter(),
    socialDraftAdmission: SocialDraftAdmission = SocialDraftAdmission(),
    ingestionHealth: GatewayIngestionHealth? = nil,
    operationsStore: (any OperationsStore)? = nil,
    telemetry: OperationsTelemetryBuffer? = nil,
    telemetryEnvironment: String = "unknown",
    telemetryInstanceId: String = "unknown",
    logger: Logger
  ) -> Router<GatewayRequestContext> {
    let router = Router(context: GatewayRequestContext.self)
    router.add(
      middleware: RequestTraceMiddleware(
        service: "gateway", environment: telemetryEnvironment, instanceId: telemetryInstanceId,
        telemetry: telemetry))
    router.add(middleware: GatewayCORSPolicy.middleware(config: config.core))
    router.get("/health") { _, _ in ["status": "ok", "service": "gateway"] }
    router.get("/xrpc/app.thesocialwire.teapot") { _, _ in
      Response(status: .init(code: 418, reasonPhrase: "I'm a teapot"), headers: [.contentType: "application/json"],
        body: .init(byteBuffer: .init(string: #"{"error":"Teapot","message":"I'm a teapot"}"#)))
    }
    router.get("/livez") { _, _ in ["status": "live", "service": "gateway"] }
    router.get("/readyz") { _, _ async throws -> [String: String] in
      do {
        try await GatewayReadinessProbe(
          operationsStore: operationsStore,
          appViewBaseURL: config.appViewBaseURL,
          httpClient: httpClient,
          recordFailure: { dependency in
            logger.error(
              "Gateway readiness required dependency failed",
              metadata: ["dependency": .string(dependency.rawValue)]
            )
          }
        ).run()
      } catch {
        try Task.checkCancellation()
        if error is CancellationError { throw error }
        throw HTTPError(.serviceUnavailable, message: "Gateway serving dependency unavailable")
      }
      let ingestion = config.globalIngestionEnabled
        ? await ingestionHealth?.snapshot() ?? .unknown : .disabled
      return ingestion.dependencyState.merging(["status": "ready", "service": "gateway"]) { _, new in new }
    }
    router.get("/freshness") { _, _ async -> GatewayIngestionHealthSnapshot in
      config.globalIngestionEnabled ? await ingestionHealth?.snapshot() ?? .unknown : .disabled
    }

    GatewayOAuthMetadataRoutes(
      publicOrigin: config.core.oauthPublicOrigin,
      nativeOrigin: config.core.oauthIosMetadataOrigin,
      operationsOrigin: config.core.oauthOperationsOrigin,
      podcastsEnabled: config.podcastsEnabled
    ).register(on: router)

    let authMiddleware = ATProtoAuthMiddleware(
      httpClient: httpClient,
      plcURL: config.core.atprotoPLCURL,
      gatewayClientPolicy: config.core.oauthGateway,
      attestationReceipt: config.pdsAttestationReceipt,
      supplementalJwksJSON: config.core.oauthAccessTokenSupplementalJwksJSON,
      logger: logger
    )
    let protected = router.group()
      .add(middleware: XRPCErrorMiddleware())
      .add(middleware: authMiddleware)
    let optionalAuthentication = router.group()
      .add(middleware: XRPCErrorMiddleware())
      .add(middleware: OptionalATProtoAuthMiddleware(strict: authMiddleware))
    let entitlements = GatewayEntitlements(gatingEnabled: config.proGatingEnabled, proDIDs: config.proDIDs)
    SubscriptionRoutes(entitlements: entitlements).register(on: protected)
    GIFRoutes(catalog: GIFCatalogService(apiKey: config.klipyAPIKey, httpClient: httpClient), limiter: wireLimiter, admission: socialDraftAdmission).register(on: protected)

    let prefs = PreferenceSyncService(
      httpClient: httpClient,
      cache: cache,
      plcURL: config.core.atprotoPLCURL,
      logger: logger
    )
    let repo = ATProtoAuthenticatedRepoClient(
      httpClient: httpClient,
      plcURL: config.core.atprotoPLCURL,
      logger: logger
    )
    SyncRoutes(preferenceService: prefs, repo: repo).register(on: protected)
    ClientPerformanceTelemetryRoutes(
      telemetry: telemetry,
      environment: telemetryEnvironment
    ).register(on: protected)

    if let appViewBase = config.appViewBaseURL {
      if config.toolsEnabled {
        AnalyticsProxyRoutes(proxy: AppViewProxyRoutes(baseURL: appViewBase, internalSecret: config.core.gatewayAppViewInternalSecret, httpClient: httpClient, logger: logger), entitlements: entitlements).register(on: protected)
      }
      SocialDraftProxyRoutes(baseURL: appViewBase, internalSecret: config.core.gatewayAppViewInternalSecret,
        httpClient: httpClient, admission: socialDraftAdmission).register(on: protected)
      PodcastMediaProxyRoutes(baseURL: appViewBase, internalSecret: config.core.gatewayAppViewInternalSecret,
        httpClient: httpClient).register(on: protected, router: router)
      AppViewProxyRoutes(
        baseURL: appViewBase,
        internalSecret: config.core.gatewayAppViewInternalSecret,
        httpClient: httpClient,
        logger: logger
      ).register(on: protected)
      if config.wireFeedMode.servesAPI || config.financeFeedMode.servesAPI || config.sportsFeedMode.servesAPI {
        WireProxyRoutes(
          baseURL: appViewBase,
          internalSecret: config.core.gatewayAppViewInternalSecret,
          httpClient: httpClient,
          limiter: wireLimiter,
          logger: logger
        ).register(on: optionalAuthentication)
      }
      if config.circleFeedMode.servesAPI {
        CircleProxyRoutes(
          baseURL: appViewBase,
          internalSecret: config.core.gatewayAppViewInternalSecret,
          httpClient: httpClient,
          limiter: wireLimiter,
          logger: logger
        ).register(on: protected)
      }
    }

    if let operationsBase = config.operationsBaseURL {
      OperationsProxyRoutes(
        baseURL: operationsBase,
        internalSecret: config.core.gatewayOperationsInternalSecret,
        httpClient: httpClient
      ).register(on: protected)
    }

    if let latrIosProxy = config.latrIosProxy {
      LatrProxyRoutes(
        config: latrIosProxy,
        httpClient: httpClient,
        logger: logger
      ).register(on: protected)
    }

    return router
  }
}
