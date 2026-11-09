import Foundation
import GatewayCore
import Hummingbird
import HummingbirdTesting
import Logging
import Testing

@testable import AppView

@Suite struct StandaloneAdmissionTests {
  @Test func preservesProGateWhileAllowingEventCollection() async throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).path
    defer {
      for suffix in ["", "-wal", "-shm"] {
        try? FileManager.default.removeItem(atPath: path + suffix)
      }
    }
    let runtime = try runtime(path: path)
    let router = Router(context: GatewayRequestContext.self)
    router.add(middleware: SocialDraftTestAuth(owner: "did:plc:free-viewer"))
    router.add(middleware: StandaloneAdmissionMiddleware(runtime: runtime))
    router.get("/v1/analytics/summary") { _, _ in Response(status: .ok) }
    router.post("/v1/analytics/refresh") { _, _ in Response(status: .ok) }
    router.post("/v1/analytics/events") { _, _ in Response(status: .accepted) }
    try await Application(router: router).test(.router) { client in
      let summary = try await client.execute(uri: "/v1/analytics/summary", method: .get)
      let refresh = try await client.execute(uri: "/v1/analytics/refresh", method: .post)
      let events = try await client.execute(uri: "/v1/analytics/events", method: .post)
      #expect(summary.status == .forbidden)
      #expect(refresh.status == .forbidden)
      #expect(events.status == .accepted)
    }
  }

  @Test func limitsTransfersAndReleasesCompletedResponseLease() async throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).path
    defer {
      for suffix in ["", "-wal", "-shm"] {
        try? FileManager.default.removeItem(atPath: path + suffix)
      }
    }
    let runtime = try runtime(path: path)
    let first = try await runtime.admission.acquire(owner: "did:plc:viewer", transfer: true)
    let second = try await runtime.admission.acquire(owner: "did:plc:viewer", transfer: true)
    let router = Router(context: GatewayRequestContext.self)
    router.add(middleware: SocialDraftTestAuth(owner: "did:plc:viewer"))
    router.add(middleware: StandaloneAdmissionMiddleware(runtime: runtime))
    router.get("/xrpc/app.thesocialwire.socialDraft.getAttachment") { _, _ in Response(status: .ok)
    }
    try await Application(router: router).test(.router) { client in
      let denied = try await client.execute(
        uri: "/xrpc/app.thesocialwire.socialDraft.getAttachment", method: .get)
      #expect(denied.status == .tooManyRequests)
      await runtime.admission.release(second)
      for _ in 0..<3 {
        let accepted = try await client.execute(
          uri: "/xrpc/app.thesocialwire.socialDraft.getAttachment", method: .get)
        #expect(accepted.status == .ok)
      }
    }
    await runtime.admission.release(first)
  }

  private func runtime(path: String) throws -> StandaloneEdgeRuntime {
    let loaded = try StandaloneEdgeConfiguration.load(
      [
        "BACKEND_MODE": "standalone", "SQLITE_DB_PATH": path,
        "PDS_ATTESTATION_RECEIPT_SECRET": "standalone-test-attestation-secret-at-least-32-bytes",
        "SOCIALWIRE_PRO_GATING_ENABLED": "true",
      ], appEnvironment: .local)
    let configuration = try #require(loaded)
    return StandaloneEdgeRuntime(
      configuration: configuration,
      cache: try SQLiteCache(path: path, logger: Logger(label: "standalone-policy-test")),
      environment: "local")
  }
}
