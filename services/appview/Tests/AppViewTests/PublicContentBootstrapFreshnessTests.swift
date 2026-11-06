import AsyncHTTPClient
import Foundation
import GatewayCore
import Logging
import Testing
import ThinAppViewCore
@testable import AppView

@Suite struct PublicContentBootstrapFreshnessTests {
  private actor Probe {
    var provider = 0
    var recent = 0
    func stall() async throws { provider += 1; try await Task.sleep(for: .seconds(60)) }
    func enumerate(recentOnly: Bool) -> Int { if recentOnly { recent += 1 }; return 1 }
  }
  private struct StalledProvider: PublicContentDiscovering {
    let probe: Probe
    func discover(scope: PublicContentDiscoveryScope) async throws -> Int { try await probe.stall(); return 0 }
  }

  @Test func coldBootstrapFinishesFastPDSWhileProviderIsStalled() async throws {
    let probe = Probe(), logger = Logger(label: "public-bootstrap.test")
    let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).path
    defer { try? FileManager.default.removeItem(atPath: path) }
    let store = try SQLiteThinAppViewStore(path: path, logger: logger)
    let config = ThinAppViewConfig.fromEnvironment(["ENABLE_THIN_APPVIEW": "true"])
    let indexer = ThinAppViewIndexer(store: store, config: config, logger: logger)
    let http = HTTPClient(eventLoopGroupProvider: .singleton)
    let service = ThinAppViewEnrollService(store: store, indexer: indexer, httpClient: http,
      plcURL: "https://plc.directory", config: config, logger: logger,
      publicContentProvider: StalledProvider(probe: probe), publicContentPDS: { _, recent in await probe.enumerate(recentOnly: recent) })
    let enrollment = BootstrapEnrollment.start {
      _ = try? await service.refreshPublicContentFast(authorDid: "did:plc:aaaaaaaaaaaaaaaaaaaaaaaa",
        publicationAtUri: "at://did:plc:aaaaaaaaaaaaaaaaaaaaaaaa/site.standard.publication/main")
    }
    let until = ContinuousClock.now.advanced(by: .seconds(2))
    while ContinuousClock.now < until {
      let finished = await enrollment.isFinished
      let started = await probe.provider > 0
      if finished && started { break }
      try await Task.sleep(for: .milliseconds(10))
    }
    #expect(await enrollment.isFinished)
    #expect(await probe.provider == 1)
    #expect(await probe.recent == 1)
    await service.shutdown()
    try await http.shutdown()
  }

  @Test func malformedLegacyAliasesKeepAuthorOnlyPDSFallback() throws {
    let scope = try ThinAppViewEnrollService.discoveryScope(authorDid: "did:plc:aaaaaaaaaaaaaaaaaaaaaaaa",
      publicationAtUri: "at://did:plc:aaaaaaaaaaaaaaaaaaaaaaaa/com.standard.publication/main", publicationSiteUrls: ["not-a-public-url"])
    #expect(scope.publicationKeys.isEmpty)
  }
}
