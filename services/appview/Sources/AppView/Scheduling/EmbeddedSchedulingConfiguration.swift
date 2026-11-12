import AsyncHTTPClient
import Foundation
import Logging
import SkejBackend

struct EmbeddedSchedulingConfiguration {
  let origin: String
  let databasePath: String
  let encryptionKey: String
  let importLegacySchedules: Bool

  static func load(
    _ environment: [String: String], config: AppViewServiceConfig
  ) throws -> Self? {
    guard environment["EMBEDDED_SCHEDULING_ENABLED"] == "true" else { return nil }
    guard config.standaloneEdge != nil,
      case .sqlite(let appViewPath) = config.storeBackend,
      appViewPath.hasPrefix("/")
    else { throw ConfigurationError.persistentStandaloneStorageRequired }

    guard let origin = environment["EMBEDDED_SCHEDULING_PUBLIC_ORIGIN"],
      let url = URLComponents(string: origin),
      url.scheme == "https", let host = url.host, !host.isEmpty,
      url.user == nil, url.password == nil, url.path.isEmpty,
      url.query == nil, url.fragment == nil
    else { throw ConfigurationError.invalidPublicOrigin }

    guard let key = environment["EMBEDDED_SCHEDULING_STORAGE_KEY"],
      Data(base64Encoded: key)?.count == 32
    else { throw ConfigurationError.invalidStorageKey }

    let directory = URL(fileURLWithPath: appViewPath).deletingLastPathComponent()
    return Self(origin: origin, databasePath: directory.appendingPathComponent("scheduling.sqlite").path,
      encryptionKey: key,
      importLegacySchedules: environment["EMBEDDED_SCHEDULING_IMPORT_LEGACY"] == "true")
  }

  func open(httpClient: HTTPClient, logger: Logger) async throws -> EmbeddedSkejBackend {
    let transport = SchedulingPublicHTTPTransport(httpClient: httpClient)
    return try await EmbeddedSkejBackend.open(publicOrigin: origin, sqlitePath: databasePath,
      authEncryptionSecret: encryptionKey, importLegacySchedules: importLegacySchedules, logger: logger,
      transport: transport.execute)
  }

  enum ConfigurationError: LocalizedError {
    case persistentStandaloneStorageRequired
    case invalidPublicOrigin
    case invalidStorageKey

    var errorDescription: String? {
      switch self {
      case .persistentStandaloneStorageRequired:
        "Embedded scheduling requires standalone AppView with an absolute SQLite database path on its persistent volume."
      case .invalidPublicOrigin:
        "EMBEDDED_SCHEDULING_PUBLIC_ORIGIN must be an HTTPS origin."
      case .invalidStorageKey:
        "EMBEDDED_SCHEDULING_STORAGE_KEY must be a Base64-encoded 32-byte key."
      }
    }
  }
}
