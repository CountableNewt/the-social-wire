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
    let serviceLogger = logger
    let environment = AppEnvironmentLoader.mergeProcessWithDotenv()
    let config = OperationsServiceConfig.fromEnvironment(environment)
    let port = port ?? Int(environment["PORT"] ?? "8083") ?? 8083
    let host = hostname ?? environment["BIND_HOST"] ?? "0.0.0.0"
    let httpClient = HTTPClient(eventLoopGroupProvider: .singleton)
    defer { Task { try? await httpClient.shutdown() } }

    switch config.database {
    case .sqlite(let path):
      let store = try SQLiteOperationsStore(path: path, logger: serviceLogger)
      try await Self.runServer(
        config: config, store: store, httpClient: httpClient, logger: serviceLogger, host: host, port: port
      )
    case .postgres(let url):
      let pgConfig = try makePostgresConfig(from: url, logger: serviceLogger)
      let pool = PostgresClient(configuration: pgConfig, backgroundLogger: serviceLogger)
      let store = PostgresOperationsStore(pool: pool, logger: serviceLogger)
      try await withThrowingTaskGroup(of: Void.self) { group in
        group.addTask { await pool.run() }
        group.addTask {
          try await Self.runServer(
            config: config, store: store, httpClient: httpClient, logger: serviceLogger, host: host, port: port
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
    let telemetry = OperationsTelemetryBuffer(store: store, logger: logger)
    let router = OperationsRouterBuilder.router(
      config: config,
      httpClient: httpClient,
      store: store,
      telemetry: config.operations.enabled ? telemetry : nil,
      logger: logger
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
    let heartbeat = OperationsHeartbeatJob(store: store, service: "operations", environment: config.operations.environment, instanceId: config.operations.instanceId, logger: logger)
    try await withThrowingTaskGroup(of: Void.self) { group in
      group.addTask { try await app.run() }
      group.addTask { await evaluator.runForever() }
      if config.operations.enabled { group.addTask { await telemetry.runForever() } }
      group.addTask { await heartbeat.runForever() }
      try await group.next()
      group.cancelAll()
    }
  }
}
