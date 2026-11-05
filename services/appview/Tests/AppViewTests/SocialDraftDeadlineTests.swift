import NIOConcurrencyHelpers
import Testing
@testable import AppView

@Suite struct SocialDraftDeadlineTests {
  @Test func completedResponseDoesNotCloseKeepaliveTransportLater() async throws {
    let calls = NIOLockedValueBox(0)
    let deadline = SocialDraftDeadline(seconds: 0.01) { calls.withLockedValue { $0 += 1 } }
    deadline.complete()
    try await Task.sleep(for: .milliseconds(30))
    #expect(calls.withLockedValue { $0 } == 0)
    withExtendedLifetime(deadline) {}
  }

  @Test func expiryCancelsWorkAndClosesTransportOnce() async throws {
    let calls = NIOLockedValueBox((closed: 0, cancelled: 0))
    let deadline = SocialDraftDeadline(seconds: 0.01) { calls.withLockedValue { $0.closed += 1 } }
    deadline.bind { calls.withLockedValue { $0.cancelled += 1 } }
    let end = ContinuousClock.now.advanced(by: .seconds(2))
    while calls.withLockedValue({ $0.closed == 0 }) && ContinuousClock.now < end {
      try await Task.sleep(for: .milliseconds(10))
    }
    #expect(calls.withLockedValue { $0.closed == 1 && $0.cancelled == 1 })
    deadline.complete()
  }
}
