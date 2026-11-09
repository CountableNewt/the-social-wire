import AsyncHTTPClient
import Foundation

public protocol TapRepositoryRestorer: Sendable {
  func restoreCurrentRepository(repoDid: String) async throws -> PDSReconciliationReport
  func restoreCurrentRepository(repoDid: String, recovery: PDSRepositoryRecoveryContext)
    async throws
    -> PDSReconciliationReport
}

extension TapRepositoryRestorer {
  public func restoreCurrentRepository(repoDid: String, recovery: PDSRepositoryRecoveryContext)
    async throws -> PDSReconciliationReport
  {
    try await restoreCurrentRepository(repoDid: repoDid)
  }
}

public struct TapPDSRepositoryRestorer: TapRepositoryRestorer, Sendable {
  private let store: any ThinAppViewStore
  private let backfill: ThinAppViewEnrollBackfill
  private let projectionCache: (any AppViewProjectionCacheStore)?
  private let maxConcurrency: Int
  private let rateLimitPerSecond: Int
  private let timeoutSeconds: TimeInterval
  var snapshotReader: RepositorySnapshotReader?

  public init(
    store: any ThinAppViewStore,
    backfill: ThinAppViewEnrollBackfill,
    projectionCache: (any AppViewProjectionCacheStore)? = nil,
    maxConcurrency: Int,
    rateLimitPerSecond: Int,
    timeoutSeconds: TimeInterval = 120,
    httpClient: HTTPClient? = nil,
    plcBase: String? = nil
  ) {
    self.store = store
    self.backfill = backfill
    self.projectionCache = projectionCache
    self.maxConcurrency = max(1, maxConcurrency)
    self.rateLimitPerSecond = max(1, rateLimitPerSecond)
    self.timeoutSeconds = max(0.01, timeoutSeconds)
    if let httpClient, let plcBase {
      snapshotReader = RepositorySnapshotReader(httpClient: httpClient, plcBase: plcBase)
    }
  }

  public func restoreCurrentRepository(repoDid: String) async throws
    -> PDSReconciliationReport
  {
    try await withThrowingTaskGroup(of: PDSReconciliationReport.self) { group in
      group.addTask { try await performRestore(repoDid: repoDid) }
      group.addTask {
        try await Task.sleep(for: .seconds(timeoutSeconds))
        throw TapRepositoryRestorationError.timedOut
      }
      defer { group.cancelAll() }
      guard let first = try await group.next() else {
        throw TapRepositoryRestorationError.unavailable
      }
      return first
    }
  }

  public func restoreCurrentRepository(
    repoDid: String, recovery context: PDSRepositoryRecoveryContext
  )
    async throws -> PDSReconciliationReport
  {
    guard repoDid == context.repoDid, let recoveryStore = store as? any PDSRepositoryRecoveryStore
    else {
      throw PDSRepositoryRecoveryError.unavailable
    }
    let state = try await recoveryStore.loadRepositoryRecovery(context)
    let recovery = PDSRepositoryRecovery(context: context, store: recoveryStore, state: state)
    return try await withThrowingTaskGroup(of: PDSReconciliationReport.self) { group in
      group.addTask {
        if state.completed { return try await completedReport(repoDid: repoDid, state: state) }
        if let mode = state.snapshotMode {
          guard mode == "signed_blocks_v1" else {
            throw PDSRepositoryRecoveryError.signedSnapshotUnavailable
          }
          return try await restoreSignedSnapshot(
            context: context, state: state, recoveryStore: recoveryStore)
        }
        let defaults = backfill.diagnosticOptions(
          maxConcurrency: maxConcurrency,
          rateLimitPerSecond: rateLimitPerSecond)
        let report = try await backfill.reconcile(
          authorDids: [repoDid],
          options: .init(
            maxConcurrency: 1, rateLimitPerSecond: rateLimitPerSecond,
            recordCapPerAuthor: min(200, defaults.recordCapPerAuthor)), recovery: recovery)
        guard report.complete else {
          if Self.requiresSignedSnapshot(report) {
            let latest = try await recoveryStore.loadRepositoryRecovery(context)
            return try await restoreSignedSnapshot(
              context: context, state: latest, recoveryStore: recoveryStore)
          }
          throw TapRepositoryRestorationError.incomplete(report)
        }
        if !state.completed { try await recovery.finish() }
        try await store.markUnreadCountersDirtyForAuthor(authorDid: repoDid)
        try await projectionCache?.invalidateAllProjectionCaches()
        return report
      }
      group.addTask {
        try await Task.sleep(for: .seconds(timeoutSeconds))
        throw TapRepositoryRestorationError.timedOut
      }
      defer { group.cancelAll() }
      guard let result = try await group.next() else {
        throw TapRepositoryRestorationError.unavailable
      }
      return result
    }
  }

  private static func requiresSignedSnapshot(_ report: PDSReconciliationReport) -> Bool {
    report.authors.flatMap(\.collections).contains { result in
      result.issues.contains { issue in
        (result.collection == "site.standard.document" && issue.kind == .unexpectedStatus
          && issue.detail == "http_400_document_invalid_request")
          || (issue.kind == .malformedResponse && issue.detail == "cursor_cycle")
      }
    }
  }

  private func restoreSignedSnapshot(
    context: PDSRepositoryRecoveryContext, state previous: PDSRepositoryRecoveryState,
    recoveryStore: any PDSRepositoryRecoveryStore
  ) async throws -> PDSReconciliationReport {
    guard let snapshotReader else { throw PDSRepositoryRecoveryError.signedSnapshotUnavailable }
    let startedAt = Date()
    let collections: Set<String> = ["site.standard.document", "site.standard.entry"]
    let snapshot = try await snapshotReader.read(did: context.repoDid, collections: collections)
    var state = previous
    if state.snapshotMode != "signed_blocks_v1" || state.commitCid != snapshot.commitCID
      || state.revision != snapshot.revision || state.pdsBase != snapshot.pdsBase
    {
      state = PDSRepositoryRecoveryState()
      state.startedAt = startedAt
      state.snapshotMode = "signed_blocks_v1"
      state.commitCid = snapshot.commitCID
      state.revision = snapshot.revision
      state.pdsBase = snapshot.pdsBase
      state.recordOffset = 0
      state = try await recoveryStore.saveRepositoryRecovery(
        context, state: state, observedURIs: [], finish: false)
    }
    let offset = state.recordOffset ?? 0
    guard offset >= 0, offset <= snapshot.records.count else {
      throw RepositorySnapshotError.invalid("invalid recovery offset")
    }
    let end = min(offset + 200, snapshot.records.count)
    var uris: [String] = []
    for record in snapshot.records[offset..<end] {
      try await backfill.applyVerifiedSnapshotRecord(
        record, did: context.repoDid, pdsBase: snapshot.pdsBase)
      let collection = String(
        record.uri.dropFirst("at://\(context.repoDid)/".count).split(separator: "/")[0])
      var progress = state.collections[collection] ?? .init()
      progress.observedCount += 1
      progress.indexedCount += 1
      state.collections[collection] = progress
      uris.append(record.uri)
    }
    state.recordOffset = end
    if end == snapshot.records.count {
      for collection in collections {
        var progress = state.collections[collection] ?? .init()
        progress.complete = true
        state.collections[collection] = progress
      }
    }
    state = try await recoveryStore.saveRepositoryRecovery(
      context, state: state, observedURIs: uris, finish: false)
    if end < snapshot.records.count {
      try await recoveryStore.yieldRepositoryRecovery(context)
      throw PDSRepositoryRecoveryError.yielded
    }
    state = try await recoveryStore.saveRepositoryRecovery(
      context, state: state, observedURIs: [], finish: true)
    guard state.completed else {
      try await recoveryStore.yieldRepositoryRecovery(context)
      throw PDSRepositoryRecoveryError.yielded
    }
    return try await completedReport(repoDid: context.repoDid, state: state)
  }

  private func completedReport(repoDid: String, state: PDSRepositoryRecoveryState) async throws
    -> PDSReconciliationReport
  {
    try await store.markUnreadCountersDirtyForAuthor(authorDid: repoDid)
    try await projectionCache?.invalidateAllProjectionCaches()
    return PDSReconciliationReport(
      authorScope: .init(requestedAuthorDids: [repoDid], acceptedAuthorDids: [repoDid], issues: []),
      limits: .init(
        maximumAuthors: 1, recordCapPerAuthor: 200, maxConcurrency: 1,
        rateLimitPerSecond: rateLimitPerSecond, maxRateLimitRetries: 3),
      authors: [
        .init(
          authorDid: repoDid, pdsBase: state.pdsBase,
          collections: state.collections.keys.sorted().map { collection in
            let progress = state.collections[collection]!
            return .init(
              collection: collection, observedCount: progress.observedCount,
              indexedCount: progress.indexedCount, truncated: !progress.complete, issues: [])
          }, issues: [])
      ], unsupportedCollections: [], historicalDeletesProvable: false)
  }

  private func performRestore(repoDid: String) async throws -> PDSReconciliationReport {
    let snapshotStartedAt = Date()
    let observedURIs = ObservedRepositoryURIs()
    let report = try await backfill.reconcile(
      authorDids: [repoDid],
      options: backfill.diagnosticOptions(
        maxConcurrency: maxConcurrency,
        rateLimitPerSecond: rateLimitPerSecond
      ),
      onProgress: { progress in
        await observedURIs.insert(progress.lastRecordUri)
      }
    )
    guard report.complete else {
      throw TapRepositoryRestorationError.incomplete(report)
    }

    // Never clear existing rows before the PDS has supplied a complete snapshot: a large response,
    // invalid historical record, or transient PDS failure must leave the last good projection intact.
    // The indexed-at cutoff also preserves later commits applied concurrently by another worker.
    try Task.checkCancellation()
    let currentURIs = await observedURIs.values.sorted()
    try Task.checkCancellation()
    _ = try await store.deleteContentItems(
      authorDid: repoDid,
      excludingURIs: currentURIs,
      indexedAtOrBefore: snapshotStartedAt
    )
    try Task.checkCancellation()
    try await store.markUnreadCountersDirtyForAuthor(authorDid: repoDid)
    try Task.checkCancellation()
    try await projectionCache?.invalidateAllProjectionCaches()
    return report
  }
}

private actor ObservedRepositoryURIs {
  private(set) var values: Set<String> = []

  func insert(_ uri: String) {
    values.insert(uri)
  }
}

public enum TapRepositoryRestorationError: Error, Sendable {
  case unavailable
  case incomplete(PDSReconciliationReport)
  case timedOut
}
