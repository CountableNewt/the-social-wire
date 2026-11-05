import Foundation
import Testing

@testable import IndexingWorkerCore

@Suite("Indexing worker supervision")
struct IndexingWorkerRuntimeTests {
  @Test func rejectsMissingAuthorityAndFactory() throws {
    let config = try IndexingWorkerConfig.load(["INDEXING_WORKER_ROLE": "coordinator"])
    #expect(throws: IndexingWorkerRuntimeError.coordinatorRequiresAuthority) {
      try IndexingWorkerRuntime(
        config: config, lanes: [.appView], factories: [.appView: { Component() }],
        terminate: { _ in })
    }
    #expect(throws: IndexingWorkerRuntimeError.missingLaneFactory(.appView)) {
      try IndexingWorkerRuntime(
        config: config, lanes: [.appView], factories: [:], terminate: { _ in })
    }
    #expect(throws: IndexingWorkerRuntimeError.noActiveLanes) {
      try IndexingWorkerRuntime(config: config, lanes: [], factories: [:], terminate: { _ in })
    }
  }

  @Test func independentlyRestartsFailedProjectionAndJoinsCancelledHosts() async throws {
    let config = try IndexingWorkerConfig.load(["INDEXING_WORKER_ROLE": "projection"])
    let counter = Counter()
    let (events, continuation) = AsyncStream<String>.makeStream()
    let runtime = try IndexingWorkerRuntime(
      config: config, lanes: [.appView, .wire],
      factories: [
        .appView: {
          let attempt = await counter.increment(.appView)
          return Component(run: {
            continuation.yield("app\(attempt)")
            if attempt == 1 { throw Failure.workload }
            defer { continuation.yield("app-stopped") }
            try await Task.sleep(for: .seconds(3_600))
          })
        },
        .wire: {
          _ = await counter.increment(.wire)
          return Component(run: {
            continuation.yield("wire")
            defer { continuation.yield("wire-stopped") }
            try await Task.sleep(for: .seconds(3_600))
          })
        },
      ], sleep: { _ in try Task.checkCancellation() }, terminate: { _ in }
    )
    let task = Task { try await runtime.run() }
    var iterator = events.makeAsyncIterator()
    var started = Set<String>()
    while !started.contains("app2") || !started.contains("wire") {
      if let value = await iterator.next() { started.insert(value) }
    }
    #expect(await counter.count(.appView) == 2)
    #expect(await counter.count(.wire) == 1)
    try await runtime.ready()
    task.cancel()
    await #expect(throws: CancellationError.self) { try await task.value }
    var stopped = Set<String>()
    while stopped.count < 2 {
      if let value = await iterator.next(), value.hasSuffix("stopped") { stopped.insert(value) }
    }
  }

  @Test func standbyIsReadyOnlyWithFreshControlEvidence() async throws {
    let config = try IndexingWorkerConfig.load(["INDEXING_WORKER_ROLE": "coordinator"])
    let state = IndexingWorkerLaneState()
    let now = Date(timeIntervalSince1970: 100)
    let runtime = try IndexingWorkerRuntime(
      config: config, lanes: [.appView], factories: [.appView: { Component() }],
      supervisor: Standby(), state: state, now: { now }, terminate: { _ in }
    )
    await #expect(throws: IndexingWorkerRuntimeError.controlEvidenceUnavailable) {
      try await runtime.ready()
    }
    await state.record(.standby, for: .appView, at: now)
    try await runtime.ready()
    await state.record(.operationStarted, for: .appView, at: now)
    await #expect(throws: IndexingWorkerRuntimeError.laneUnavailable(.appView)) {
      try await runtime.ready()
    }
  }

  @Test func coordinatorCancellationJoinsSupervisorWithoutHangingWatchdog() async throws {
    let config = try IndexingWorkerConfig.load(["INDEXING_WORKER_ROLE": "coordinator"])
    let (events, continuation) = AsyncStream<Void>.makeStream()
    let runtime = try IndexingWorkerRuntime(
      config: config, lanes: [.appView], factories: [.appView: { Component() }],
      supervisor: Standby(started: { continuation.yield(()) }), terminate: { _ in }
    )
    let task = Task { try await runtime.run() }
    var iterator = events.makeAsyncIterator()
    _ = await iterator.next()
    try await runtime.ready()
    task.cancel()
    await #expect(throws: CancellationError.self) { try await task.value }
  }

  private enum Failure: Error { case workload }

  private struct Component: IndexingWorkerComponent {
    var run: @Sendable () async throws -> Void = {}
    func run(authority: IndexingWorkerLeaseAuthority?) async throws { try await run() }
    func startup() async throws {}
    func ready() async throws {}
  }

  private actor Counter {
    var counts: [IndexingWorkerLane: Int] = [:]
    func increment(_ lane: IndexingWorkerLane) -> Int {
      counts[lane, default: 0] += 1
      return counts[lane]!
    }
    func count(_ lane: IndexingWorkerLane) -> Int { counts[lane, default: 0] }
  }

  private struct Standby: IndexingWorkerLeaseSupervisor {
    var started: @Sendable () -> Void = {}
    func run(
      configuration: IndexingWorkerLeaseConfiguration,
      onEvent: @escaping @Sendable (IndexingWorkerLeaseEvent) async -> Void,
      operation: @escaping @Sendable (IndexingWorkerLeaseAuthority) async throws -> Void
    ) async throws {
      await onEvent(.standby)
      started()
      try await Task.sleep(for: .seconds(3_600))
    }
  }
}
