import Foundation

public protocol PodcastStoring: Sendable {
  func requirePrivateStorage() async throws -> PodcastPrivateStorage
  func stalePrivateFeeds(viewer: String, limit: Int) async throws -> [String]
  func queuedEpisodes(viewer: String) async throws -> [PodcastEpisode]
  func ownsPrivateEpisodes(viewer: String, ids: [String]) async throws -> Bool
  func savePrivateCatalog(
    viewer: String, feedURL: String, show: PodcastShow, episodes: [PodcastEpisode],
    existingOnly: Bool
  ) async throws
  func privateShows(viewer: String) async throws -> [PodcastShow]
  func privateShow(viewer: String, id: String) async throws -> PodcastShow?
  func privateFeed(viewer: String, id: String) async throws -> (url: String, refreshedAt: Date)?
  func privateEpisode(viewer: String, id: String) async throws -> PodcastEpisode?
  func privateEpisodes(viewer: String, showID: String, cursor: String?, limit: Int) async throws
    -> [PodcastEpisode]
  func subscribedEpisodes(viewer: String, cursor: String?, limit: Int) async throws
    -> [PodcastEpisode]
  func removePrivateSubscription(viewer: String, showID: String) async throws -> Bool
  func updateMetadata(episode: PodcastEpisode, viewer: String?) async throws
  func upsert(show: PodcastShow, episodes: [PodcastEpisode]) async throws
  func alias(_ alias: String, canonical: String, kind: String) async throws
  func show(id: String) async throws -> PodcastShow?
  func shows(viewer: String) async throws -> [PodcastShow]
  func episode(id: String) async throws -> PodcastEpisode?
  func episodes(showID: String, cursor: String?, limit: Int) async throws -> [PodcastEpisode]
  func canonicalEpisodeIDs(_ ids: [String]) async throws -> [String: String]
  func manualEpisodeAliases(links: [PodcastManualLink]) async throws -> [String: String]
  func subscriptions(viewer: String, records: [(showID: String, uri: String)]) async throws
  func state(viewer: String) async throws -> PodcastStateSnapshot
  func saveState(viewer: String, expected: Int64, state: PodcastListenerState) async throws
    -> PodcastStateSnapshot
  func enqueue(
    viewer: String?, episodeID: String?, kind: String, key: String, payload: String
  ) async throws -> String
  func job(
    viewer: String, jobID: String?, episodeID: String?, kind: String?, showID: String?,
    key: String?, clipID: String?
  ) async throws -> String?
  func invalidateAnalysis(id: String, fingerprint: String) async throws
  func retry(viewer: String, id: String) async throws -> Bool
  func prepareClip(viewer: String, clip: PodcastClip, payload: String) async throws -> String
  func removeClip(viewer: String, clip: PodcastClip, payload: String) async throws
  func createClip(viewer: String, clip: PodcastClip) async throws
  func clips(viewer: String) async throws -> [PodcastClip]
  func clip(id: String, viewer: String?, publishedOnly: Bool) async throws
    -> PodcastClip?
  func publishClip(id: String, viewer: String, uri: String) async throws
  func deleteClip(id: String, viewer: String) async throws
  func search(viewer: String, request: PodcastSearchRequest) async throws -> PodcastSearchResponse
}

extension PostgresPodcastStore: PodcastStoring {}

extension PodcastStoring {
  public func job(
    viewer: String, jobID: String?, episodeID: String?, kind: String?, showID: String? = nil,
    key: String? = nil, clipID: String? = nil
  ) async throws -> String? {
    try await self.job(
      viewer: viewer, jobID: jobID, episodeID: episodeID, kind: kind, showID: showID, key: key,
      clipID: clipID)
  }
  public func clip(id: String, viewer: String?, publishedOnly: Bool = false) async throws
    -> PodcastClip?
  {
    try await self.clip(id: id, viewer: viewer, publishedOnly: publishedOnly)
  }
  public func stalePrivateFeeds(viewer: String, limit: Int = 2) async throws -> [String] {
    try await self.stalePrivateFeeds(viewer: viewer, limit: limit)
  }
  public func savePrivateCatalog(
    viewer: String, feedURL: String, show: PodcastShow, episodes: [PodcastEpisode],
    existingOnly: Bool = false
  ) async throws {
    try await self.savePrivateCatalog(
      viewer: viewer, feedURL: feedURL, show: show, episodes: episodes, existingOnly: existingOnly)
  }
}
