import Foundation
import NIOSSL
import PostgresNIO

enum SchemaPostgresConfiguration {
  static func make(url: String) throws -> PostgresClient.Configuration {
    guard let parts = URLComponents(string: url),
      ["postgres", "postgresql"].contains(parts.scheme), let host = parts.host, !host.isEmpty,
      let username = parts.user, !username.isEmpty, parts.path.count > 1
    else { throw SchemaReadinessError.invalidConfiguration("Invalid PostgreSQL configuration") }
    let parameters = parts.queryItems ?? []
    guard parameters.filter({ $0.name == "sslmode" }).count <= 1,
      !parameters.contains(where: { $0.name.lowercased().hasPrefix("ssl") && $0.name != "sslmode" })
    else { throw SchemaReadinessError.invalidConfiguration("Unsupported PostgreSQL TLS settings") }
    let mode = parameters.first(where: { $0.name == "sslmode" })?.value ?? "prefer"
    let settings = try tlsConfiguration(mode: mode)
    let tls: PostgresClient.Configuration.TLS
    switch mode {
    case "disable": tls = .disable
    case "prefer", "allow": tls = .prefer(settings)
    default: tls = .require(settings)
    }
    var configuration = PostgresClient.Configuration(
      host: host, port: parts.port ?? 5432, username: username, password: parts.password,
      database: String(parts.path.dropFirst()), tls: tls)
    configuration.options.maximumConnections = 1
    configuration.options.minimumConnections = 0
    configuration.options.connectionIdleTimeout = .seconds(1)
    configuration.options.connectTimeout = .seconds(5)
    configuration.options.additionalStartupParameters = [
      ("application_name", "socialwire-schema-ready"),
      ("statement_timeout", "5000"), ("lock_timeout", "1000"),
      ("default_transaction_read_only", "on"),
    ]
    return configuration
  }

  static func tlsConfiguration(mode: String) throws -> TLSConfiguration {
    var settings = TLSConfiguration.makeClientConfiguration()
    switch mode {
    // Encryption selection is independent of certificate authentication.
    // Prefer/allow retain the existing fallback when the server has no TLS support.
    case "disable", "prefer", "allow", "require": settings.certificateVerification = .none
    case "verify-ca": settings.certificateVerification = .noHostnameVerification
    case "verify-full": settings.certificateVerification = .fullVerification
    default: throw SchemaReadinessError.invalidConfiguration("Unsupported PostgreSQL TLS mode")
    }
    return settings
  }
}
