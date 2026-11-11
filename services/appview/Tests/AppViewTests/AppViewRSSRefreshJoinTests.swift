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
            ingest: { .leaseBusy }, metadata: { await evidence.metadata(feed: url) },
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
        ingest: { .leaseBusy }, metadata: { await evidence.metadata(feed: feed) },
        maxWaits: 3, wait: { await evidence.advance() })
    }
  }

  @Test("never promotes stale evidence to completed work and bounds waiting")
  func staleTimeout() async throws {
    let started = Date()
    let evidence = RSSJoinEvidence(started: started, completesAfter: 4)
    await #expect(throws: AppViewRSSRefreshJoinTimeout.self) {
      try await AppViewRSSRefreshEvidence.refresh(feedUrl: feed, started: started,
        ingest: { .leaseBusy }, metadata: { await evidence.metadata(feed: feed) },
        maxWaits: 2, wait: { await evidence.advance() })
    }
    #expect(await evidence.waits == 2)
  }

  @Test("an owner that leaves no completion can be replaced on the next attempt")
  func reacquiresAfterOwnerExits() async throws {
    let started = Date()
    let evidence = RSSJoinEvidence(started: started, completesAfter: 1)
    let indexed = try await AppViewRSSRefreshEvidence.refresh(feedUrl: feed, started: started,
      ingest: {
        guard await evidence.waits > 0 else { return .leaseBusy }
        return .completed(indexed: 4, metadata: await evidence.metadata(feed: feed))
      }, metadata: { nil }, maxWaits: 2, wait: { await evidence.advance() })
    #expect(indexed == 4)
    #expect(await evidence.waits == 1)
  }

  @Test("blocked feeds, active backoff, and own completed polls never join")
  func onlyHeldLeasesWait() async throws {
    let started = Date()
    let complete = await RSSJoinEvidence(started: started, completesAfter: 0).metadata(feed: feed)
    let failed = await RSSJoinEvidence(started: started, completesAfter: 0, failed: true).metadata(feed: feed)
    let read: @Sendable () async throws -> RssFeedFetchMetadata? = {
      Issue.record("only a busy lease should read join evidence")
      return nil
    }
    let wait: @Sendable () async throws -> Void = { Issue.record("only a busy lease should wait") }
    #expect(try await AppViewRSSRefreshEvidence.refresh(feedUrl: feed, started: started,
      ingest: { .completed(indexed: 0, metadata: complete) }, metadata: read, wait: wait) == 0)
    let unavailable: [RssFeedPollResult] = [.unfetchable, .backedOff(failed), .completed(indexed: 0, metadata: failed)]
    for result in unavailable {
      await #expect(throws: AppViewRSSRefreshFailure.self) {
        try await AppViewRSSRefreshEvidence.refresh(feedUrl: feed, started: started,
          ingest: { result }, metadata: read, wait: wait)
      }
    }
    await #expect(throws: HTTPError.self) {
      try await AppViewRSSRefreshEvidence.refresh(feedUrl: feed, started: started,
        ingest: { throw HTTPError(.serviceUnavailable) }, metadata: read, wait: wait)
    }
  }

  @Test("a join timeout still attempts subsequent healthy feeds without claiming partial success")
  func timeoutContinues() async throws {
    let healthy = "https://other.example/rss"
    let recorder = SkyreaderWarmingRecorder()
    let service = ThinAppViewSkyreaderIngestionService(
      viewerFeedUrls: { _ in [feed, healthy] }, ingestFeed: { _ in 0 },
      refreshFeed: { url in
        await recorder.recordFeed(url)
        if url == feed { throw AppViewRSSRefreshJoinTimeout() }
        return 3
      }, logger: Logger(label: "rss.join.timeout.test"))
    let auth = AuthContext(did: "did:plc:viewer", authorizationForwardingValue: "DPoP fixture", dpopProof: "fixture")
    await #expect(throws: AppViewRSSRefreshJoinTimeout.self) {
      try await service.refreshViewerSubscriptions(auth: auth, feedUrls: [feed, healthy])
    }
    #expect(await recorder.feeds == [feed, healthy])
  }

  @Test("storage errors and cancellation remain failures")
  func storageAndCancellation() async throws {
    await #expect(throws: HTTPError.self) {
      try await AppViewRSSRefreshEvidence.refresh(feedUrl: feed, started: Date(),
        ingest: { .leaseBusy }, metadata: { throw HTTPError(.serviceUnavailable) },
        wait: { Issue.record("storage failure must not wait") })
    }
    await #expect(throws: CancellationError.self) {
      try await AppViewRSSRefreshEvidence.refresh(feedUrl: feed, started: Date(),
        ingest: { .leaseBusy }, metadata: { nil }, wait: { throw CancellationError() })
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
