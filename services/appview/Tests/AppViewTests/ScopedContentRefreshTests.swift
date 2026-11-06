import Foundation
import Logging
import NIOConcurrencyHelpers
import Testing
import ThinAppViewCore
@testable import AppView

@Suite struct ScopedContentRefreshTests {
  private actor Counter {
    var count = 0
    func next() -> Int { count += 1; return count }
  }
  private struct Provider: PublicContentDiscovering {
    let counter: Counter
    var fails = false
    var delay = false
    func discover(scope: PublicContentDiscoveryScope) async throws -> Int {
      _ = await counter.next()
      if delay { try await Task.sleep(for: .milliseconds(40)) }
      if fails { throw PublicContentDiscoveryError.unavailable }
      return 2
    }
  }

  @Test func providerHintsAlwaysRetainPDSHistoryAndBoundRefreshFrequency() async throws {
    let provider = Counter(), pds = Counter()
    let clock = NIOLockedValueBox(Date())
    let coordinator = ScopedContentRefresh(provider: Provider(counter: provider), logger: Logger(label: "scope.test"),
      now: { clock.withLockedValue { $0 } }) { _ in _ = await pds.next(); return 5 }
    let scope = try PublicContentDiscoveryScope(authorDid: "did:plc:aaaaaaaaaaaaaaaaaaaaaaaa", publicationKeys: ["https://publisher.example"])
    #expect(try await coordinator.refresh(scope: scope) == 7)
    #expect(try await coordinator.refresh(scope: scope) == 0)
    let providerCount = await provider.count
    let pdsCount = await pds.count
    #expect(providerCount == 1 && pdsCount == 1)
    clock.withLockedValue { $0 = $0.addingTimeInterval(61) }
    #expect(try await coordinator.refresh(scope: scope) == 7)
    let refreshedProviderCount = await provider.count
    let refreshedPDSCount = await pds.count
    #expect(refreshedProviderCount == 2 && refreshedPDSCount == 2)
  }

  @Test func providerFailureAndAuthorOnlyScopesStillEnumeratePDS() async throws {
    let provider = Counter(), pds = Counter()
    let coordinator = ScopedContentRefresh(provider: Provider(counter: provider, fails: true), logger: Logger(label: "scope.test")) { _ in _ = await pds.next(); return 5 }
    let scope = try PublicContentDiscoveryScope(authorDid: "did:plc:aaaaaaaaaaaaaaaaaaaaaaaa", publicationKeys: ["https://publisher.example"])
    #expect(try await coordinator.refresh(scope: scope) == 5)
    #expect(await pds.count == 1)
    #expect(await provider.count == 1)
    let authorOnly = try PublicContentDiscoveryScope(authorDid: "did:plc:bbbbbbbbbbbbbbbbbbbbbbbb", publicationKeys: [])
    #expect(try await coordinator.refresh(scope: authorOnly) == 5)
    #expect(await provider.count == 1)
    #expect(await pds.count == 2)
  }

  @Test func concurrentRefreshesCoalesceAndCancellationDoesNotBecomeFresh() async throws {
    let provider = Counter(), pds = Counter()
    let coordinator = ScopedContentRefresh(provider: Provider(counter: provider, delay: true), logger: Logger(label: "scope.test")) { _ in _ = await pds.next(); return 5 }
    let scope = try PublicContentDiscoveryScope(authorDid: "did:plc:aaaaaaaaaaaaaaaaaaaaaaaa", publicationKeys: ["https://publisher.example"])
    try await withThrowingTaskGroup(of: Int.self) { group in
      for _ in 0..<10 { group.addTask { try await coordinator.refresh(scope: scope) } }
      var total = 0
      for try await value in group { total += value }
      #expect(total >= 7 && total <= 70)
    }
    let providerCount = await provider.count
    let pdsCount = await pds.count
    #expect(providerCount == 1 && pdsCount == 1)
    let other = try PublicContentDiscoveryScope(authorDid: "did:plc:bbbbbbbbbbbbbbbbbbbbbbbb", publicationKeys: [])
    let cancelled = Task { try await coordinator.refresh(scope: other) }
    cancelled.cancel()
    do { _ = try await cancelled.value; Issue.record("Cancelled refresh succeeded") }
    catch { #expect(error is CancellationError) }
    #expect(try await coordinator.refresh(scope: other) == 5)
  }

  @Test func providerModeIsExplicitAndDefaultsEnabledForHostedConfiguration() throws {
    #expect(try PublicContentDiscoveryMode.load([:]) == .microcosm)
    #expect(try PublicContentDiscoveryMode.load(["PUBLIC_CONTENT_DISCOVERY_MODE": "off"]) == .off)
    #expect(throws: AppViewServiceConfigError.invalidPublicContentDiscoveryMode) {
      try PublicContentDiscoveryMode.load(["PUBLIC_CONTENT_DISCOVERY_MODE": "unknown"])
    }
  }
}
