import Testing

@testable import ThinAppViewCore

struct ThinAppViewMaintenanceCacheProbeTests {
  @Test func unavailableCacheFailsReadinessProbe() async {
    let probe = ThinAppViewMaintenanceCacheProbe(acquire: { _ in nil }, release: { _ in
      Issue.record("A failed acquisition must not release another owner's lease")
    })
    await #expect(throws: ThinAppViewMaintenanceCacheProbe.Failure.unavailable) {
      try await probe.run()
    }
  }

  @Test func failOpenRebuildLeaseIsNotHealthyCacheEvidence() async {
    let probe = ThinAppViewMaintenanceCacheProbe(acquire: { _ in
      AppViewProjectionRefreshLease(key: "", owner: "fail-open", ttlMilliseconds: 10_000)
    }, release: { _ in Issue.record("Synthetic leases are not acquired Redis keys") })
    await #expect(throws: ThinAppViewMaintenanceCacheProbe.Failure.unavailable) {
      try await probe.run()
    }
  }

  @Test func successUsesUniqueKeysAndJoinsRelease() async throws {
    let cache = LeaseFixture()
    let probe = ThinAppViewMaintenanceCacheProbe(
      acquire: { await cache.acquire($0) }, release: { await cache.release($0) })
    try await probe.run()
    try await probe.run()
    #expect(await cache.resources.count == 2)
    #expect(await cache.released == 2)
  }

  private actor LeaseFixture {
    var resources: Set<String> = []
    var released = 0
    func acquire(_ resource: String) -> AppViewProjectionRefreshLease {
      resources.insert(resource)
      return AppViewProjectionRefreshLease(key: resource, owner: "fixture", ttlMilliseconds: 10_000)
    }
    func release(_ lease: AppViewProjectionRefreshLease) { released += 1 }
  }
}
