import AsyncHTTPClient
import GatewayCore
import Hummingbird
import Logging
import OperationsCore

/// Edge routes share the same HTTP listener and authentication context as the
/// reader. There is no Gateway hop or internal-HMAC authentication bypass.
struct StandaloneEdgeRuntime: Sendable {
  let configuration: StandaloneEdgeConfiguration
  let cache: any PdsRepoRecordCacheStore
  let admission: EdgeDraftAdmission
  let limiter: WireRequestLimiter

  init(
    configuration: StandaloneEdgeConfiguration, cache: any PdsRepoRecordCacheStore,
    environment: String
  ) {
    self.configuration = configuration
    self.cache = cache
    self.admission = EdgeDraftAdmission(environment: environment)
    self.limiter = WireRequestLimiter(environment: environment)
  }

  func register(
    on protected: RouterGroup<GatewayRequestContext>, router: Router<GatewayRequestContext>,
    config: AppViewServiceConfig, httpClient: HTTPClient,
    telemetry: OperationsTelemetryBuffer?, telemetryEnvironment: String, logger: Logger
  ) {
    GatewayOAuthMetadataRoutes(
      publicOrigin: config.core.oauthPublicOrigin,
      nativeOrigin: config.core.oauthIosMetadataOrigin,
      operationsOrigin: config.core.oauthOperationsOrigin,
      podcastsEnabled: config.podcastsEnabled
    ).register(on: router)
    SubscriptionRoutes(entitlements: configuration.entitlements).register(on: protected)
    GIFRoutes(
      catalog: GIFCatalogService(apiKey: configuration.klipyAPIKey, httpClient: httpClient),
      limiter: limiter, admission: admission
    ).register(on: protected)
    let repo = ATProtoAuthenticatedRepoClient(
      httpClient: httpClient, plcURL: config.core.atprotoPLCURL, logger: logger)
    SyncRoutes(
      preferenceService: PreferenceSyncService(
        httpClient: httpClient, cache: cache,
        plcURL: config.core.atprotoPLCURL, logger: logger), repo: repo
    ).register(on: protected)
    ClientPerformanceTelemetryRoutes(telemetry: telemetry, environment: telemetryEnvironment)
      .register(on: protected)
    if let credentials = configuration.latrProxy {
      LatrProxyRoutes(config: credentials, httpClient: httpClient, logger: logger).register(
        on: protected)
    }
  }
}
