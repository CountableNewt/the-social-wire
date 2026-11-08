import Foundation
import GRDB
import Logging
import ReadStateCore

public actor SQLitePodcastStore: PodcastStoring {
  let db: DatabasePool
  let privateStorage: PodcastPrivateStorage?

  public init(
    path: String, logger: Logger,
    privateStorageKey: String? = ProcessInfo.processInfo.environment["PODCAST_PRIVATE_STORAGE_KEY"]
  ) throws {
    var config = Configuration()
    config.busyMode = .timeout(5)
    db = try DatabasePool(path: path, configuration: config)
    privateStorage = privateStorageKey.flatMap { try? PodcastPrivateStorage(base64Key: $0) }
    try db.write { database in try Self.migrate(database) }
  }
  func json<T: Encodable>(_ value: T) throws -> String {
    String(decoding: try JSONEncoder().encode(value), as: UTF8.self)
  }
  func decode<T: Decodable>(_ raw: String, _ type: T.Type) throws -> T {
    try JSONDecoder().decode(type, from: Data(raw.utf8))
  }

  public func upsert(show: PodcastShow, episodes: [PodcastEpisode]) async throws {
    guard show.visibility != "private", !PodcastPrivateCatalog.isPrivateID(show.id),
      episodes.allSatisfy({
        $0.visibility != "private" && !PodcastPrivateCatalog.isPrivateID($0.id)
      })
    else { throw PodcastStoreError.invalidRequest }
    let data = try json(show)
    let now = Date().timeIntervalSince1970
    try await db.write { database in
      try database.execute(
        sql:
          "INSERT INTO podcast_shows(id,feed_url,source_kind,source_uri,show_json,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET show_json=excluded.show_json,source_uri=COALESCE(excluded.source_uri,podcast_shows.source_uri),updated_at=excluded.updated_at",
        arguments: [show.id, show.feedUrl, show.sourceKind, show.sourceUri, data, now])
      for var episode in episodes {
        if let guid = episode.guid,
          let row = try Row.fetchOne(
            database,
            sql: "SELECT id,episode_json FROM podcast_episodes WHERE show_id=? AND guid=?",
            arguments: [show.id, guid])
        {
          let canonical: String = row["id"]
          let existing = try JSONDecoder().decode(
            PodcastEpisode.self, from: Data((row["episode_json"] as String).utf8))
          episode.sourceUri = episode.sourceUri ?? existing.sourceUri
          if episode.chapters.isEmpty, episode.chapterSourceUrl == existing.chapterSourceUrl {
            episode.chapters = existing.chapters
          }
          if canonical != episode.id {
            try database.execute(
              sql: "INSERT OR IGNORE INTO podcast_aliases VALUES(?,?,'episode')",
              arguments: [episode.id, canonical])
            episode.id = canonical
          }
        }
        let payload = String(decoding: try JSONEncoder().encode(episode), as: UTF8.self)
        let published =
          (try? ReadStateValidation.date(episode.publishedAt).timeIntervalSince1970) ?? 0
        try database.execute(
          sql:
            "INSERT INTO podcast_episodes(id,show_id,guid,episode_json,published_at,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET episode_json=excluded.episode_json,updated_at=excluded.updated_at",
          arguments: [episode.id, show.id, episode.guid, payload, published, now])
      }
      for alias in [show.feedUrl, show.sourceUri, show.guid.map { "guid:" + $0 }].compactMap({ $0 })
      {
        try database.execute(
          sql: "INSERT OR IGNORE INTO podcast_aliases VALUES(?,?,'show')",
          arguments: [alias, show.id])
      }
    }
  }
  public func alias(_ alias: String, canonical: String, kind: String) async throws {
    try await db.write {
      try $0.execute(
        sql: "INSERT OR IGNORE INTO podcast_aliases VALUES(?,?,?)",
        arguments: [alias, canonical, kind])
    }
  }
  public func show(id: String) async throws -> PodcastShow? {
    let raw = try await db.read {
      try String.fetchOne(
        $0,
        sql:
          "SELECT show_json FROM podcast_shows WHERE id=? OR id=(SELECT canonical_id FROM podcast_aliases WHERE alias=? AND entity_kind='show') LIMIT 1",
        arguments: [id, id])
    }
    return try raw.map { try decode($0, PodcastShow.self) }
  }
  public func shows(viewer: String) async throws -> [PodcastShow] {
    let rows = try await db.read {
      try String.fetchAll(
        $0,
        sql:
          "SELECT s.show_json FROM podcast_shows s JOIN podcast_subscriptions v ON v.show_id=s.id WHERE v.viewer_did=? ORDER BY lower(json_extract(s.show_json,'$.title')),s.id",
        arguments: [viewer])
    }
    return try rows.map { try decode($0, PodcastShow.self) }
  }
  public func episode(id: String) async throws -> PodcastEpisode? {
    let raw = try await db.read {
      try String.fetchOne(
        $0,
        sql:
          "SELECT episode_json FROM podcast_episodes WHERE id=? OR id=(SELECT canonical_id FROM podcast_aliases WHERE alias=? AND entity_kind='episode') LIMIT 1",
        arguments: [id, id])
    }
    return try raw.map { try decode($0, PodcastEpisode.self) }
  }
  public func episodes(showID: String, cursor: String?, limit: Int) async throws -> [PodcastEpisode]
  {
    let rows = try await db.read {
      try String.fetchAll(
        $0,
        sql:
          "SELECT episode_json FROM podcast_episodes WHERE show_id=? AND (? IS NULL OR (published_at,id)<(SELECT published_at,id FROM podcast_episodes WHERE id=?)) ORDER BY published_at DESC,id DESC LIMIT ?",
        arguments: [showID, cursor, cursor, max(1, min(limit, 100))])
    }
    return try rows.map { try decode($0, PodcastEpisode.self) }
  }
  public func canonicalEpisodeIDs(_ ids: [String]) async throws -> [String: String] {
    try await db.read { database in
      var result: [String: String] = [:]
      for id in ids {
        if let canonical = try String.fetchOne(
          database,
          sql: "SELECT canonical_id FROM podcast_aliases WHERE alias=? AND entity_kind='episode'",
          arguments: [id])
        {
          result[id] = canonical
        }
      }
      return result
    }
  }
  public func manualEpisodeAliases(links: [PodcastManualLink]) async throws -> [String: String] {
    try await db.read { database in
      var result: [String: String] = [:]
      for link in links {
        let rows = try Row.fetchAll(
          database,
          sql:
            "SELECT native.id AS native_id,rss.id AS rss_id FROM podcast_episodes native JOIN podcast_episodes rss ON rss.guid=native.guid WHERE native.show_id=? AND rss.show_id=? AND native.guid IS NOT NULL",
          arguments: [link.protocolShowId, link.rssShowId])
        for row in rows { result[row["native_id"]] = row["rss_id"] }
      }
      return result
    }
  }
  public func subscriptions(viewer: String, records: [(showID: String, uri: String)]) async throws {
    try await db.write { database in
      let ids = String(decoding: try JSONEncoder().encode(records.map(\.showID)), as: UTF8.self)
      try database.execute(
        sql:
          "DELETE FROM podcast_subscriptions WHERE viewer_did=? AND show_id NOT IN (SELECT value FROM json_each(?))",
        arguments: [viewer, ids])
      for record in records {
        try database.execute(
          sql:
            "INSERT INTO podcast_subscriptions(viewer_did,show_id,source_uri,updated_at) VALUES(?,?,?,?) ON CONFLICT(viewer_did,show_id) DO UPDATE SET source_uri=excluded.source_uri,updated_at=excluded.updated_at",
          arguments: [viewer, record.showID, record.uri, Date().timeIntervalSince1970])
      }
    }
  }
  public func state(viewer: String) async throws -> PodcastStateSnapshot {
    try await db.read { database in try Self.state(database, viewer: viewer) }
  }
  static func state(_ database: Database, viewer: String) throws -> PodcastStateSnapshot {
    guard
      let row = try Row.fetchOne(
        database, sql: "SELECT revision,state_json FROM podcast_viewer_state WHERE viewer_did=?",
        arguments: [viewer])
    else { return PodcastStateSnapshot(revision: 0, state: PodcastListenerState()) }
    var state = try JSONDecoder().decode(
      PodcastListenerState.self, from: Data((row["state_json"] as String).utf8))
    state.normalizePlaybackSpeed()
    return PodcastStateSnapshot(revision: row["revision"], state: state)
  }
  public func saveState(viewer: String, expected: Int64, state: PodcastListenerState) async throws
    -> PodcastStateSnapshot
  {
    guard expected >= 0, state.validate() else { throw PodcastStoreError.invalidState }
    let privateIDs = Set(state.queue + Array(state.progress.keys)).filter(
      PodcastPrivateCatalog.isPrivateID)
    let storage = privateStorage
    let payload = try json(state)
    return try await db.write { database in
      if !privateIDs.isEmpty {
        guard let storage else { throw PodcastStoreError.privateStorageUnavailable }
        for id in privateIDs {
          guard
            let raw = try String.fetchOne(
              database,
              sql: "SELECT episode_data FROM podcast_private_episodes WHERE viewer_did=? AND id=?",
              arguments: [viewer, id])
          else { throw PodcastStoreError.invalidState }
          _ = try storage.open(raw, viewer: viewer, entity: "episode", id: id)
        }
      }
      let prior = try Self.state(database, viewer: viewer)
      guard prior.revision == expected else { throw PodcastStoreError.revisionConflict }
      try database.execute(
        sql:
          "INSERT INTO podcast_viewer_state(viewer_did,revision,state_json,updated_at) VALUES(?,?,?,?) ON CONFLICT(viewer_did) DO UPDATE SET revision=excluded.revision,state_json=excluded.state_json,updated_at=excluded.updated_at",
        arguments: [viewer, expected + 1, payload, Date().timeIntervalSince1970])
      return PodcastStateSnapshot(revision: expected + 1, state: state)
    }
  }
}
