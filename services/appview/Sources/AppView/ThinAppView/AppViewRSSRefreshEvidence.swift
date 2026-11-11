import Foundation
import Hummingbird
import ThinAppViewCore

/// Persisted polling evidence is required because background ingestion suppresses
/// transport and metadata errors. Missing/stale evidence cannot imply success.
enum AppViewRSSRefreshEvidence {
  /// A lease held by a background poll is unfinished work, not a failed poll.
  /// Join its persisted result without starting another fetch or accepting stale evidence.
  static func refresh(
    feedUrl: String,
    started: Date,
    ingest: @Sendable () async throws -> Int,
    metadata: @Sendable () async throws -> RssFeedFetchMetadata?,
    maxWaits: Int = 100,
    wait: @Sendable () async throws -> Void = { try await Task.sleep(for: .milliseconds(250)) }
  ) async throws -> Int {
    let indexed = try await ingest()
    for attempt in 0...maxWaits {
      try Task.checkCancellation()
      let current = try await metadata()
      if let completed = current?.lastPollAt, completed >= started {
        try checkCompletion(current, feedUrl: feedUrl, started: started)
        return indexed
      }
      guard attempt < maxWaits else { break }
      try await wait()
    }
    throw HTTPError(.serviceUnavailable, message: "RSS refresh completion unavailable")
  }

  static func checkBackoff(_ metadata: RssFeedFetchMetadata?, feedUrl: String, at: Date) throws {
    if let backoff = metadata?.backoffUntil, backoff > at,
      (metadata?.consecutiveErrorCount ?? 0) > 0 {
      throw AppViewRSSRefreshFailure(feedUrl: feedUrl)
    }
  }

  static func checkCompletion(_ metadata: RssFeedFetchMetadata?, feedUrl: String, started: Date) throws {
    guard let metadata, let polledAt = metadata.lastPollAt, polledAt >= started else {
      throw HTTPError(.serviceUnavailable, message: "RSS refresh completion unavailable")
    }
    if metadata.consecutiveErrorCount > 0, let backoff = metadata.backoffUntil, backoff > started {
      throw AppViewRSSRefreshFailure(feedUrl: feedUrl, category: "rss_fetch")
    }
    guard metadata.consecutiveErrorCount == 0, metadata.backoffUntil == nil else {
      throw HTTPError(.serviceUnavailable, message: "RSS refresh completion unavailable")
    }
  }
}
