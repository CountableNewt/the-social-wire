import Foundation
import NIOConcurrencyHelpers

/// One absolute lifetime spans handler work and its response body. Closing the
/// stream interrupts blocked socket I/O; cancelling work interrupts DB/S3 waits.
final class SocialDraftDeadline: Sendable {
  private let state: NIOLockedValueBox<(expired: Bool, complete: Bool, cancel: (@Sendable () -> Void)?)>
  private let timer: Task<Void, Never>

  init(seconds: Double, closeTransport: @escaping @Sendable () -> Void) {
    let state = NIOLockedValueBox<(expired: Bool, complete: Bool, cancel: (@Sendable () -> Void)?)>((false, false, nil))
    self.state = state
    self.timer = Task {
      do {
        try await Task.sleep(for: .seconds(seconds))
        let result = state.withLockedValue { value -> (Bool, (@Sendable () -> Void)?) in
          guard !value.complete else { return (false, nil) }
          value.expired = true
          return (true, value.cancel)
        }
        if result.0 { result.1?(); closeTransport() }
      } catch { }
    }
  }

  func bind(cancel: @escaping @Sendable () -> Void) {
    let expired = state.withLockedValue { value in value.cancel = cancel; return value.expired }
    if expired { cancel() }
  }
  func complete() {
    timer.cancel()
    state.withLockedValue { $0.complete = true; $0.cancel = nil }
  }
  deinit { timer.cancel() }
}
