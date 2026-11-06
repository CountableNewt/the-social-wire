import Foundation

/// Owns service-scoped background work until its clients can safely shut down.
/// Register during router construction, before shutdown starts. The lock only
/// protects synchronous bookkeeping; callbacks run outside it and are joined.
final class AppViewBackgroundTasks: @unchecked Sendable {
  private let lock = NSLock()
  private var callbacks: [@Sendable () async -> Void] = []
  private var shutdownTask: Task<Void, Never>?

  @discardableResult
  func register(_ callback: @escaping @Sendable () async -> Void) -> Bool {
    lock.withLock {
      guard shutdownTask == nil else { return false }
      callbacks.append(callback)
      return true
    }
  }

  func shutdown() async {
    let task = lock.withLock {
      if let shutdownTask { return shutdownTask }
      let work = callbacks
      callbacks.removeAll()
      let task = Task { for callback in work { await callback() } }
      shutdownTask = task
      return task
    }
    await task.value
  }
}
