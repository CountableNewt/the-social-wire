import Foundation
import PostgresNIO

public enum PostgresConfiguration {
  public static func make(
    url: String, name: String, maxOpen: Int, minIdle: Int, idleTimeout: Double,
    readOnly: Bool = false
  ) throws -> PostgresClient.Configuration {
    guard let parts = URLComponents(string: url),
      ["postgres", "postgresql"].contains(parts.scheme), let host = parts.host,
      let username = parts.user
    else { throw IngressError.invalidConfiguration("Invalid PostgreSQL configuration") }
    let mode = parts.queryItems?.first(where: { $0.name == "sslmode" })?.value ?? "prefer"
    let tls: PostgresClient.Configuration.TLS
    switch mode {
    case "disable": tls = .disable
    case "prefer", "allow": tls = .prefer(.makeClientConfiguration())
    case "require", "verify-ca", "verify-full": tls = .require(.makeClientConfiguration())
    default: throw IngressError.invalidConfiguration("Unsupported PostgreSQL TLS mode")
    }
    var configuration = PostgresClient.Configuration(
      host: host, port: parts.port ?? 5432, username: username, password: parts.password,
      database: String(parts.path.dropFirst()), tls: tls)
    configuration.options.maximumConnections = maxOpen
    configuration.options.minimumConnections = minIdle
    configuration.options.connectionIdleTimeout = .milliseconds(Int64(idleTimeout * 1000))
    configuration.options.connectTimeout = .seconds(5)
    configuration.options.additionalStartupParameters = [
      ("application_name", String(name.prefix(63))),
      ("statement_timeout", readOnly ? "5000" : "30000"), ("lock_timeout", "1000"),
    ]
    if readOnly {
      configuration.options.additionalStartupParameters.append(
        ("default_transaction_read_only", "on"))
    }
    return configuration
  }
}
