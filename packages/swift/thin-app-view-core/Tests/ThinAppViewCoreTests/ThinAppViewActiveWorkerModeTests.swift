import Foundation
import Logging
import Testing

@testable import ThinAppViewCore

@Suite("Active Swift worker scope")
struct ThinAppViewActiveWorkerModeTests {
  @Test func requiresPDSAuthorityAdapters() async throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).path
    defer { try? FileManager.default.removeItem(atPath: path) }
    let store = try SQLiteThinAppViewStore(path: path, logger: Logger(label: "worker-scope"))
    let config = ThinAppViewConfig.fromEnvironment([
      "THIN_APPVIEW_JETSTREAM_MODE": "v2_authoritative"
    ])
    await #expect(throws: ThinAppViewWorkerRuntimeError.missingPDSResolution) {
      try await ThinAppViewWorkerRuntime.run(
        store: store, config: config, role: .projection,
        logger: Logger(label: "worker-scope"))
    }
  }

  @Test(arguments: ["v1_authoritative", "v2_shadow"])
  func refusesRetiredIngestionModes(mode: String) async throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).path
    defer { try? FileManager.default.removeItem(atPath: path) }
    let store = try SQLiteThinAppViewStore(path: path, logger: Logger(label: "worker-scope"))
    let config = ThinAppViewConfig.fromEnvironment(["THIN_APPVIEW_JETSTREAM_MODE": mode])
    await #expect(throws: ThinAppViewWorkerRuntimeError.unsupportedInactiveMode) {
      try await ThinAppViewWorkerRuntime.run(
        store: store, config: config, role: .projection,
        logger: Logger(label: "worker-scope"))
    }
  }

  @Test func refusesLegacyCombinedHost() async throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).path
    defer { try? FileManager.default.removeItem(atPath: path) }
    let store = try SQLiteThinAppViewStore(path: path, logger: Logger(label: "worker-scope"))
    let config = ThinAppViewConfig.fromEnvironment([
      "THIN_APPVIEW_JETSTREAM_MODE": "v2_authoritative"
    ])
    await #expect(throws: ThinAppViewWorkerRuntimeError.unsupportedInactiveMode) {
      try await ThinAppViewWorkerRuntime.run(
        store: store, config: config, role: .combined,
        logger: Logger(label: "worker-scope"))
    }
  }

  @Test func refusesTapAuthority() async throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).path
    defer { try? FileManager.default.removeItem(atPath: path) }
    let store = try SQLiteThinAppViewStore(path: path, logger: Logger(label: "worker-scope"))
    let config = ThinAppViewConfig.fromEnvironment([
      "THIN_APPVIEW_JETSTREAM_MODE": "v2_authoritative"
    ])
    let tap = TapConsumerConfiguration(
      mode: .authoritative, environment: "dev",
      baseURL: URL(string: "https://unused.example")!,
      channelURL: URL(string: "wss://unused.example/channel")!,
      adminPassword: "unused")
    await #expect(throws: ThinAppViewWorkerRuntimeError.unsupportedInactiveMode) {
      try await ThinAppViewWorkerRuntime.run(
        store: store, config: config, role: .projection,
        logger: Logger(label: "worker-scope"), tapConfiguration: tap)
    }
  }
}
