import Foundation
import Hummingbird
import Logging
import Testing
import ThinAppViewCore
import GatewayCore
@testable import AppView

@Suite("Explicit RSS refresh results")
struct AppViewEnrollmentErrorTests {
  private var auth: AuthContext {
    AuthContext(did: "did:plc:viewer", authorizationForwardingValue: "DPoP fixture", dpopProof: "fixture")
  }

  @Test("attempts healthy requested subscriptions after failures without enrolling other subscriptions")
  func partialFeeds() async throws {
    let first = "https://publisher.example/first?token=FIRST"
    let second = "https://publisher.example/second?token=SECOND"
    let healthy = "https://publisher.example/healthy"
    let unrequested = "https://publisher.example/unrequested"
    let unsubscribed = "https://publisher.example/unsubscribed"
    let recorder = SkyreaderWarmingRecorder()
    let service = ThinAppViewSkyreaderIngestionService(
      viewerFeedUrls: { _ in [first, healthy, second, unrequested, first] },
      ingestFeed: { _ in Issue.record("explicit refresh used background ingestion"); return 0 },
      refreshFeed: { url in
        await recorder.recordFeed(url)
        if url != healthy { throw AppViewRSSRefreshFailure(feedUrl: url) }
        return 4
      }, logger: Logger(label: "rss.partial.test"))
    do {
      _ = try await service.refreshViewerSubscriptions(auth: auth, feedUrls: [first, healthy, second, unsubscribed, first])
      Issue.record("expected partial source failure")
    } catch let partial as AppViewEnrollmentPartialFailure {
      #expect(partial.indexed == 4)
      #expect(await recorder.feeds == [first, healthy, second])
      let error = AppViewEnrollmentError(partial)
      let ids = [first, second].map { RssFeedIdentity.rssPublicationId(from: $0) }.sorted()
      #expect(error.unavailablePublicationIds == ids)
      #expect(error.sourceFailures.count == 1)
      #expect(error.sourceFailures.first?.count == 2)
      #expect(error.status == .serviceUnavailable)
      let data = try JSONEncoder().encode(error)
      let json = try #require(JSONSerialization.jsonObject(with: data) as? [String: Any])
      #expect(json["articlesUpdated"] as? Bool == true)
      #expect(json["unavailablePublicationIds"] as? [String] == ids)
      let diagnostics = String(decoding: try JSONEncoder().encode(error.sourceFailures), as: UTF8.self)
      for secret in ["FIRST", "SECOND", "token=", "first?", "second?", ids[0], ids[1]] {
        #expect(!diagnostics.contains(secret))
        #expect(!String(describing: partial).contains(secret))
      }
    }
  }

  @Test("repository and storage failures never become partial source success")
  func infrastructureFailure() async throws {
    let service = ThinAppViewSkyreaderIngestionService(
      viewerFeedUrls: { _ in ["https://publisher.example/first", "https://publisher.example/store"] },
      ingestFeed: { _ in 0 },
      refreshFeed: { url in
        if url.hasSuffix("first") { throw AppViewRSSRefreshFailure(feedUrl: url) }
        throw HTTPError(.serviceUnavailable, message: "storage unavailable")
      }, logger: Logger(label: "rss.storage.test"))
    await #expect(throws: HTTPError.self) {
      try await service.refreshViewerSubscriptions(auth: auth,
        feedUrls: ["https://publisher.example/first", "https://publisher.example/store"])
    }
  }

  @Test("canonical subscription identities are not normalized a second time")
  func canonicalFeedIdentity() async throws {
    let feed = "https://publisher.example/feed/"
    let recorder = SkyreaderWarmingRecorder()
    let service = ThinAppViewSkyreaderIngestionService(
      viewerFeedUrls: { _ in [feed] }, ingestFeed: { _ in 0 },
      refreshFeed: { url in
        await recorder.recordFeed(url)
        throw AppViewRSSRefreshFailure(feedUrl: url)
      }, logger: Logger(label: "rss.identity.test"))
    do {
      _ = try await service.refreshViewerSubscriptions(auth: auth, feedUrls: [feed])
      Issue.record("expected source failure")
    } catch let partial as AppViewEnrollmentPartialFailure {
      #expect(await recorder.feeds == [feed])
      #expect(AppViewEnrollmentError(partial).unavailablePublicationIds == [RssFeedIdentity.rssPublicationId(from: feed)])
    }
  }

  @Test("missing or stale metadata cannot claim completed RSS work")
  func completionEvidence() throws {
    let started = Date()
    let feed = "https://publisher.example/rss"
    let stale = metadata(feed: feed, at: started.addingTimeInterval(-1), errors: 0)
    #expect(throws: HTTPError.self) { try AppViewRSSRefreshEvidence.checkCompletion(nil, feedUrl: feed, started: started) }
    #expect(throws: HTTPError.self) { try AppViewRSSRefreshEvidence.checkCompletion(stale, feedUrl: feed, started: started) }
    let failed = metadata(feed: feed, at: started, errors: 1)
    #expect(throws: AppViewRSSRefreshFailure.self) { try AppViewRSSRefreshEvidence.checkCompletion(failed, feedUrl: feed, started: started) }
    #expect(throws: AppViewRSSRefreshFailure.self) { try AppViewRSSRefreshEvidence.checkBackoff(failed, feedUrl: feed, at: started) }
    try AppViewRSSRefreshEvidence.checkCompletion(metadata(feed: feed, at: started, errors: 0), feedUrl: feed, started: started)
  }

  private func metadata(feed: String, at: Date, errors: Int) -> RssFeedFetchMetadata {
    RssFeedFetchMetadata(normalizedFeedUrl: feed, etag: nil, lastModified: nil, lastPollAt: at,
      backoffUntil: errors > 0 ? at.addingTimeInterval(300) : nil, consecutiveErrorCount: errors)
  }
}
