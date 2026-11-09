import AsyncHTTPClient
import Foundation
import GatewayCore
import Hummingbird
import HummingbirdTesting
import Logging
import Testing
import ThinAppViewCore

@testable import AppView

struct StandaloneMediaReadinessTests {
  @Test func requiresStartupAndHealthyProcessorWithoutSendingCredentials() async throws {
    let probe = StandaloneMediaReadinessProbe(baseURL: "http://127.0.0.1:9080/") { url in
      #expect(["http://127.0.0.1:9080/startupz", "http://127.0.0.1:9080/healthz"].contains(url))
      return (
        200, Data((url.hasSuffix("startupz") ? #"{"ready":true}"# : #"{"status":"ok"}"#).utf8)
      )
    }
    try await probe.run()
    for url in [
      nil, "https://127.0.0.1:9080", "http://media.railway.internal",
      "http://user:secret@127.0.0.1:9080",
    ] as [String?] {
      let invalid = StandaloneMediaReadinessProbe(baseURL: url) { _ in
        Issue.record("Invalid processor origin must not be requested")
        return (200, Data())
      }
      await #expect(throws: HTTPError.self) { try await invalid.run() }
    }
  }

  @Test func refusesStartingUnhealthyMalformedAndUnavailableProcessors() async throws {
    for (status, startup, health) in [
      (200, #"{"ready":false}"#, #"{"status":"ok"}"#),
      (200, #"{"ready":true}"#, #"{"status":"unhealthy"}"#),
      (503, #"{"ready":true}"#, #"{"status":"ok"}"#),
      (200, "not json", #"{"status":"ok"}"#),
    ] {
      let probe = StandaloneMediaReadinessProbe(baseURL: "http://127.0.0.1:9080") { url in
        (status, Data((url.hasSuffix("startupz") ? startup : health).utf8))
      }
      await #expect(throws: HTTPError.self) { try await probe.run() }
    }
    let unavailable = StandaloneMediaReadinessProbe(baseURL: "http://127.0.0.1:9080") { _ in
      throw HTTPClientError.deadlineExceeded
    }
    await #expect(throws: HTTPError.self) { try await unavailable.run() }
  }

  @Test func processorFailureFailsReadinessWhileLivenessStaysAvailable() async throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).path
    defer {
      for suffix in ["", "-wal", "-shm"] {
        try? FileManager.default.removeItem(atPath: path + suffix)
      }
    }
    let config = try AppViewServiceConfig.fromEnvironment([
      "APP_ENV": "local", "BACKEND_MODE": "standalone", "SQLITE_DB_PATH": path,
      "PDS_ATTESTATION_RECEIPT_SECRET": "standalone-test-attestation-secret-at-least-32-bytes",
      "ENABLE_THIN_APPVIEW": "true", "PODCASTS_ENABLED": "true", "TOOLS_ENABLED": "false",
    ])
    let logger = Logger(label: "standalone-media-readiness")
    let store = try SQLiteThinAppViewStore(path: path, logger: logger)
    let http = HTTPClient(eventLoopGroupProvider: .singleton)
    do {
      let probe = StandaloneMediaReadinessProbe(baseURL: "http://127.0.0.1:9080") { _ in
        throw HTTPClientError.deadlineExceeded
      }
      let router = AppViewRouterBuilder.router(
        config: config, httpClient: http, thinAppViewStore: store,
        projectionCache: nil, mediaReadiness: probe, logger: logger)
      try await Application(router: router).test(.router) { client in
        let readiness = try await client.execute(uri: "/readyz", method: .get)
        let liveness = try await client.execute(uri: "/livez", method: .get)
        #expect(readiness.status == .serviceUnavailable)
        #expect(liveness.status == .ok)
      }
    } catch {
      try? await http.shutdown()
      throw error
    }
    try await http.shutdown()
  }
}
