import ArgumentParser
import AsyncHTTPClient
import Foundation
import GatewayCore
import Hummingbird
import Logging
import OperationsCore
import PostgresNIO
import ThinAppViewCore

@main
@available(macOS 10.15, macCatalyst 13, iOS 13, tvOS 13, watchOS 6, *)
struct OperationsCommand: AsyncParsableCommand {
  static let configuration = CommandConfiguration(abstract: "The Social Wire operations control plane")

  @Option(name: .long) var port: Int?
  @Option(name: .long) var hostname: String?

  mutating func run() async throws {
    var logger = Logger(label: "com.thesocialwire.operations")
    logger.logLevel = .info
    let environment = AppEnvironmentLoader.mergeProcessWithDotenv()
    let config = OperationsServiceConfig.fromEnvironment(environment)
    let port = port ?? Int(environment["PORT"] ?? "8083") ?? 8083
    let host = hostname ?? environment["BIND_HOST"] ?? "0.0.0.0"
    let httpClient = HTTPClient(eventLoopGroupProvider: .singleton)
    defer { Task { try? await httpClient.shutdown() } }

    switch config.database {
    case .sqlite(let path):
      let store = try SQLiteOperationsStore(path: path, logger: logger)
      try await Self.runServer(
        config: config, store: store, httpClient: httpClient, logger: logger, host: host, port: port
      )
    case .postgres(let url):
      let pgConfig = try makePostgresConfig(from: url, logger: logger)
      let pool = PostgresClient(configuration: pgConfig, backgroundLogger: logger)
      let store = PostgresOperationsStore(pool: pool, logger: logger)
      try await withThrowingTaskGroup(of: Void.self) { group in
        group.addTask { await pool.run() }
        group.addTask {
          try await Self.runServer(
            config: config, store: store, httpClient: httpClient, logger: logger, host: host, port: port
          )
        }
        try await group.next()
        group.cancelAll()
      }
    }
  }

  private static func runServer(
    config: OperationsServiceConfig,
    store: any OperationsStore,
    httpClient: HTTPClient,
    logger: Logger,
    host: String,
    port: Int
  ) async throws {
    let router = OperationsRouterBuilder.router(
      config: config, httpClient: httpClient, store: store, logger: logger
    )
    let app = Application(
      router: router,
      configuration: .init(address: .hostname(host, port: port))
    )
    let webhook: OperationsWebhookDelivery?
    if let url = config.operations.webhookURL, let secret = config.operations.webhookSecret {
      webhook = OperationsWebhookDelivery(url: url, secret: secret, httpClient: httpClient, logger: logger)
    } else {
      webhook = nil
    }
    let evaluator = AlertEvaluator(store: store, config: config.operations, logger: logger, webhook: webhook)
    try await withThrowingTaskGroup(of: Void.self) { group in
      group.addTask { try await app.run() }
      group.addTask { await evaluator.runForever() }
      try await group.next()
      group.cancelAll()
    }
  }
}
