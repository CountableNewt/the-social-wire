import Foundation
import GRDB
import Logging
import Testing

@testable import ThinAppViewCore

struct SQLitePodcastStoreTests {
  private func withStore(_ body: (SQLitePodcastStore) async throws -> Void) async throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).path
    defer {
      for suffix in ["", "-wal", "-shm"] {
        try? FileManager.default.removeItem(atPath: path + suffix)
      }
    }
    let store = try SQLitePodcastStore(
      path: path, logger: Logger(label: "podcast-sqlite"),
      privateStorageKey: Data(repeating: 7, count: 32).base64EncodedString())
    try await body(store)
  }
  @Test func publicAliasesMetadataAndRevisionFencing() async throws {
    try await withStore { store in
      let show = PodcastShow(
        id: "rss-show", title: "Public", feedUrl: "https://feed.example.com/rss", sourceKind: "rss")
      var episode = PodcastEpisode(
        id: "canonical", showId: show.id, title: "Original", publishedAt: "2026-10-05T00:00:00Z",
        audioUrl: "https://media.example.com/public.mp3", guid: "guid", transcripts: [])
      episode.chapterSourceUrl = "https://media.example.com/chapters.json"
      try await store.upsert(show: show, episodes: [episode])
      var metadata = episode
      metadata.chapters = [PodcastChapter(startSeconds: 0, title: "Intro")]
      episode.title = "Refreshed"
      episode.id = "new-alias"
      try await store.upsert(show: show, episodes: [episode])
      try await store.updateMetadata(episode: metadata, viewer: nil)
      let merged = try #require(try await store.episode(id: "new-alias"))
      #expect(merged.id == "canonical" && merged.title == "Refreshed")
      #expect(merged.chapters == metadata.chapters)
      #expect(try await store.show(id: show.feedUrl!)?.id == show.id)
      let initial = try await store.state(viewer: "owner")
      var state = initial.state
      state.queue = ["canonical"]
      let saved = try await store.saveState(
        viewer: "owner", expected: initial.revision, state: state)
      #expect(saved.revision == 1)
      await #expect(throws: PodcastStoreError.revisionConflict) {
        try await store.saveState(viewer: "owner", expected: 0, state: state)
      }
      try await store.subscriptions(
        viewer: "owner", records: [(show.id, "at://owner/subscription/one")])
      #expect(
        try await store.subscribedEpisodes(viewer: "owner", cursor: nil, limit: 20).count == 1)
      #expect(try await store.queuedEpisodes(viewer: "owner").first?.id == "canonical")
    }
  }
  @Test func privateEncryptionIsolationAndAtomicUnsubscribe() async throws {
    try await withStore { store in
      let viewer = "did:plc:owner"
      let other = "did:plc:other"
      let feed = "https://feeds.example.com/private?token=owner-secret"
      let show = PodcastShow(id: "parsed-show", title: "Private", feedUrl: feed, sourceKind: "rss")
      let episode = PodcastEpisode(
        id: "parsed-episode", showId: show.id, title: "Private Episode",
        publishedAt: "2026-10-05T00:00:00Z",
        audioUrl: "https://media.example.com/private.mp3?token=owner-secret", guid: "guid",
        transcripts: [])
      let scoped = PodcastPrivateCatalog.scope(
        viewer: viewer, feedURL: feed, show: show, episodes: [episode])
      try await store.savePrivateCatalog(
        viewer: viewer, feedURL: feed, show: scoped.show, episodes: scoped.episodes)
      #expect(try await store.privateShow(viewer: other, id: scoped.show.id) == nil)
      #expect(try await store.show(id: scoped.show.id) == nil)
      #expect(try await store.ownsPrivateEpisodes(viewer: viewer, ids: [scoped.episodes[0].id]))
      #expect(
        try await store.ownsPrivateEpisodes(viewer: other, ids: [scoped.episodes[0].id]) == false)
      let encrypted = try await store.db.read {
        try String.fetchOne($0, sql: "SELECT feed_data||show_data FROM podcast_private_shows")!
      }
      #expect(!encrypted.contains("owner-secret") && !encrypted.contains("Private"))
      var state = PodcastListenerState()
      state.queue = [scoped.episodes[0].id]
      state.subscriptions = [scoped.show.id]
      _ = try await store.saveState(viewer: viewer, expected: 0, state: state)
      await #expect(throws: PodcastStoreError.invalidState) {
        try await store.saveState(viewer: other, expected: 0, state: state)
      }
      #expect(try await store.removePrivateSubscription(viewer: viewer, showID: scoped.show.id))
      #expect(
        try await store.privateEpisodes(
          viewer: viewer, showID: scoped.show.id, cursor: nil, limit: 20
        ).isEmpty)
      let updated = try await store.state(viewer: viewer)
      #expect(
        updated.revision == 2 && updated.state.queue.isEmpty && updated.state.subscriptions.isEmpty)
      await #expect(throws: PodcastStoreError.notFound) {
        try await store.savePrivateCatalog(
          viewer: viewer, feedURL: feed, show: scoped.show, episodes: scoped.episodes,
          existingOnly: true)
      }
    }
  }
}
