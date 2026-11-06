import Foundation
import PostgresNIO

enum SchemaPostgresConfiguration {
  static func make(url: String) throws -> PostgresClient.Configuration {
    guard let parts = URLComponents(string: url),
      ["postgres", "postgresql"].contains(parts.scheme), let host = parts.host, !host.isEmpty,
      let username = parts.user, !username.isEmpty, parts.path.count > 1
    else { throw SchemaReadinessError.invalidConfiguration("Invalid PostgreSQL configuration") }
    let mode = parts.queryItems?.first(where: { $0.name == "sslmode" })?.value ?? "prefer"
    let tls: PostgresClient.Configuration.TLS
    switch mode {
    case "disable": tls = .disable
    case "prefer", "allow": tls = .prefer(.makeClientConfiguration())
    case "require", "verify-ca", "verify-full": tls = .require(.makeClientConfiguration())
    default: throw SchemaReadinessError.invalidConfiguration("Unsupported PostgreSQL TLS mode")
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
}
