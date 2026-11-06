import Foundation
import Logging
import NIOConcurrencyHelpers
import Testing
@testable import AppView

@Suite struct ScopedContentHistoryTests {
  private actor Probe {
    var calls = 0
    var active = 0
    var maximum = 0
    func enumerate(_ author: String, delay: Duration = .milliseconds(30)) async throws -> Int {
      calls += 1; active += 1; maximum = max(maximum, active)
      defer { active -= 1 }
      try await Task.sleep(for: delay)
      return 100
    }
  }

  @Test func coldRequestedAuthorsGetCoalescedBoundedHistoryAndASeparateLongTTL() async throws {
    let probe = Probe(), clock = NIOLockedValueBox(Date())
    let history = ScopedContentHistory(logger: Logger(label: "history.test"), now: { clock.withLockedValue { $0 } }) { try await probe.enumerate($0) }
    let authors = ["did:plc:aaaaaaaaaaaaaaaaaaaaaaaa", "did:plc:bbbbbbbbbbbbbbbbbbbbbbbb", "did:plc:cccccccccccccccccccccccc"]
    for author in authors + authors { await history.schedule(authorDid: author) }
    await history.waitForCurrentWork()
    #expect(await probe.calls == 3)
    #expect(await probe.maximum <= 2)
    for author in authors { await history.schedule(authorDid: author) }
    await history.waitForCurrentWork()
    #expect(await probe.calls == 3)
    clock.withLockedValue { $0 = $0.addingTimeInterval(6 * 3600 + 1) }
    await history.schedule(authorDid: authors[0])
    await history.waitForCurrentWork()
    #expect(await probe.calls == 4)
  }

  @Test func aTimedOutHistoryIsRetryableAndCannotBecomeACompleteSnapshot() async throws {
    let probe = Probe(), clock = NIOLockedValueBox(Date())
    let history = ScopedContentHistory(logger: Logger(label: "history.test"), now: { clock.withLockedValue { $0 } }, deadline: .milliseconds(5)) { try await probe.enumerate($0, delay: .seconds(60)) }
    let author = "did:plc:aaaaaaaaaaaaaaaaaaaaaaaa"
    await history.schedule(authorDid: author)
    await history.waitForCurrentWork()
    #expect(await probe.calls == 1)
    clock.withLockedValue { $0 = $0.addingTimeInterval(61) }
    await history.schedule(authorDid: author)
    await history.waitForCurrentWork()
    #expect(await probe.calls == 2)
  }
}
