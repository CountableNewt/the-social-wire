import Foundation
import Logging
import ThinAppViewCore

/// Lazy history only for authors requested by a viewer. No global discovery or
/// stream consumer. A capped/failed PDS report is retried, never called complete.
actor ScopedContentHistory {
  private let enumeratePDS: @Sendable (String) async throws -> Int
  private let logger: Logger
  private let now: @Sendable () -> Date
  private let deadline: Duration
  private var stopped = false
  private var tasks: [String: Task<Void, Never>] = [:]
  private var pending: [String] = []
  private var retryAfter: [String: Date] = [:]

  init(logger: Logger, now: @escaping @Sendable () -> Date = Date.init, deadline: Duration = .seconds(60),
    enumeratePDS: @escaping @Sendable (String) async throws -> Int) {
    self.logger = logger
    self.now = now
    self.deadline = deadline
    self.enumeratePDS = enumeratePDS
  }

  func schedule(authorDid: String) {
    guard !stopped, ThinAppViewEnrollBackfill.isBackfillEligibleAuthorDid(authorDid),
      tasks[authorDid] == nil, !pending.contains(authorDid), pending.count < 32,
      (retryAfter[authorDid] ?? .distantPast) <= now() else { return }
    pending.append(authorDid)
    startNext()
  }

  private func startNext() {
    while !stopped, tasks.count < 2, !pending.isEmpty {
      let author = pending.removeFirst()
      tasks[author] = Task { await self.run(author: author) }
    }
  }

  private func run(author: String) async {
    let succeeded: Bool
    do {
      try await withThrowingTaskGroup(of: Int.self) { group in
        defer { group.cancelAll() }
        group.addTask { try await self.enumeratePDS(author) }
        group.addTask { try await Task.sleep(for: self.deadline); throw PublicContentDiscoveryError.unavailable }
        _ = try await group.next()
      }
      succeeded = true
    } catch {
      succeeded = false
      logger.debug("Scoped PDS history incomplete; retaining indexed rows and retrying later")
    }
    guard !stopped else { tasks.removeValue(forKey: author); return }
    let at = now()
    retryAfter = retryAfter.filter { $0.value > at }
    if retryAfter.count >= 512 { retryAfter.removeAll(keepingCapacity: true) }
    retryAfter[author] = at.addingTimeInterval(succeeded ? 6 * 3600 : 60)
    tasks.removeValue(forKey: author)
    startNext()
  }

  func shutdown() async {
    stopped = true
    pending.removeAll()
    let current = Array(tasks.values)
    for task in current { task.cancel() }
    for task in current { await task.value }
  }

  func waitForCurrentWork() async {
    while !tasks.isEmpty {
      let current = Array(tasks.values)
      for task in current { await task.value }
    }
  }
}
