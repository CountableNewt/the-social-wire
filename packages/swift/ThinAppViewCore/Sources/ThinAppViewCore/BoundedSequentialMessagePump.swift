import Foundation

/// A bounded FIFO for callback-based transports. Saturation is reported synchronously so callers reconnect.
final class BoundedSequentialMessagePump: @unchecked Sendable {
  private let lock = NSLock()
  private let capacity: Int
  private let handleMessage: @Sendable (String) async throws -> Void
  private let onFailure: @Sendable (Error) -> Void
  private var tail: Task<Void, Never>?
  private var pending = 0
  private var failed = false

  init(
    capacity: Int,
    handleMessage: @Sendable @escaping (String) async throws -> Void,
    onFailure: @Sendable @escaping (Error) -> Void
  ) {
    self.capacity = capacity
    self.handleMessage = handleMessage
    self.onFailure = onFailure
  }

  func enqueue(_ message: String) -> Bool {
    lock.lock()
    guard pending < capacity, !failed else {
      lock.unlock()
      return false
    }
    pending += 1
    let previous = tail
    tail = Task { [weak self] in
      _ = await previous?.result
      guard let self else { return }
      guard !isFailed else {
        completedOne()
        return
      }
      do {
        try await handleMessage(message)
      } catch {
        markFailed()
        onFailure(error)
      }
      completedOne()
    }
    lock.unlock()
    return true
  }

  private func completedOne() {
    lock.lock()
    pending = max(0, pending - 1)
    lock.unlock()
  }

  private var isFailed: Bool {
    lock.lock()
    defer { lock.unlock() }
    return failed
  }

  private func markFailed() {
    lock.lock()
    failed = true
    lock.unlock()
  }
}
