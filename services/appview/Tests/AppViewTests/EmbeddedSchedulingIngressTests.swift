import AsyncHTTPClient
import Foundation
import GatewayCore
import Hummingbird
import HummingbirdTesting
import Logging
import Testing
import ThinAppViewCore

@testable import AppView

@Suite struct EmbeddedSchedulingIngressTests {
  @Test func schedulerIsOptInAndProtectsActorRoutes() async throws {
    let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: directory) }
    let path = directory.appendingPathComponent("social-wire.sqlite").path
    var environment = [
      "APP_ENV": "local", "BACKEND_MODE": "standalone", "SQLITE_DB_PATH": path,
      "PDS_ATTESTATION_RECEIPT_SECRET": "standalone-test-attestation-secret-at-least-32-bytes",
      "ENABLE_THIN_APPVIEW": "true", "TOOLS_ENABLED": "false",
      "OAUTH_IOS_METADATA_ORIGIN": "https://api.testing.thesocialwire.app",
    ]
    let config = try AppViewServiceConfig.fromEnvironment(environment)
    #expect(try EmbeddedSchedulingConfiguration.load(environment, config: config) == nil)
    environment["EMBEDDED_SCHEDULING_ENABLED"] = "true"
    environment["EMBEDDED_SCHEDULING_PUBLIC_ORIGIN"] = "https://api.testing.thesocialwire.app"
    #expect(throws: EmbeddedSchedulingConfiguration.ConfigurationError.invalidStorageKey) {
      try EmbeddedSchedulingConfiguration.load(environment, config: config)
    }
    environment["EMBEDDED_SCHEDULING_STORAGE_KEY"] = Data(repeating: 23, count: 32).base64EncodedString()
    let scheduling = try #require(try EmbeddedSchedulingConfiguration.load(environment, config: config))
    #expect(!scheduling.importLegacySchedules)
    environment["EMBEDDED_SCHEDULING_IMPORT_LEGACY"] = "true"
    #expect(try EmbeddedSchedulingConfiguration.load(environment, config: config)?.importLegacySchedules == true)
    #expect(scheduling.databasePath == directory.appendingPathComponent("scheduling.sqlite").path)

    let logger = Logger(label: "embedded-scheduling-test")
    let store = try SQLiteThinAppViewStore(path: path, logger: logger)
    let edge = StandaloneEdgeRuntime(configuration: try #require(config.standaloneEdge),
      cache: try SQLiteCache(path: path, logger: logger), environment: "local")
    let http = HTTPClient(eventLoopGroupProvider: .singleton)
    do {
      let backend = try await scheduling.open(httpClient: http, logger: logger)
      let router = AppViewRouterBuilder.router(config: config, httpClient: http,
        thinAppViewStore: store, projectionCache: nil, standaloneEdge: edge,
        schedulingBackend: backend, logger: logger)
      try await Application(router: router).test(.router) { client in
        let metadata = try await client.execute(uri: "/oauth/scheduling/client-metadata.json", method: .get)
        #expect(metadata.status == .ok)
        let document = try JSONSerialization.jsonObject(with: Data(buffer: metadata.body)) as? [String: Any]
        #expect(document?["client_id"] as? String == "https://api.testing.thesocialwire.app/oauth/scheduling/client-metadata.json")
        let response = try await client.execute(uri: "/xrpc/at.skej.schedule.list", method: .get)
        #expect(response.status == .unauthorized)
        let oldRoute = try await client.execute(uri: "/xrpc/at.skej.team.list", method: .get)
        #expect(oldRoute.status == .notFound)
      }
    } catch {
      try? await http.shutdown()
      throw error
    }
    try await http.shutdown()
  }
}
