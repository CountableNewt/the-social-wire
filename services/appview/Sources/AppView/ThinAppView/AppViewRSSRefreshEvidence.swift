import Foundation
import Hummingbird
import ThinAppViewCore

/// Persisted polling evidence is required because background ingestion suppresses
/// transport and metadata errors. Missing/stale evidence cannot imply success.
enum AppViewRSSRefreshEvidence {
  /// Only a held lease can join another poll. A freed lease can be acquired
  /// on the next attempt if its prior owner left no completion evidence.
  static func refresh(
    feedUrl: String,
    started: Date,
    ingest: @Sendable () async throws -> RssFeedPollResult,
    metadata: @Sendable () async throws -> RssFeedFetchMetadata?,
    maxWaits: Int = 100,
    wait: @Sendable () async throws -> Void = { try await Task.sleep(for: .milliseconds(250)) }
  ) async throws -> Int {
    for attempt in 0...maxWaits {
      try Task.checkCancellation()
      let result = try await ingest()
      try Task.checkCancellation()
      switch result {
      case .completed(let indexed, let evidence):
        try checkCompletion(evidence, feedUrl: feedUrl, started: started)
        return indexed
      case .backedOff(let evidence):
        try checkBackoff(evidence, feedUrl: feedUrl, at: started)
        throw HTTPError(.serviceUnavailable, message: "RSS refresh completion unavailable")
      case .unfetchable:
        throw AppViewRSSRefreshFailure(feedUrl: feedUrl, category: "rss_fetch")
      case .leaseBusy:
        let current = try await metadata()
        try Task.checkCancellation()
        if let completed = current?.lastPollAt, completed >= started {
          try checkCompletion(current, feedUrl: feedUrl, started: started)
          return 0
        }
      }
      guard attempt < maxWaits else { break }
      try await wait()
    }
    throw AppViewRSSRefreshJoinTimeout()
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
