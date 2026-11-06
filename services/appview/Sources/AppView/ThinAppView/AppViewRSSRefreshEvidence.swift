import Foundation
import Hummingbird
import ThinAppViewCore

/// Persisted polling evidence is required because background ingestion suppresses
/// transport and metadata errors. Missing/stale evidence cannot imply success.
enum AppViewRSSRefreshEvidence {
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
