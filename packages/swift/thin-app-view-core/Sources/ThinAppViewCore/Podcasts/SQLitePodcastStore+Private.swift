import Foundation
import GRDB
import ReadStateCore

extension SQLitePodcastStore {
  public func requirePrivateStorage() throws -> PodcastPrivateStorage {
    guard let privateStorage else { throw PodcastStoreError.privateStorageUnavailable }
    return privateStorage
  }
  func privateEpisode(_ raw: String, viewer: String, id: String) throws -> PodcastEpisode {
    try decode(
      requirePrivateStorage().open(raw, viewer: viewer, entity: "episode", id: id),
      PodcastEpisode.self)
  }
  public func stalePrivateFeeds(viewer: String, limit: Int = 2) async throws -> [String] {
    guard let storage = privateStorage else { return [] }
    return try await db.read { database in
      try Row.fetchAll(
        database,
        sql:
          "SELECT id,feed_data FROM podcast_private_shows WHERE viewer_did=? AND updated_at<? ORDER BY updated_at,id LIMIT ?",
        arguments: [
          viewer, Date().addingTimeInterval(-900).timeIntervalSince1970, max(0, min(limit, 2)),
        ]
      ).map { row in
        try storage.open(row["feed_data"], viewer: viewer, entity: "feed", id: row["id"])
      }
    }
  }
  public func queuedEpisodes(viewer: String) async throws -> [PodcastEpisode] {
    let queue = try await state(viewer: viewer).state.queue
    var result: [PodcastEpisode] = []
    for id in queue {
      if PodcastPrivateCatalog.isPrivateID(id) {
        if let episode = try await privateEpisode(viewer: viewer, id: id) { result.append(episode) }
      } else if let episode = try await episode(id: id) {
        result.append(episode)
      }
    }
    return result
  }
  public func ownsPrivateEpisodes(viewer: String, ids: [String]) async throws -> Bool {
    if ids.isEmpty { return true }
    let storage = try requirePrivateStorage()
    return try await db.read { database in
      for id in Set(ids) {
        guard
          let raw = try String.fetchOne(
            database,
            sql: "SELECT episode_data FROM podcast_private_episodes WHERE viewer_did=? AND id=?",
            arguments: [viewer, id])
        else { return false }
        let clear = try storage.open(raw, viewer: viewer, entity: "episode", id: id)
        _ = try JSONDecoder().decode(PodcastEpisode.self, from: Data(clear.utf8))
      }
      return true
    }
  }
  public func savePrivateCatalog(
    viewer: String, feedURL: String, show: PodcastShow, episodes: [PodcastEpisode],
    existingOnly: Bool = false
  ) async throws {
    guard PodcastPrivateCatalog.isAllowedURL(feedURL), show.visibility == "private",
      PodcastPrivateCatalog.isPrivateID(show.id),
      episodes.allSatisfy({
        $0.showId == show.id && $0.visibility == "private"
          && PodcastPrivateCatalog.isPrivateID($0.id)
      })
    else { throw PodcastStoreError.invalidRequest }
    let storage = try requirePrivateStorage()
    let hash = PodcastRSSParser.identity(feedURL)
    let feed = try storage.seal(feedURL, viewer: viewer, entity: "feed", id: show.id)
    let data = try storage.seal(json(show), viewer: viewer, entity: "show", id: show.id)
    try await db.write { database in
      let previous = try String.fetchOne(
        database, sql: "SELECT show_data FROM podcast_private_shows WHERE viewer_did=? AND id=?",
        arguments: [viewer, show.id])
      if let previous {
        _ = try storage.open(previous, viewer: viewer, entity: "show", id: show.id)
      }
      guard !existingOnly || previous != nil else { throw PodcastStoreError.notFound }
      try database.execute(
        sql:
          "INSERT INTO podcast_private_shows(viewer_did,id,feed_hash,feed_data,show_data,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(viewer_did,id) DO UPDATE SET feed_data=excluded.feed_data,show_data=excluded.show_data,updated_at=excluded.updated_at",
        arguments: [viewer, show.id, hash, feed, data, Date().timeIntervalSince1970])
      for var episode in episodes {
        if let raw = try String.fetchOne(
          database,
          sql: "SELECT episode_data FROM podcast_private_episodes WHERE viewer_did=? AND id=?",
          arguments: [viewer, episode.id])
        {
          let prior = try JSONDecoder().decode(
            PodcastEpisode.self,
            from: Data(storage.open(raw, viewer: viewer, entity: "episode", id: episode.id).utf8))
          if episode.chapters.isEmpty, episode.chapterSourceUrl == prior.chapterSourceUrl {
            episode.chapters = prior.chapters
          }
        }
        let clear = String(decoding: try JSONEncoder().encode(episode), as: UTF8.self)
        let encrypted = try storage.seal(clear, viewer: viewer, entity: "episode", id: episode.id)
        let published =
          (try? ReadStateValidation.date(episode.publishedAt).timeIntervalSince1970) ?? 0
        try database.execute(
          sql:
            "INSERT INTO podcast_private_episodes(viewer_did,id,show_id,episode_data,published_at) VALUES(?,?,?,?,?) ON CONFLICT(viewer_did,id) DO UPDATE SET episode_data=excluded.episode_data,published_at=excluded.published_at",
          arguments: [viewer, episode.id, show.id, encrypted, published])
      }
    }
  }
  public func privateShows(viewer: String) async throws -> [PodcastShow] {
    guard let storage = privateStorage else { return [] }
    return try await db.read { database in
      try Row.fetchAll(
        database,
        sql: "SELECT id,show_data FROM podcast_private_shows WHERE viewer_did=? ORDER BY id",
        arguments: [viewer]
      ).map { row in
        try JSONDecoder().decode(
          PodcastShow.self,
          from: Data(
            storage.open(row["show_data"], viewer: viewer, entity: "show", id: row["id"]).utf8))
      }.sorted { $0.title.localizedCaseInsensitiveCompare($1.title) == .orderedAscending }
    }
  }
  public func privateShow(viewer: String, id: String) async throws -> PodcastShow? {
    let storage = try requirePrivateStorage()
    let raw = try await db.read {
      try String.fetchOne(
        $0, sql: "SELECT show_data FROM podcast_private_shows WHERE viewer_did=? AND id=?",
        arguments: [viewer, id])
    }
    return try raw.map {
      try decode(storage.open($0, viewer: viewer, entity: "show", id: id), PodcastShow.self)
    }
  }
  public func privateFeed(viewer: String, id: String) async throws -> (
    url: String, refreshedAt: Date
  )? {
    let storage = try requirePrivateStorage()
    return try await db.read { database in
      guard
        let row = try Row.fetchOne(
          database,
          sql: "SELECT feed_data,updated_at FROM podcast_private_shows WHERE viewer_did=? AND id=?",
          arguments: [viewer, id])
      else { return nil }
      return (
        try storage.open(row["feed_data"], viewer: viewer, entity: "feed", id: id),
        Date(timeIntervalSince1970: row["updated_at"])
      )
    }
  }
  public func privateEpisode(viewer: String, id: String) async throws -> PodcastEpisode? {
    _ = try requirePrivateStorage()
    let raw = try await db.read {
      try String.fetchOne(
        $0, sql: "SELECT episode_data FROM podcast_private_episodes WHERE viewer_did=? AND id=?",
        arguments: [viewer, id])
    }
    return try raw.map { try privateEpisode($0, viewer: viewer, id: id) }
  }
  public func privateEpisodes(viewer: String, showID: String, cursor: String?, limit: Int)
    async throws -> [PodcastEpisode]
  {
    let storage = try requirePrivateStorage()
    return try await db.read { database in
      try Row.fetchAll(
        database,
        sql:
          "SELECT id,episode_data FROM podcast_private_episodes WHERE viewer_did=? AND show_id=? AND (? IS NULL OR (published_at,id)<(SELECT published_at,id FROM podcast_private_episodes WHERE viewer_did=? AND id=?)) ORDER BY published_at DESC,id DESC LIMIT ?",
        arguments: [viewer, showID, cursor, viewer, cursor, max(1, min(limit, 100))]
      ).map { row in
        try JSONDecoder().decode(
          PodcastEpisode.self,
          from: Data(
            storage.open(row["episode_data"], viewer: viewer, entity: "episode", id: row["id"]).utf8
          ))
      }
    }
  }
  public func subscribedEpisodes(viewer: String, cursor: String?, limit: Int) async throws
    -> [PodcastEpisode]
  {
    let storage = privateStorage
    return try await db.read { database in
      try Row.fetchAll(
        database,
        sql: """
          WITH catalog AS (
            SELECT e.id,e.published_at,e.episode_json AS payload,0 AS is_private FROM podcast_episodes e JOIN podcast_subscriptions s ON s.show_id=e.show_id WHERE s.viewer_did=?
            UNION ALL SELECT id,published_at,episode_data,1 FROM podcast_private_episodes WHERE viewer_did=? AND ?
          ) SELECT id,payload,is_private FROM catalog WHERE (? IS NULL OR (published_at,id)<(SELECT published_at,id FROM catalog WHERE id=?)) ORDER BY published_at DESC,id DESC LIMIT ?
          """, arguments: [viewer, viewer, storage != nil, cursor, cursor, max(1, min(limit, 100))]
      ).map { row in
        let raw: String = row["payload"]
        let clear: String
        if row["is_private"] as Bool {
          guard let storage else { throw PodcastStoreError.privateStorageUnavailable }
          clear = try storage.open(raw, viewer: viewer, entity: "episode", id: row["id"])
        } else {
          clear = raw
        }
        return try JSONDecoder().decode(PodcastEpisode.self, from: Data(clear.utf8))
      }
    }
  }
  public func removePrivateSubscription(viewer: String, showID: String) async throws -> Bool {
    let storage = try requirePrivateStorage()
    return try await db.write { database in
      guard
        let raw = try String.fetchOne(
          database, sql: "SELECT show_data FROM podcast_private_shows WHERE viewer_did=? AND id=?",
          arguments: [viewer, showID])
      else { return false }
      _ = try storage.open(raw, viewer: viewer, entity: "show", id: showID)
      let ids = Set(
        try String.fetchAll(
          database, sql: "SELECT id FROM podcast_private_episodes WHERE viewer_did=? AND show_id=?",
          arguments: [viewer, showID]))
      try database.execute(
        sql: "DELETE FROM podcast_private_shows WHERE viewer_did=? AND id=?",
        arguments: [viewer, showID])
      if let row = try Row.fetchOne(
        database, sql: "SELECT state_json,revision FROM podcast_viewer_state WHERE viewer_did=?",
        arguments: [viewer])
      {
        var state = try JSONDecoder().decode(
          PodcastListenerState.self, from: Data((row["state_json"] as String).utf8))
        state.queue.removeAll { ids.contains($0) }
        state.progress = state.progress.filter { !ids.contains($0.key) }
        state.subscriptions.removeAll { $0 == showID }
        let payload = String(decoding: try JSONEncoder().encode(state), as: UTF8.self)
        try database.execute(
          sql:
            "UPDATE podcast_viewer_state SET revision=revision+1,state_json=?,updated_at=? WHERE viewer_did=?",
          arguments: [payload, Date().timeIntervalSince1970, viewer])
      }
      return true
    }
  }
}
