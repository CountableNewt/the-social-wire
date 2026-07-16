import Foundation
import GatewayCore
import OperationsCore

struct OperationsServiceConfig: Sendable {
  let core: GatewayConfig
  let operations: OperationsConfiguration
  let gatewayOperationsInternalSecret: String?
  let database: Database

  enum Database: Sendable {
    case sqlite(path: String)
    case postgres(url: String)
  }

  static func fromEnvironment(_ environment: [String: String]) -> OperationsServiceConfig {
    let core = GatewayConfig.fromEnvironment(environment)
    let operations = OperationsConfiguration.fromEnvironment(environment)
    let database: Database
    switch core.appEnv {
    case .local:
      database = .sqlite(path: environment["SQLITE_DB_PATH"] ?? "./social-wire-operations.sqlite")
    case .dev, .prod:
      guard let url = environment["SUPABASE_DATABASE_URL"], !url.isEmpty else {
        fatalError("SUPABASE_DATABASE_URL is required for the operations service")
      }
      database = .postgres(url: url)
    }
    return OperationsServiceConfig(
      core: core,
      operations: operations,
      gatewayOperationsInternalSecret: core.gatewayOperationsInternalSecret,
      database: database
    )
  }
}
