import Foundation
import Logging
import Testing
@testable import ThinAppViewCore

@Suite("RSS completion timestamps")
struct SQLiteRSSCompletionTimestampTests {
  @Test("preserves completion evidence within the refresh's starting second")
  func subsecondCompletion() async throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent("rss-completion-\(UUID()).sqlite").path
    defer {
      for suffix in ["", "-wal", "-shm"] { try? FileManager.default.removeItem(atPath: path + suffix) }
    }
    let store = try SQLiteThinAppViewStore(path: path, logger: Logger(label: "rss.completion.test"))
    let started = Date(timeIntervalSince1970: 1_800_000_000.25)
    let completed = started.addingTimeInterval(0.125)
    let feed = "https://publisher.example/rss"
    try await store.storeRssFeedMetadata(RssFeedFetchMetadata(normalizedFeedUrl: feed,
      etag: nil, lastModified: nil, lastPollAt: completed, backoffUntil: nil, consecutiveErrorCount: 0))
    let restored = try #require(try await store.fetchRssFeedMetadata(normalizedFeedUrl: feed)?.lastPollAt)
    #expect(restored >= started)
    #expect(abs(restored.timeIntervalSince(completed)) < 0.000001)
  }
}
