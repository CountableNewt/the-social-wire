import Foundation
@preconcurrency import GRDB

extension SQLitePodcastStore {
  /// Scope the SQL catalog to this viewer before decrypting at most 500 rows.
  /// Private titles never enter a plaintext search index.
  public func search(viewer: String, request: PodcastSearchRequest) async throws
    -> PodcastSearchResponse
  {
    try request.validate()
    guard request.scope == nil || request.scope == "library" else {
      throw PodcastStoreError.invalidRequest
    }
    let binding = try PodcastSearch.binding(viewer: viewer, request: request)
    let cursor = try PodcastSearchCursor.decode(request.cursor, binding: binding)
    let snapshot = try await state(viewer: viewer)
    let links = snapshot.state.manualLinks.map {
      ["native": $0.protocolShowId, "rss": $0.rssShowId]
    }
    let linksJSON = try json(links)
    let hidden = Set(snapshot.state.manualLinks.map(\.protocolShowId))
    let kind = request.kind ?? "all"
    let privateEnabled = privateStorage != nil
    let rows: [(Int, String, String, Bool, String)] = try await db.read { database in
      try Row.fetchAll(
        database,
        sql: """
          WITH mapping AS (
            SELECT json_extract(value,'$.native') AS native_show_id,
              json_extract(value,'$.rss') AS rss_show_id,CAST(key AS INTEGER) AS ordinal
            FROM json_each(?)
          ), ranked_links AS (
            SELECT native_show_id,rss_show_id,
              row_number() OVER (PARTITION BY native_show_id ORDER BY ordinal) AS position
            FROM mapping WHERE EXISTS (
              SELECT 1 FROM podcast_subscriptions WHERE viewer_did=? AND show_id=mapping.rss_show_id
            )
          ), links AS (
            SELECT native_show_id,rss_show_id FROM ranked_links WHERE position=1
          ), catalog AS (
            SELECT 0 AS entity,s.id,s.id AS show_id,s.show_json AS payload,0 AS is_private
            FROM podcast_shows s JOIN podcast_subscriptions v ON v.show_id=s.id WHERE v.viewer_did=?
            UNION ALL SELECT 0,id,id,show_data,1 FROM podcast_private_shows WHERE viewer_did=? AND ?
            UNION ALL SELECT 1,e.id,COALESCE(l.rss_show_id,e.show_id),e.episode_json,0
            FROM podcast_episodes e JOIN podcast_subscriptions v ON v.show_id=e.show_id
            LEFT JOIN links l ON l.native_show_id=e.show_id
            WHERE v.viewer_did=? AND NOT EXISTS (
              SELECT 1 FROM podcast_episodes rss
              WHERE rss.show_id=l.rss_show_id AND rss.guid=e.guid AND e.guid IS NOT NULL
            )
            UNION ALL SELECT 1,id,show_id,episode_data,1 FROM podcast_private_episodes WHERE viewer_did=? AND ?
          ) SELECT entity,id,payload,is_private,show_id FROM catalog
          WHERE (?='all' OR (?='shows' AND entity=0) OR (?='episodes' AND entity=1))
            AND (? IS NULL OR show_id=?) AND (entity,id)>(?,?)
          ORDER BY entity,id LIMIT 501
          """,
        arguments: [
          linksJSON, viewer, viewer, viewer, privateEnabled, viewer, viewer, privateEnabled,
          kind, kind, kind, request.showId, request.showId, cursor?.entity ?? -1, cursor?.id ?? "",
        ]
      )
      .map { row in (row["entity"], row["id"], row["payload"], row["is_private"], row["show_id"]) }
    }
    let limit = request.limit ?? 20
    var response = PodcastSearchResponse()
    var scanned = 0
    var last: PodcastSearchCursor?
    for (entity, id, payload, isPrivate, logicalShowID) in rows {
      if scanned >= 500 || response.shows.count + response.episodes.count >= limit {
        response.hasMore = true
        break
      }
      scanned += 1
      last = PodcastSearchCursor(binding: binding, entity: entity, id: id)
      let raw =
        try isPrivate
        ? requirePrivateStorage().open(
          payload, viewer: viewer, entity: entity == 0 ? "show" : "episode", id: id)
        : payload
      if entity == 0 {
        let show = try decode(raw, PodcastShow.self)
        let visible = isPrivate ? PodcastPrivateCatalog.visible(show) : show
        if !hidden.contains(id),
          PodcastSearch.matches(
            request.query, title: visible.title,
            description: visible.description, hosts: visible.hosts)
        {
          response.shows.append(visible)
        }
      } else {
        let episode = try decode(raw, PodcastEpisode.self)
        var visible = isPrivate ? PodcastPrivateCatalog.visible(episode) : episode
        visible.showId = logicalShowID
        visible.chapterSourceUrl = nil
        if PodcastSearch.matches(
          request.query, title: visible.title, description: visible.description)
        {
          response.episodes.append(visible)
        }
      }
    }
    if response.hasMore { response.cursor = try last?.encoded() }
    return response
  }
}
