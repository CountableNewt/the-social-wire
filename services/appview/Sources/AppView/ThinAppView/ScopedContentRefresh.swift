import Foundation
import Logging
import ThinAppViewCore

/// Coalesced publication refreshes retain PDS enumeration as the authority.
/// Four provider jobs and four fast author jobs retain at most32 waiters per scope.
actor ScopedContentRefresh {
  private let provider: (any PublicContentDiscovering)?
  private let enumeratePDS: @Sendable (String) async throws -> Int
  private let logger: Logger
  private let now: @Sendable () -> Date
  private var stopped = false
  private var freshUntil: [PublicContentDiscoveryScope: Date] = [:]
  private var pdsFreshUntil: [String: Date] = [:]
  private var pdsActive: Set<String> = []
  private var jobs: [PublicContentDiscoveryScope: (id: UUID, task: Task<Void, Never>)] = [:]
  private var waiters: [PublicContentDiscoveryScope: [UUID: CheckedContinuation<Int, any Error>]] = [:]

  init(provider: (any PublicContentDiscovering)?, logger: Logger,
    now: @escaping @Sendable () -> Date = Date.init,
    enumeratePDS: @escaping @Sendable (String) async throws -> Int) {
    self.provider = provider
    self.logger = logger
    self.now = now
    self.enumeratePDS = enumeratePDS
  }

  func refresh(scope: PublicContentDiscoveryScope) async throws -> Int {
    try Task.checkCancellation()
    guard !stopped else { throw CancellationError() }
    guard (freshUntil[scope] ?? .distantPast) <= now() else { return 0 }
    let waiter = UUID()
    let value = try await withTaskCancellationHandler {
      try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Int, any Error>) in
        let sameKind = jobs.keys.filter { $0.publicationKeys.isEmpty == scope.publicationKeys.isEmpty }.count
        guard jobs[scope] != nil || sameKind < 4, (waiters[scope]?.count ?? 0) < 32 else {
          continuation.resume(returning: 0)
          return
        }
        waiters[scope, default: [:]][waiter] = continuation
        if jobs[scope] == nil {
          let id = UUID()
          let task = Task {
            let result: Result<Int, any Error>
            do { result = .success(try await self.perform(scope: scope)) }
            catch { result = .failure(error) }
            self.complete(scope: scope, id: id, result: result)
          }
          jobs[scope] = (id, task)
        }
      }
    } onCancel: {
      Task { await self.cancel(scope: scope, waiter: waiter) }
    }
    try Task.checkCancellation()
    return value
  }

  func shutdown() async {
    stopped = true
    let current = jobs.values.map(\.task)
    for job in current { job.cancel() }
    for job in current { await job.value }
  }

  private func cancel(scope: PublicContentDiscoveryScope, waiter: UUID) {
    waiters[scope]?.removeValue(forKey: waiter)?.resume(throwing: CancellationError())
    if waiters[scope]?.isEmpty == true {
      waiters.removeValue(forKey: scope)
      jobs[scope]?.task.cancel()
    }
  }

  private func complete(scope: PublicContentDiscoveryScope, id: UUID, result: Result<Int, any Error>) {
    guard jobs[scope]?.id == id else { return }
    jobs.removeValue(forKey: scope)
    let waiting = waiters.removeValue(forKey: scope) ?? [:]
    for continuation in waiting.values { continuation.resume(with: result) }
  }

  private func perform(scope: PublicContentDiscoveryScope) async throws -> Int {
    var indexed = 0
    if !scope.publicationKeys.isEmpty, let provider {
      do { indexed += try await provider.discover(scope: scope) }
      catch is CancellationError { throw CancellationError() }
      catch {
        try Task.checkCancellation()
        logger.debug("Public content provider unavailable; using scoped PDS enumeration")
      }
    }
    try Task.checkCancellation()
    // Hints do not promise complete historical or legacy backlink coverage.
    do { indexed += try await refreshAuthor(scope.authorDid) }
    catch is CancellationError { throw CancellationError() }
    catch {
      try Task.checkCancellation()
      remember(scope, seconds: 5)
      throw error
    }
    try Task.checkCancellation()
    remember(scope, seconds: 60)
    return indexed
  }

  private func refreshAuthor(_ authorDid: String) async throws -> Int {
    while pdsActive.contains(authorDid) { try await Task.sleep(for: .milliseconds(10)) }
    try Task.checkCancellation()
    guard (pdsFreshUntil[authorDid] ?? .distantPast) <= now() else { return 0 }
    pdsActive.insert(authorDid)
    defer { pdsActive.remove(authorDid) }
    let count = try await enumeratePDS(authorDid)
    try Task.checkCancellation()
    let at = now()
    pdsFreshUntil = pdsFreshUntil.filter { $0.value > at }
    if pdsFreshUntil.count >= 512 { pdsFreshUntil.removeAll(keepingCapacity: true) }
    pdsFreshUntil[authorDid] = at.addingTimeInterval(60)
    return count
  }

  private func remember(_ scope: PublicContentDiscoveryScope, seconds: TimeInterval) {
    let at = now()
    freshUntil = freshUntil.filter { $0.value > at }
    if freshUntil.count >= 512 { freshUntil.removeAll(keepingCapacity: true) }
    freshUntil[scope] = at.addingTimeInterval(seconds)
  }
}
