import Foundation
import GatewayCore
import ThinAppViewCore

struct AppViewServiceConfig: Sendable {
  let core: GatewayConfig
  let thinAppView: ThinAppViewConfig
  let storeBackend: StoreBackend
  let wire: WireDiscoveryConfig
  let circle: CircleDiscoveryConfig
  var finance: FinanceDiscoveryConfig = FinanceDiscoveryConfig.disabled
  var sports: SportsDiscoveryConfig = .disabled
  var podcastsEnabled: Bool = false
  var toolsEnabled: Bool = false
  let semble: SembleProjectionConfig

  enum StoreBackend: Sendable {
    case sqlite(path: String)
    case postgres(url: String)
  }

  static func fromEnvironment(
    _ env: [String: String] = AppEnvironmentLoader.mergeProcessWithDotenv()
  ) throws -> AppViewServiceConfig {
    var env = env
    let rawEnvironment = (env["APP_ENV"] ?? "local").trimmingCharacters(in: .whitespacesAndNewlines)
    let appEnvironment = rawEnvironment.isEmpty ? "local" : rawEnvironment
    guard GatewayConfig.AppEnvironment(rawValue: appEnvironment) != nil else {
      throw AppViewServiceConfigError.invalidEnvironment
    }
    env["APP_ENV"] = appEnvironment
    if let rawPort = env["PORT"], !rawPort.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
      guard let port = Int(rawPort), (1...65535).contains(port) else {
        throw AppViewServiceConfigError.invalidPort
      }
    }
    for key in ["ATPROTO_PLC_URL", "SEMBLE_PUBLIC_API_BASE_URL"] {
      guard let raw = env[key], !raw.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { continue }
      guard let url = URLComponents(string: raw), ["http", "https"].contains(url.scheme ?? ""),
        let host = url.host, !host.isEmpty, url.user == nil, url.password == nil,
        url.query == nil, url.fragment == nil
      else { throw AppViewServiceConfigError.invalidServiceURL }
    }
    let core = GatewayConfig.fromEnvironment(env)
    let thin = ThinAppViewConfig.fromEnvironment(env)
    let backend: StoreBackend
    switch core.appEnv {
    case .local:
      backend = .sqlite(path: env["SQLITE_DB_PATH"] ?? "./social-wire-appview.sqlite")
    case .dev, .prod:
      guard let dbURL = env["DATABASE_URL"], !dbURL.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
        throw AppViewServiceConfigError.missingDatabaseURL
      }
      backend = .postgres(url: dbURL)
    }
    let finance = try FinanceDiscoveryConfig.fromEnvironment(env)
    let sports = try SportsDiscoveryConfig.fromEnvironment(env)
    let wire = try WireDiscoveryConfig.fromEnvironment(env)
    let circle = try CircleDiscoveryConfig.fromEnvironment(env)
    let semble = SembleProjectionConfig.fromEnvironment(env)
    if core.appEnv == .dev, (wire.mode.servesAPI || circle.mode.servesAPI || finance.mode.canServeAPI || sports.mode.canServeAPI), wire.corpusEdge == nil {
      throw WireDiscoveryConfigError.missingCorpusEdgeForDevelopment
    }
    if core.appEnv == .prod, wire.corpusEdge != nil || sports.corpusEdge != nil {
      throw WireDiscoveryConfigError.remoteCorpusEdgeNotAllowedInProduction
    }
    return AppViewServiceConfig(
      core: core,
      thinAppView: thin,
      storeBackend: backend,
      wire: wire,
      circle: circle,
      finance: finance,
      sports: sports,
      podcastsEnabled: env["PODCASTS_ENABLED"]?.lowercased() == "true",
      toolsEnabled: env["TOOLS_ENABLED"].flatMap { raw in
        let value = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        return value.isEmpty ? nil : value.lowercased() == "true"
      } ?? (core.appEnv != .prod),
      semble: semble
    )
  }
}
