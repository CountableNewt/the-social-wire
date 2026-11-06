import Foundation

/// Resolves subscribed feeds that are not yet stored as podcast shows outside the request path.
/// Skyreader subscriptions mix podcasts with ordinary blog feeds, which never resolve, so a
/// failed key backs off instead of being fetched again on every state hydration.
actor PodcastFeedResolutionQueue {
  private let resolve: @Sendable (String) async throws -> Void
  private let now: @Sendable () -> Date
  private let maxConcurrent: Int
  private let retryAfter: TimeInterval
  private let maxPending: Int
  private var stopped = false
  private var pending: [String] = []
  private var queued: Set<String> = []
  private var active: [String: Task<Void, Never>] = [:]
  private var retryAt: [String: Date] = [:]

  init(maxConcurrent: Int = 4, retryAfter: TimeInterval = 6 * 60 * 60, maxPending: Int = 2_000,
    now: @escaping @Sendable () -> Date = Date.init,
    resolve: @escaping @Sendable (String) async throws -> Void) {
    self.maxConcurrent = maxConcurrent
    self.retryAfter = retryAfter
    self.maxPending = maxPending
    self.now = now
    self.resolve = resolve
  }

  func schedule(_ keys: [String]) {
    guard !stopped else { return }
    let current = now()
    for key in keys where !queued.contains(key) && active[key] == nil && pending.count < maxPending {
      if let retry = retryAt[key], retry > current { continue }
      retryAt.removeValue(forKey: key)
      queued.insert(key)
      pending.append(key)
    }
    drain()
  }

  func shutdown() async {
    stopped = true
    pending.removeAll()
    queued.removeAll()
    let current = Array(active.values)
    for task in current { task.cancel() }
    for task in current { await task.value }
  }

  var isIdle: Bool { pending.isEmpty && active.isEmpty }

  private func drain() {
    while !stopped, active.count < maxConcurrent, !pending.isEmpty {
      let key = pending.removeFirst()
      queued.remove(key)
      active[key] = Task {
        let succeeded: Bool
        do { try await self.resolve(key); succeeded = true } catch { succeeded = false }
        self.complete(key, succeeded: succeeded)
      }
    }
  }

  private func complete(_ key: String, succeeded: Bool) {
    active.removeValue(forKey: key)
    if !succeeded, !stopped { retryAt[key] = now().addingTimeInterval(retryAfter) }
    drain()
  }
}
