import IndexingWorkerCore
import IndexingWorkerHost
import Logging
import Testing

@Suite("Active indexing service scope")
struct IndexingWorkerActiveScopeTests {
  @Test func refusesCoordinatorWithoutAuthority() async {
    await #expect(throws: AppViewWorkerRuntimeError.coordinatorRequiresAuthority) {
      try await AppViewWorkerHost.run(
        environment: [:], role: .coordinator,
        logger: Logger(label: "scope"))
    }
  }

  @Test func refusesRetiredCombinedRole() async {
    await #expect(throws: AppViewWorkerRuntimeError.unsupportedRole) {
      try await AppViewWorkerHost.run(
        environment: [:], role: .combined,
        logger: Logger(label: "scope"))
    }
  }

  @Test func requiresDatabaseBeforeStartingResources() async throws {
    let config = try IndexingWorkerConfig.load(["INDEXING_WORKER_ROLE": "projection"])
    await #expect(throws: IndexingWorkerHostError.missingDatabaseURL) {
      try await IndexingWorkerHost.run(
        environment: [:], config: config,
        logger: Logger(label: "scope"), terminateUnresponsiveProcess: {})
    }
  }

  @Test(arguments: ["WIRE_FEED_MODE", "FINANCE_FEED_MODE", "SPORTS_FEED_MODE"])
  func refusesDormantWorkloadActivation(key: String) async throws {
    let config = try IndexingWorkerConfig.load(["INDEXING_WORKER_ROLE": "coordinator"])
    await #expect(throws: IndexingWorkerHostError.inactiveWorkloadRequested(key)) {
      try await IndexingWorkerHost.run(
        environment: ["DATABASE_URL": "postgresql://unused", key: "visible"],
        config: config, logger: Logger(label: "scope"), terminateUnresponsiveProcess: {})
    }
  }
}
