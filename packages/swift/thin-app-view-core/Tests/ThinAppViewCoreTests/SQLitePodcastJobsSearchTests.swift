import Foundation
@preconcurrency import GRDB
import Logging
import Testing

@testable import ThinAppViewCore

struct SQLitePodcastJobsSearchTests {
  @Test func searchAdvancesOverNonmatchesAndBindsCursorToViewer() async throws {
    let (store, path) = try fixture()
    defer { remove(path) }
    let show = PodcastShow(id: "show", title: "Public Café Search", sourceKind: "rss")
    let episodes = (0..<505).map { index in
      PodcastEpisode(
        id: String(format: "episode-%04d", index), showId: show.id,
        title: index == 504 ? "Late Needle" : "Ordinary Episode",
        publishedAt: "2026-10-05T00:00:00Z",
        audioUrl: "https://example.com/audio.mp3", transcripts: [])
    }
    try await store.upsert(show: show, episodes: episodes)
    try await store.subscriptions(
      viewer: "owner", records: [(show.id, "at://owner/subscription/one")])
    let first = try await store.search(
      viewer: "owner", request: .init(query: "late needle", kind: "episodes"))
    #expect(first.episodes.isEmpty && first.hasMore)
    let cursor = try #require(first.cursor)
    let second = try await store.search(
      viewer: "owner", request: .init(query: "late needle", kind: "episodes", cursor: cursor))
    #expect(second.episodes.map(\.id) == [episodes[504].id])
    #expect(!second.hasMore)
    await #expect(throws: PodcastStoreError.invalidRequest) {
      try await store.search(
        viewer: "other", request: .init(query: "late needle", kind: "episodes", cursor: cursor))
    }
    let foreign = try await store.search(viewer: "other", request: .init(query: "cafe"))
    #expect(foreign.shows.isEmpty && foreign.episodes.isEmpty)
    let own = try await store.search(viewer: "owner", request: .init(query: "cafe", kind: "shows"))
    #expect(own.shows.map(\.id) == [show.id])
  }

  @Test func privateSearchDecryptsOnlyViewerRowsAndRedactsSourceSecrets() async throws {
    let (store, path) = try fixture()
    defer { remove(path) }
    let feed = "https://feeds.example.com/owner-secret.xml"
    let show = PodcastShow(id: "raw-show", title: "Private Show", feedUrl: feed, sourceKind: "rss")
    var episode = PodcastEpisode(
      id: "raw-episode", showId: show.id, title: "Private Episode",
      publishedAt: "2026-10-05T00:00:00Z", audioUrl: "https://media.example.com/owner-secret.mp3",
      transcripts: [])
    episode.chapterSourceUrl = "https://media.example.com/owner-secret.json"
    let scoped = PodcastPrivateCatalog.scope(
      viewer: "owner", feedURL: feed, show: show, episodes: [episode])
    try await store.savePrivateCatalog(
      viewer: "owner", feedURL: feed, show: scoped.show, episodes: scoped.episodes)
    let own = try await store.search(viewer: "owner", request: .init(query: "private"))
    #expect(own.shows.map(\.id) == [scoped.show.id])
    #expect(own.episodes.map(\.id) == [scoped.episodes[0].id])
    let encoded = String(decoding: try JSONEncoder().encode(own), as: UTF8.self)
    #expect(!encoded.contains("owner-secret"))
    #expect(own.shows.first?.feedUrl == nil && own.episodes.first?.chapterSourceUrl == nil)
    let foreign = try await store.search(
      viewer: "other", request: .init(query: "private", showId: scoped.show.id))
    #expect(foreign.shows.isEmpty && foreign.episodes.isEmpty)
    let wrong = try SQLitePodcastStore(
      path: path, logger: Logger(label: "wrong-podcast-key"),
      privateStorageKey: Data(repeating: 9, count: 32).base64EncodedString())
    await #expect(throws: PodcastStoreError.privateStorageUnavailable) {
      try await wrong.search(viewer: "owner", request: .init(query: "private"))
    }
  }

  @Test func jobsAreDurableDeduplicatedAndScopedToOwnersOrSubscribers() async throws {
    let (store, path) = try fixture()
    defer { remove(path) }
    try await seed(store)
    let owned = try await store.enqueue(
      viewer: "owner", episodeID: "episode", kind: "clip", key: "owner-clip", payload: "{}")
    let deduplicated = try await store.enqueue(
      viewer: "owner", episodeID: "episode", kind: "clip", key: "owner-clip", payload: "{}")
    #expect(owned == deduplicated)
    let foreign = try await store.job(viewer: "other", jobID: owned, episodeID: nil, kind: nil)
    #expect(foreign == nil)
    let bridge = try await store.enqueue(
      viewer: nil, episodeID: nil, kind: "bridge", key: "bridge-show",
      payload: #"{"showId":"show"}"#)
    let hidden = try await store.job(viewer: "other", jobID: bridge, episodeID: nil, kind: nil)
    #expect(hidden == nil)
    try await store.subscriptions(
      viewer: "other", records: [("show", "at://other/subscription/one")])
    let visible = try await store.job(viewer: "other", jobID: bridge, episodeID: nil, kind: nil)
    #expect(visible != nil)
    let reopened = try SQLitePodcastStore(
      path: path, logger: Logger(label: "sqlite-podcast-reopen"))
    let persisted = try await reopened.job(viewer: "owner", jobID: owned, episodeID: nil, kind: nil)
    #expect(persisted != nil)
    await #expect(throws: PodcastStoreError.invalidRequest) {
      try await store.enqueue(
        viewer: "owner", episodeID: "private-podcast:episode:secret", kind: "silence",
        key: "invalid", payload: "{}")
    }
    let database = await store.db
    try await database.write { db in
      try db.execute(
        sql: "UPDATE podcast_jobs SET status='failed',attempts=4 WHERE id=?", arguments: [owned])
    }
    let denied = try await store.retry(viewer: "other", id: owned)
    let retried = try await store.retry(viewer: "owner", id: owned)
    #expect(!denied && retried)
    try await database.write { db in
      try db.execute(
        sql: "UPDATE podcast_jobs SET status='failed',attempts=5 WHERE id=?", arguments: [owned])
    }
    let exhausted = try await store.retry(viewer: "owner", id: owned)
    #expect(!exhausted)
  }

  @Test func clipsAndAnalysisKeepAtomicStateAndOwnership() async throws {
    let (store, path) = try fixture()
    defer { remove(path) }
    try await seed(store)
    let clip = PodcastClip(
      id: UUID().uuidString.lowercased(), episodeId: "episode", startSeconds: 0,
      endSeconds: 10, title: "Private Clip", createdAt: "2026-10-05T00:00:00Z")
    let payload = String(
      decoding: try JSONSerialization.data(withJSONObject: ["clipId": clip.id]), as: UTF8.self)
    let jobID = try await store.prepareClip(viewer: "owner", clip: clip, payload: payload)
    let own = try await store.clips(viewer: "owner")
    let foreign = try await store.clips(viewer: "other")
    #expect(own.first?.jobId == jobID && foreign.isEmpty)
    try await store.publishClip(id: clip.id, viewer: "other", uri: "at://other/clip/forged")
    let unpublished = try await store.clip(id: clip.id, viewer: nil, publishedOnly: true)
    #expect(unpublished == nil)
    try await store.publishClip(id: clip.id, viewer: "owner", uri: "at://owner/clip/published")
    let published = try await store.clip(
      id: "at://owner/clip/published", viewer: nil, publishedOnly: true)
    #expect(published?.publishedUri == "at://owner/clip/published")
    try await store.removeClip(viewer: "other", clip: clip, payload: payload)
    let retained = try await store.clip(id: clip.id, viewer: "owner")
    #expect(retained != nil)
    try await store.removeClip(viewer: "owner", clip: clip, payload: payload)
    let removed = try await store.clip(id: clip.id, viewer: "owner")
    let cleanup = try await store.job(
      viewer: "owner", jobID: nil, episodeID: nil, kind: "cleanup", clipID: clip.id)
    #expect(removed == nil && cleanup != nil)
    let silence = try await store.enqueue(
      viewer: nil, episodeID: "episode", kind: "silence", key: "silence", payload: "{}")
    let database = await store.db
    try await database.write { db in
      try db.execute(
        sql: "UPDATE podcast_jobs SET status='complete',result_json=? WHERE id=?",
        arguments: [#"{"sourceFingerprint":"old"}"#, silence])
    }
    try await store.invalidateAnalysis(id: silence, fingerprint: "new")
    let failed = try await store.job(viewer: "other", jobID: silence, episodeID: nil, kind: nil)
    #expect(failed?.contains("Media Source Changed During Analysis") == true)
  }

  private func seed(_ store: SQLitePodcastStore) async throws {
    let show = PodcastShow(id: "show", title: "Show", sourceKind: "rss")
    let episode = PodcastEpisode(
      id: "episode", showId: show.id, title: "Episode",
      publishedAt: "2026-10-05T00:00:00Z", audioUrl: "https://example.com/audio.mp3",
      transcripts: [])
    try await store.upsert(show: show, episodes: [episode])
  }

  private func fixture() throws -> (SQLitePodcastStore, String) {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).path
    return (
      try SQLitePodcastStore(
        path: path, logger: Logger(label: "sqlite-podcast-jobs-search"),
        privateStorageKey: Data(repeating: 7, count: 32).base64EncodedString()), path
    )
  }
  private func remove(_ path: String) {
    for suffix in ["", "-wal", "-shm"] {
      try? FileManager.default.removeItem(atPath: path + suffix)
    }
  }
}
