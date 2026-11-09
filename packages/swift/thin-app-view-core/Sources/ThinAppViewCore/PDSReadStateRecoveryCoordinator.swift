import Crypto
import Foundation
import Logging
import OperationsCore

/// Unavailable projections fail retryably while a complete rebuild runs.
/// Ready projections refresh from PDS periodically while serving verified state.
/// Durable viewer and global slot leases bound work across all AppView replicas.
public actor PDSReadStateRecoveryCoordinator {
  private let store: any PDSReadStateLifecycleStoring
  private let operations: any OperationsStore
  private let projectionCache: (any AppViewProjectionCacheStore)?
  private let rebuild: @Sendable (String) async throws -> Bool
  private let logger: Logger
  private let deadlineSeconds: TimeInterval
  private let retrySeconds: TimeInterval
  private let refreshSeconds: TimeInterval
  private let close: @Sendable () async -> Void
  private var shutdownTask: Task<Void, Never>?
  private var inFlight: [String: Task<Void, Never>] = [:]
  private var retryAfter: [String: Date] = [:]
  private var refreshAfter: [String: Date] = [:]

  public init(store: any PDSReadStateLifecycleStoring, operations: any OperationsStore,
              projectionCache: (any AppViewProjectionCacheStore)?, plcURL: String, logger: Logger) {
    let fetcher = LivePDSReadStateRecordFetcher(plcURL: plcURL)
    let projector = PDSReadStateProjector(store: store, fetchRecord: fetcher.fetch)
    self.init(store: store, operations: operations, projectionCache: projectionCache,
      logger: logger, close: { await fetcher.shutdown() },
      rebuild: { try await projector.reconcile(viewerDid: $0) })
  }

  init(store: any PDSReadStateLifecycleStoring, operations: any OperationsStore,
       projectionCache: (any AppViewProjectionCacheStore)? = nil, logger: Logger,
       deadlineSeconds: TimeInterval = 90, retrySeconds: TimeInterval = 30,
       refreshSeconds: TimeInterval = 60,
       close: @escaping @Sendable () async -> Void = {},
       rebuild: @escaping @Sendable (String) async throws -> Bool) {
    self.store = store
    self.operations = operations
    self.projectionCache = projectionCache
    self.logger = logger
    self.deadlineSeconds = max(0.01, min(deadlineSeconds, 90))
    self.retrySeconds = max(0, retrySeconds)
    self.refreshSeconds = max(0, refreshSeconds)
    self.close = close
    self.rebuild = rebuild
  }

  public func requireReady(viewerDid: String) async throws {
    guard shutdownTask == nil else { throw CancellationError() }
    // The coalesced touch precedes cache access; an idle eviction cannot race
    // past this request and turn cached/streamed results into fabricated unread.
    try await store.touchPDSReadStateAccess(viewerDid: viewerDid, at: Date())
    let status = try await store.pdsReadStateStatus(viewerDid: viewerDid)
    guard shutdownTask == nil else { throw CancellationError() }
    guard status.authority == .pds else { return }
    let now = Date()
    let refreshReady = status.projectionReady
    if inFlight[viewerDid] == nil, inFlight.count < 2,
       (retryAfter[viewerDid] ?? .distantPast) <= now,
       (!refreshReady || (refreshAfter[viewerDid] ?? .distantPast) <= now) {
      retryAfter = retryAfter.filter { $0.value > now }
      refreshAfter = refreshAfter.filter { $0.value > now }
      inFlight[viewerDid] = Task { await performRebuild(viewerDid: viewerDid, refreshReady: refreshReady) }
    }
    // Without global ingestion, ready viewers still need periodic authoritative
    // PDS checks. Keep their last verified projection usable during refresh/outage.
    if !status.projectionReady { throw PDSReadStateStorageError.projectionNotReady }
  }

  /// Join all PDS work before the serving database and HTTP clients shut down.
  public func shutdown() async {
    if let shutdownTask { await shutdownTask.value; return }
    let tasks = Array(inFlight.values)
    let close = self.close
    let task = Task {
      for task in tasks { task.cancel() }
      for task in tasks { await task.value }
      await close()
    }
    shutdownTask = task
    await task.value
  }

  // Exposed internally to deterministic tests and controlled shutdown callers.
  func waitForCurrentRebuild(viewerDid: String) async { await inFlight[viewerDid]?.value }

  private func performRebuild(viewerDid: String, refreshReady: Bool) async {
    do {
      try await Self.withDeadline(seconds: deadlineSeconds) { [self] in
        try await Self.rebuildWithLeases(viewerDid: viewerDid, store: store,
          operations: operations, projectionCache: projectionCache, refreshReady: refreshReady, rebuild: rebuild)
      }
      if refreshAfter.count >= 1_000, let oldest = refreshAfter.min(by: { $0.value < $1.value })?.key {
        refreshAfter.removeValue(forKey: oldest)
      }
      refreshAfter[viewerDid] = Date().addingTimeInterval(refreshSeconds)
    } catch {
      if retryAfter.count >= 1_000, let oldest = retryAfter.min(by: { $0.value < $1.value })?.key {
        retryAfter.removeValue(forKey: oldest)
      }
      retryAfter[viewerDid] = Date().addingTimeInterval(retrySeconds)
      logger.warning("PDS read-state reconciliation deferred")
    }
    inFlight.removeValue(forKey: viewerDid)
  }

  private static func rebuildWithLeases(
    viewerDid: String, store: any PDSReadStateLifecycleStoring, operations: any OperationsStore,
    projectionCache: (any AppViewProjectionCacheStore)?, refreshReady: Bool, rebuild: @escaping @Sendable (String) async throws -> Bool
  ) async throws {
    let now = Date()
    let owner = UUID().uuidString
    let digest = SHA256.hash(data: Data(viewerDid.utf8)).map { String(format: "%02x", $0) }.joined()
    guard let viewerLease = try await operations.acquireIngestionLeaderLease(
      name: "pds-read-state-rebuild:" + digest, sourceGeneration: "pds-read-state-rebuild-v1",
      ownerID: owner, leaseUntil: now.addingTimeInterval(120), at: now) else {
      throw PDSReadStateStorageError.projectionNotReady
    }
    var slot: IngestionLeaderLease?
    do {
      for number in 0..<2 where slot == nil {
        slot = try await operations.acquireIngestionLeaderLease(name: "pds-read-state-rebuild-slot-\(number)",
          sourceGeneration: "pds-read-state-rebuild-v1", ownerID: owner,
          leaseUntil: now.addingTimeInterval(120), at: now)
      }
      guard let slot else { throw PDSReadStateStorageError.projectionNotReady }
      // Do not hold the slot row lock during network work: another contender
      // must be able to observe slot 0 as busy and immediately try slot 1.
      // Its 120-second lifetime exceeds the entire 90-second rebuild deadline.
      try await operations.withIngestionLeaderLeaseFence(name: slot.name, ownerID: owner,
        fencingToken: slot.fencingToken, at: Date()) {}
      try await operations.withIngestionLeaderLeaseFence(name: viewerLease.name, ownerID: owner,
        fencingToken: viewerLease.fencingToken, at: Date()) {
          if !refreshReady, try await store.pdsReadStateStatus(viewerDid: viewerDid).projectionReady { return }
          // Evicted projections remain unavailable until verified publication;
          // ready projections can continue serving their last verified state.
          try await projectionCache?.invalidateSidebarProjection(viewerDid: viewerDid)
          try await projectionCache?.invalidateUnreadCounts(viewerDid: viewerDid, publicationId: nil)
          try await projectionCache?.invalidateFirstPage(viewerDid: viewerDid, publicationId: nil)
          guard try await rebuild(viewerDid) else { throw PDSReadStateStorageError.projectionNotReady }
          try Task.checkCancellation()
          // Ready refreshes can serve requests while network verification runs;
          // invalidate again after atomic publication to evict those old snapshots.
          try await projectionCache?.invalidateSidebarProjection(viewerDid: viewerDid)
          try await projectionCache?.invalidateUnreadCounts(viewerDid: viewerDid, publicationId: nil)
          try await projectionCache?.invalidateFirstPage(viewerDid: viewerDid, publicationId: nil)
      }
    } catch {
      await releaseLeases([viewerLease] + (slot.map { [$0] } ?? []), operations: operations)
      throw error
    }
    await releaseLeases([viewerLease] + (slot.map { [$0] } ?? []), operations: operations)
  }

  private static func releaseLeases(_ leases: [IngestionLeaderLease], operations: any OperationsStore) async {
    // Cancellation must not cancel the cleanup queries themselves.
    await Task {
      for lease in leases {
        try? await operations.releaseIngestionLeaderLease(name: lease.name,
          ownerID: lease.ownerID, fencingToken: lease.fencingToken, at: Date())
      }
    }.value
  }

  private static func withDeadline(seconds: TimeInterval,
    operation: @escaping @Sendable () async throws -> Void) async throws {
    try await withThrowingTaskGroup(of: Void.self) { group in
      group.addTask { try await operation() }
      group.addTask {
        try await Task.sleep(for: .seconds(seconds))
        throw PDSReadStateStorageError.projectionNotReady
      }
      defer { group.cancelAll() }
      _ = try await group.next()
    }
  }
}
