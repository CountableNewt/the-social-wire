import Foundation
import Testing
@testable import AppView

struct PodcastFeedResolutionQueueTests {
  private actor Recorder {
    var calls: [String] = []
    var running = 0
    var peak = 0
    func start(_ key: String) { calls.append(key); running += 1; peak = max(peak, running) }
    func finish() { running -= 1 }
  }

  private final class Clock: @unchecked Sendable {
    private let lock = NSLock()
    private var value = Date(timeIntervalSince1970: 0)
    var now: Date { lock.withLock { value } }
    func advance(_ seconds: TimeInterval) { lock.withLock { value = value.addingTimeInterval(seconds) } }
  }

  private struct NotAPodcast: Error {}

  private func waitUntilIdle(_ queue: PodcastFeedResolutionQueue) async throws {
    for _ in 0..<500 {
      if await queue.isIdle { return }
      try await Task.sleep(for: .milliseconds(2))
    }
    Issue.record("Feed resolution did not finish")
  }

  @Test func resolvesEachKeyOnceWithBoundedConcurrency() async throws {
    let recorder = Recorder()
    let queue = PodcastFeedResolutionQueue(maxConcurrent: 2) { key in
      await recorder.start(key)
      try await Task.sleep(for: .milliseconds(5))
      await recorder.finish()
    }
    await queue.schedule(["a", "b", "c", "a", "d"])
    await queue.schedule(["b", "e"])
    try await waitUntilIdle(queue)
    #expect(await recorder.calls.sorted() == ["a", "b", "c", "d", "e"])
    #expect(await recorder.peak <= 2)
  }

  @Test func backsOffFailedFeedsUntilTheRetryWindowPasses() async throws {
    let recorder = Recorder()
    let clock = Clock()
    let queue = PodcastFeedResolutionQueue(retryAfter: 60, now: { clock.now }) { key in
      await recorder.start(key)
      await recorder.finish()
      throw NotAPodcast()
    }
    await queue.schedule(["https://blog.example/feed"])
    try await waitUntilIdle(queue)
    await queue.schedule(["https://blog.example/feed"])
    try await waitUntilIdle(queue)
    #expect(await recorder.calls.count == 1)
    clock.advance(61)
    await queue.schedule(["https://blog.example/feed"])
    try await waitUntilIdle(queue)
    #expect(await recorder.calls.count == 2)
  }

  @Test func ignoresScheduleAfterShutdown() async throws {
    let recorder = Recorder()
    let queue = PodcastFeedResolutionQueue { key in await recorder.start(key); await recorder.finish() }
    await queue.shutdown()
    await queue.schedule(["a"])
    #expect(await queue.isIdle)
    #expect(await recorder.calls.isEmpty)
  }
}
