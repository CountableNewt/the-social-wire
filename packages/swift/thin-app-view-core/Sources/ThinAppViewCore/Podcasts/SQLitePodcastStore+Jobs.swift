import Foundation
@preconcurrency import GRDB

extension SQLitePodcastStore {
  public func enqueue(
    viewer: String?, episodeID: String?, kind: String, key: String, payload: String
  ) async throws -> String {
    if let episodeID, PodcastPrivateCatalog.isPrivateID(episodeID) {
      throw PodcastStoreError.invalidRequest
    }
    _ = try JSONSerialization.jsonObject(with: Data(payload.utf8), options: .fragmentsAllowed)
    let id = UUID().uuidString.lowercased()
    let now = Date().timeIntervalSince1970
    return try await db.write { database in
      guard
        let saved = try String.fetchOne(
          database,
          sql: """
            INSERT INTO podcast_jobs(id,viewer_did,episode_id,kind,dedupe_key,payload_json,status,attempts,available_at,created_at,updated_at)
            VALUES(?,?,?,?,?,?,'queued',0,?,?,?)
            ON CONFLICT(dedupe_key) DO UPDATE SET dedupe_key=excluded.dedupe_key RETURNING id
            """, arguments: [id, viewer, episodeID, kind, key, payload, now, now, now])
      else { throw PodcastStoreError.notFound }
      return saved
    }
  }

  public func job(
    viewer: String, jobID: String?, episodeID: String?, kind: String?, showID: String? = nil,
    key: String? = nil, clipID: String? = nil
  ) async throws -> String? {
    try await db.read { database in
      try String.fetchOne(
        database,
        sql: """
          SELECT json_object('id',id,'kind',kind,'status',status,'result',json(result_json),'error',error)
          FROM podcast_jobs
          WHERE (viewer_did=? OR (viewer_did IS NULL AND kind='silence')
            OR (viewer_did IS NULL AND kind='bridge' AND EXISTS (
              SELECT 1 FROM podcast_subscriptions s WHERE s.viewer_did=?
                AND s.show_id=json_extract(podcast_jobs.payload_json,'$.showId')
            )))
            AND (? IS NULL OR id=?) AND (? IS NULL OR episode_id=?) AND (? IS NULL OR kind=?)
            AND (? IS NULL OR json_extract(payload_json,'$.showId')=?) AND (? IS NULL OR dedupe_key=?)
            AND (? IS NULL OR json_extract(payload_json,'$.clipId')=?)
          ORDER BY created_at DESC LIMIT 1
          """,
        arguments: [
          viewer, viewer, jobID, jobID, episodeID, episodeID, kind, kind,
          showID, showID, key, key, clipID, clipID,
        ])
    }
  }

  public func invalidateAnalysis(id: String, fingerprint: String) async throws {
    try await db.write { database in
      try database.execute(
        sql: """
          UPDATE podcast_jobs SET status='failed',result_json=NULL,
            error='Media Source Changed During Analysis',updated_at=?
          WHERE id=? AND kind='silence' AND status='complete'
            AND json_extract(result_json,'$.sourceFingerprint') IS NOT ?
          """, arguments: [Date().timeIntervalSince1970, id, fingerprint])
    }
  }

  public func retry(viewer: String, id: String) async throws -> Bool {
    guard try await job(viewer: viewer, jobID: id, episodeID: nil, kind: nil) != nil else {
      return false
    }
    return try await db.write { database in
      guard
        let row = try Row.fetchOne(
          database,
          sql: """
            UPDATE podcast_jobs SET status='queued',available_at=?,error=NULL,updated_at=?
            WHERE id=? AND status='failed' AND attempts<5 AND kind IN ('bridge','silence','clip')
            RETURNING kind,payload_json
            """, arguments: [Date().timeIntervalSince1970, Date().timeIntervalSince1970, id])
      else { return false }
      if row["kind"] as String == "clip" {
        let payload: String = row["payload_json"]
        try database.execute(
          sql: """
            UPDATE podcast_clips SET clip_json=json_set(json_remove(clip_json,'$.error'),'$.status','queued'),updated_at=?
            WHERE id=json_extract(?,'$.clipId') AND viewer_did=?
            """, arguments: [Date().timeIntervalSince1970, payload, viewer])
      }
      return true
    }
  }

  public func prepareClip(viewer: String, clip: PodcastClip, payload: String) async throws -> String
  {
    guard let parsed = UUID(uuidString: clip.id), !PodcastPrivateCatalog.isPrivateID(clip.episodeId)
    else { throw PodcastStoreError.invalidRequest }
    _ = try JSONSerialization.jsonObject(with: Data(payload.utf8), options: .fragmentsAllowed)
    let id = parsed.uuidString.lowercased()
    let jobID = UUID().uuidString.lowercased()
    let data = try json(clip)
    let now = Date().timeIntervalSince1970
    try await db.write { database in
      try database.execute(
        sql: """
          INSERT INTO podcast_clips(id,viewer_did,episode_id,clip_json,created_at,updated_at) VALUES(?,?,?,?,?,?)
          """, arguments: [id, viewer, clip.episodeId, data, now, now])
      try database.execute(
        sql: """
          INSERT INTO podcast_jobs(id,viewer_did,episode_id,kind,dedupe_key,payload_json,status,attempts,available_at,created_at,updated_at)
          VALUES(?,?,?,'clip',?,?,'queued',0,?,?,?)
          """,
        arguments: [jobID, viewer, clip.episodeId, "clip:" + clip.id, payload, now, now, now])
    }
    return jobID
  }

  public func removeClip(viewer: String, clip: PodcastClip, payload: String) async throws {
    _ = try JSONSerialization.jsonObject(with: Data(payload.utf8), options: .fragmentsAllowed)
    let jobID = UUID().uuidString.lowercased()
    let now = Date().timeIntervalSince1970
    try await db.write { database in
      guard
        try String.fetchOne(
          database, sql: "DELETE FROM podcast_clips WHERE id=? AND viewer_did=? RETURNING id",
          arguments: [clip.id, viewer]) != nil
      else { return }
      try database.execute(
        sql: """
          INSERT INTO podcast_jobs(id,viewer_did,episode_id,kind,dedupe_key,payload_json,status,attempts,available_at,created_at,updated_at)
          VALUES(?,?,?,'cleanup',?,?,'queued',0,?,?,?) ON CONFLICT(dedupe_key) DO NOTHING
          """,
        arguments: [jobID, viewer, clip.episodeId, "cleanup:" + clip.id, payload, now, now, now])
    }
  }

  public func createClip(viewer: String, clip: PodcastClip) async throws {
    guard let parsed = UUID(uuidString: clip.id) else { throw PodcastStoreError.invalidRequest }
    let data = try json(clip)
    let now = Date().timeIntervalSince1970
    try await db.write { database in
      try database.execute(
        sql: """
          INSERT INTO podcast_clips(id,viewer_did,episode_id,clip_json,created_at,updated_at) VALUES(?,?,?,?,?,?)
          """, arguments: [parsed.uuidString.lowercased(), viewer, clip.episodeId, data, now, now])
    }
  }

  public func clips(viewer: String) async throws -> [PodcastClip] {
    let rows = try await db.read { database in
      try String.fetchAll(
        database,
        sql: """
          SELECT json_set(clip_json,'$.jobId',(
            SELECT id FROM podcast_jobs WHERE json_extract(payload_json,'$.clipId')=podcast_clips.id
              AND kind='clip' ORDER BY created_at DESC LIMIT 1
          )) FROM podcast_clips WHERE viewer_did=? ORDER BY updated_at DESC LIMIT 100
          """, arguments: [viewer])
    }
    return try rows.map { try decode($0, PodcastClip.self) }
  }

  public func clip(id: String, viewer: String?, publishedOnly: Bool = false) async throws
    -> PodcastClip?
  {
    let raw = try await db.read { database in
      try String.fetchOne(
        database,
        sql: """
          SELECT clip_json FROM podcast_clips WHERE (id=? OR published_uri=?)
            AND (? IS NULL OR viewer_did=?) AND (NOT ? OR published_uri IS NOT NULL) LIMIT 1
          """, arguments: [id, id, viewer, viewer, publishedOnly])
    }
    return try raw.map { try decode($0, PodcastClip.self) }
  }

  public func publishClip(id: String, viewer: String, uri: String) async throws {
    try await db.write { database in
      try database.execute(
        sql: """
          UPDATE podcast_clips SET published_uri=?,clip_json=json_set(clip_json,'$.publishedUri',?),updated_at=?
          WHERE id=? AND viewer_did=?
          """, arguments: [uri, uri, Date().timeIntervalSince1970, id, viewer])
    }
  }

  public func deleteClip(id: String, viewer: String) async throws {
    try await db.write { database in
      try database.execute(
        sql: "DELETE FROM podcast_clips WHERE id=? AND viewer_did=?", arguments: [id, viewer])
    }
  }
}
