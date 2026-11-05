import Foundation

/// Supervises workload resources owned by fresh lane factories. The caller owns
/// the authority store; the worker does not lend its control pool to workloads.
public actor IndexingWorkerRuntime {
  public typealias Factory = @Sendable () async throws -> any IndexingWorkerComponent
  public let config: IndexingWorkerConfig
  public let state: IndexingWorkerLaneState
  private let lanes: [IndexingWorkerLane]
  private let factories: [IndexingWorkerLane: Factory]
  private let supervisor: (any IndexingWorkerLeaseSupervisor)?
  private let now: @Sendable () -> Date
  private let sleep: @Sendable (Duration) async throws -> Void
  private let onComponentError: @Sendable (IndexingWorkerLane, any Error) async -> Void
  private let terminate: @Sendable (IndexingWorkerLane) async -> Void
  private var active: [IndexingWorkerLane: any IndexingWorkerComponent] = [:]

  public init(
    config: IndexingWorkerConfig,
    lanes: [IndexingWorkerLane],
    factories: [IndexingWorkerLane: Factory],
    supervisor: (any IndexingWorkerLeaseSupervisor)? = nil,
    state: IndexingWorkerLaneState = .init(),
    now: @escaping @Sendable () -> Date = { Date() },
    sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) },
    onComponentError: @escaping @Sendable (IndexingWorkerLane, any Error) async -> Void = { _, _ in
    },
    terminate: @escaping @Sendable (IndexingWorkerLane) async -> Void
  ) throws {
    guard !lanes.isEmpty else { throw IndexingWorkerRuntimeError.noActiveLanes }
    for lane in lanes where factories[lane] == nil {
      throw IndexingWorkerRuntimeError.missingLaneFactory(lane)
    }
    if config.role == .coordinator && supervisor == nil {
      throw IndexingWorkerRuntimeError.coordinatorRequiresAuthority
    }
    self.config = config
    self.lanes = IndexingWorkerLane.allCases.filter { lanes.contains($0) }
    self.factories = factories
    self.supervisor = supervisor
    self.state = state
    self.now = now
    self.sleep = sleep
    self.onComponentError = onComponentError
    self.terminate = terminate
  }

  /// Structured children are joined on every exit. The watchdog deliberately
  /// uses a separately cancelled task so it still observes stuck lane teardown
  /// after the parent is cancelled.
  public func run() async throws {
    let workers = Task {
      try await withThrowingTaskGroup(of: Void.self) { group in
        for lane in lanes {
          group.addTask {
            if self.config.role == .projection {
              try await self.runProjection(lane)
            } else {
              try await self.runCoordinator(lane)
            }
          }
        }
        defer { group.cancelAll() }
        try await group.next()
      }
    }
    // This task deliberately does not inherit parent cancellation. Its safety
    // invariant is that it owns no authority/workload resources and is joined
    // below after every workload child has stopped. It must remain alive while
    // those children drain, or it cannot terminate an unresponsive lease owner.
    let watchdog: Task<Void, any Error>? =
      config.role == .coordinator
      ? Task.detached {
        do { try await self.watchdog() } catch {
          if !Task.isCancelled { workers.cancel() }
          throw error
        }
      } : nil
    let workerResult = await withTaskCancellationHandler {
      await workers.result
    } onCancel: {
      workers.cancel()
    }
    watchdog?.cancel()
    if let watchdog, case .failure(let error) = await watchdog.result, !(error is CancellationError)
    {
      throw error
    }
    try workerResult.get()
  }

  private func runHost(_ lane: IndexingWorkerLane, authority: IndexingWorkerLeaseAuthority?)
    async throws
  {
    guard let factory = factories[lane] else {
      throw IndexingWorkerRuntimeError.missingLaneFactory(lane)
    }
    let host = try await factory()
    active[lane] = host
    defer { active[lane] = nil }
    await state.set(.running, for: lane, at: now())
    try await host.run(authority: authority)
  }

  private func runProjection(_ lane: IndexingWorkerLane) async throws {
    var backoff: Duration = .milliseconds(250)
    while !Task.isCancelled {
      await state.set(.starting, for: lane, at: now())
      do {
        try await runHost(lane, authority: nil)
        try Task.checkCancellation()
        throw IndexingWorkerRuntimeError.componentExited(lane)
      } catch {
        if Task.isCancelled || error is CancellationError { throw CancellationError() }
        await state.set(.restarting, for: lane, at: now())
        await onComponentError(lane, error)
        try await sleep(backoff)
        backoff = min(backoff * 2, .seconds(10))
      }
    }
    throw CancellationError()
  }

  private func runCoordinator(_ lane: IndexingWorkerLane) async throws {
    guard let supervisor else { throw IndexingWorkerRuntimeError.coordinatorRequiresAuthority }
    await state.set(.starting, for: lane, at: now())
    try await supervisor.run(
      configuration: IndexingWorkerLeaseConfiguration(lane: lane, worker: config),
      onEvent: { event in await self.state.record(event, for: lane, at: self.now()) },
      operation: { authority in try await self.runHost(lane, authority: authority) }
    )
    try Task.checkCancellation()
    throw IndexingWorkerRuntimeError.componentExited(lane)
  }

  private func watchdog() async throws {
    while !Task.isCancelled {
      if let lane = await state.unresponsiveLane(at: now(), grace: 30) {
        await terminate(lane)
        throw IndexingWorkerRuntimeError.unresponsiveComponent(lane)
      }
      try await sleep(.seconds(5))
    }
    throw CancellationError()
  }

  public func startup() async throws { try await probe(ready: false) }
  public func ready() async throws { try await probe(ready: true) }

  private func probe(ready: Bool) async throws {
    if config.role == .coordinator {
      guard
        await state.hasRecentControlEvidence(
          lanes: lanes, at: now(), maximumAge: config.controlEvidenceMaximumAge
        )
      else { throw IndexingWorkerRuntimeError.controlEvidenceUnavailable }
    }
    let phases = await state.snapshot()
    for lane in lanes {
      if config.role == .coordinator && phases[lane] == .standby { continue }
      guard phases[lane] == .running else {
        throw IndexingWorkerRuntimeError.laneNotRunning(lane, phases[lane])
      }
      guard let host = active[lane] else { throw IndexingWorkerRuntimeError.laneUnavailable(lane) }
      if ready { try await host.ready() } else { try await host.startup() }
    }
  }
}
