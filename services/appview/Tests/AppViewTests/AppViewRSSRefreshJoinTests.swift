import Foundation
import Hummingbird
import Logging
import Testing
import GatewayCore
import ThinAppViewCore
@testable import AppView

@Suite("Overlapping RSS refresh")
struct AppViewRSSRefreshJoinTests {
  private let feed = "https://publisher.example/rss"

  @Test("joins a background completion and continues with other feeds")
  func joinsAndContinues() async throws {
    let started = Date()
    let evidence = RSSJoinEvidence(started: started, completesAfter: 2)
    let service = ThinAppViewSkyreaderIngestionService(
      viewerFeedUrls: { _ in [feed, "https://other.example/rss"] },
      ingestFeed: { _ in Issue.record("unexpected background path"); return 0 },
      refreshFeed: { url in
        if url == feed {
          return try await AppViewRSSRefreshEvidence.refresh(feedUrl: url, started: started,
            ingest: { 0 }, metadata: { await evidence.metadata(feed: url) },
            maxWaits: 3, wait: { await evidence.advance() })
        }
        return 7
      }, logger: Logger(label: "rss.join.test"))
    let auth = AuthContext(did: "did:plc:viewer", authorizationForwardingValue: "DPoP fixture", dpopProof: "fixture")
    #expect(try await service.refreshViewerSubscriptions(auth: auth,
      feedUrls: [feed, "https://other.example/rss"]) == 7)
    #expect(await evidence.waits == 2)
  }

  @Test("retains publisher failure from the joined poll")
  func joinedFailure() async throws {
    let started = Date()
    let evidence = RSSJoinEvidence(started: started, completesAfter: 1, failed: true)
    await #expect(throws: AppViewRSSRefreshFailure.self) {
      try await AppViewRSSRefreshEvidence.refresh(feedUrl: feed, started: started,
        ingest: { 0 }, metadata: { await evidence.metadata(feed: feed) },
        maxWaits: 3, wait: { await evidence.advance() })
    }
  }

  @Test("never promotes stale evidence to completed work and bounds waiting")
  func staleTimeout() async throws {
    let started = Date()
    let evidence = RSSJoinEvidence(started: started, completesAfter: 4)
    await #expect(throws: HTTPError.self) {
      try await AppViewRSSRefreshEvidence.refresh(feedUrl: feed, started: started,
        ingest: { 0 }, metadata: { await evidence.metadata(feed: feed) },
        maxWaits: 2, wait: { await evidence.advance() })
    }
    #expect(await evidence.waits == 2)
  }

  @Test("storage errors and cancellation remain failures")
  func storageAndCancellation() async throws {
    await #expect(throws: HTTPError.self) {
      try await AppViewRSSRefreshEvidence.refresh(feedUrl: feed, started: Date(),
        ingest: { 0 }, metadata: { throw HTTPError(.serviceUnavailable) },
        wait: { Issue.record("storage failure must not wait") })
    }
    await #expect(throws: CancellationError.self) {
      try await AppViewRSSRefreshEvidence.refresh(feedUrl: feed, started: Date(),
        ingest: { 0 }, metadata: { nil }, wait: { throw CancellationError() })
    }
  }
}

private actor RSSJoinEvidence {
  let started: Date
  let completesAfter: Int
  let failed: Bool
  var waits = 0

  init(started: Date, completesAfter: Int, failed: Bool = false) {
    self.started = started
    self.completesAfter = completesAfter
    self.failed = failed
  }

  func advance() { waits += 1 }

  func metadata(feed: String) -> RssFeedFetchMetadata {
    let completed = waits >= completesAfter
    return RssFeedFetchMetadata(normalizedFeedUrl: feed, etag: nil, lastModified: nil,
      lastPollAt: started.addingTimeInterval(completed ? 1 : -1),
      backoffUntil: completed && failed ? started.addingTimeInterval(300) : nil,
      consecutiveErrorCount: completed && failed ? 1 : 0)
  }
}
