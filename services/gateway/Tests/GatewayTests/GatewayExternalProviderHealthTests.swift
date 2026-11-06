import AsyncHTTPClient
import Foundation
import GatewayCore
import Hummingbird
import HummingbirdTesting
import Logging
import OperationsCore
import Testing

@testable import Gateway

struct GatewayExternalProviderHealthTests {
  @Test func disabledCollectorNeverContactsRetainedOrigins() async throws {
    let health = GatewayIngestionHealth(baseURL: "http://retired-pool") { _ in
      Issue.record("Disabled global ingestion must not be contacted")
      return 503
    }
    try await health.collectOnce()
    try await health.runForever()
    let snapshot = await health.snapshot()
    #expect(snapshot.poolReadiness == "disabled")
    #expect(snapshot.freshness == .unknown && snapshot.completeness == .unknown)
    #expect(snapshot.checkedAt == nil && snapshot.validUntil == nil)
  }

  @Test(arguments: [true, false])
  func externalServingHealthRetainsAppViewReadiness(appViewReady: Bool) async throws {
    let retained = GatewayIngestionHealth(baseURL: "http://old-pool", enabled: true) { _ in 503 }
    try await retained.collectOnce()
    let disabled = GatewayIngestionHealth(baseURL: "http://retired-pool") { _ in
      Issue.record("Disabled ingestion must not affect the heartbeat")
      return 503
    }
    let upstream = Router()
    upstream.get("/readyz") { _, _ in
      Response(status: appViewReady ? .ok : .serviceUnavailable)
    }
    let client = HTTPClient(eventLoopGroupProvider: .singleton)
    let path = FileManager.default.temporaryDirectory
      .appendingPathComponent("gateway-external-health-\(UUID()).sqlite").path
    defer { try? FileManager.default.removeItem(atPath: path) }
    do {
      try await Application(router: upstream).test(.live) { upstream in
        let port = try #require(upstream.port)
        let config = try GatewayServiceConfig.fromEnvironment([
          "APP_ENV": "local", "SQLITE_DB_PATH": path,
          "APPVIEW_BASE_URL": "http://localhost:\(port)",
          "PROJECTION_POOL_BASE_URL": "http://old-pool",
        ])
        let logger = Logger(label: "external-health-test")
        let operations = try SQLiteOperationsStore(path: path, environment: "test", logger: logger)
        let router = GatewayRouterBuilder.router(
          config: config, httpClient: client, cache: try SQLiteCache(path: path, logger: logger),
          ingestionHealth: retained, operationsStore: operations, logger: logger)
        try await Application(router: router).test(.router) { gateway in
          let ready = try await gateway.execute(uri: "/readyz", method: .get)
          #expect(ready.status == (appViewReady ? .ok : .serviceUnavailable))
          let response = try await gateway.execute(uri: "/freshness", method: .get)
          let body = try #require(
            JSONSerialization.jsonObject(with: Data(buffer: response.body)) as? [String: Any])
          #expect(body["poolReadiness"] as? String == "disabled")
          #expect(body["freshness"] as? String == "unknown")
          #expect(body["completeness"] as? String == "unknown")
          #expect(body["checkedAt"] == nil && body["validUntil"] == nil)
        }
        let evidence = try await Serve.gatewayDependencyProbe(
          appViewBaseURL: "http://localhost:\(port)", ingestionHealth: disabled, httpClient: client)()
        #expect(evidence.readiness == (appViewReady ? .healthy : .degraded))
        #expect(evidence.freshness == .unknown && evidence.completeness == .unknown)
        #expect(evidence.dependencyState["projection_pool"] == "disabled")
        #expect(evidence.requiredDependencyKeys == ["appview"])
      }
    } catch {
      try? await client.shutdown()
      throw error
    }
    try await client.shutdown()
  }
}
