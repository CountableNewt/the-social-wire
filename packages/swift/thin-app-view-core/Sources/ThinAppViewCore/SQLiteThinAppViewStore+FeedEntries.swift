import Foundation
import GRDB
import ReadStateCore

extension SQLiteThinAppViewStore {
  /// Merge scoped feeds in one ordered database scan, retaining each publication's
  /// own read floor and duplicate suppression instead of querying every feed.
  public func listFeedEntries(
    viewerDid: String,
    scopes: [PublicationUnreadScope],
    filter: EntryListFilter,
    cursor: String?,
    limit: Int
  ) async throws -> AppViewEntryListResponse {
    let pageLimit = max(1, min(limit, 100))
    guard !scopes.isEmpty else { return .init(entries: [], cursor: nil) }
    if scopes.count == 1, let scope = scopes.first {
      let boundary = filter == .all ? nil
        : try await readBoundary(viewerDid: viewerDid, publicationId: scope.publicationId)
      let page = try await listEntries(
        viewerDid: viewerDid, authorDid: scope.authorDid,
        publicationAtUri: scope.publicationAtUri,
        publicationScopeAtUris: scope.publicationScopeAtUris,
        publicationSiteUrls: scope.publicationSiteUrls, filter: filter,
        cursor: cursor, limit: min(100, pageLimit + 1), readBoundary: boundary)
      let entries = page.entries.prefix(pageLimit).map { $0.withPublicationId(scope.publicationId) }
      let hasMore = page.cursor != nil || page.entries.count > pageLimit
      return .init(entries: entries, cursor: hasMore ? entries.last.map {
        ThinAppViewCursor.encode(createdAt: $0.feedPositionAt, uri: $0.entryId)
      } : nil)
    }

    let now = ISO8601DateFormatter().string(from: Date())
    let batchSize = max(100, pageLimit * 4)
    return try await db.read { database in
      let requests = try Self.requestedFeedScopes(
        database: database, viewerDid: viewerDid, scopes: scopes, filter: filter, now: now)
      guard !requests.isEmpty else { return .init(entries: [], cursor: nil) }
      var broadRequests: [String: [RequestedFeedScope]] = [:]
      var siteRequests: [String: [String: [RequestedFeedScope]]] = [:]
      for request in requests {
        if let site = request.site {
          siteRequests[request.authorDid, default: [:]][site, default: []].append(request)
        } else {
          broadRequests[request.authorDid, default: []].append(request)
        }
      }
      let encoder = JSONEncoder()
      let encodedBroad = try broadRequests.mapValues { String(decoding: try encoder.encode($0), as: UTF8.self) }
      let encodedSites = try Dictionary(uniqueKeysWithValues: siteRequests.map { author, sites in
        (author, try sites.mapValues { values in
          String(decoding: try encoder.encode(values + (broadRequests[author] ?? [])), as: UTF8.self)
        })
      })
      // The read closure owns this connection until every statement finishes.
      // Resolve raw-site membership once in Swift instead of scanning hundreds
      // of requested scopes for every content row, including exhausted feeds.
      let scopeFunction = DatabaseFunction("tsw_requested_feed_scopes", argumentCount: 2, pure: true) { values in
        guard let author = String.fromDatabaseValue(values[0]) else { return "[]" }
        if let site = String.fromDatabaseValue(values[1]), let scopes = encodedSites[author]?[site] {
          return scopes
        }
        return encodedBroad[author] ?? "[]"
      }
      database.add(function: scopeFunction)
      defer { database.remove(function: scopeFunction) }
      let matchedScopesSQL = """
        SELECT json_extract(value,'$.position') AS position,
               json_extract(value,'$.authorDid') AS author_did,
               json_extract(value,'$.site') AS site,
               json_extract(value,'$.floorAt') AS floor_at,
               json_extract(value,'$.floorUri') AS floor_uri
        FROM json_each(tsw_requested_feed_scopes(ci.author_did,ci.publication_site))
        """
      let timestampSQL = Self.feedTimestampSQL("ci.created_at")
      let readPredicate: String
      switch filter {
      case .all:
        readPredicate = "1"
      case .unread:
        readPredicate = """
          rm.subject_uri IS NULL AND (
            scope.floor_at IS NULL OR uo.subject_uri IS NOT NULL
            OR \(timestampSQL) > scope.floor_at
            OR (\(timestampSQL) = scope.floor_at
                AND scope.floor_uri IS NOT NULL AND ci.uri > scope.floor_uri))
          """
      case .read:
        readPredicate = """
          rm.subject_uri IS NOT NULL OR (
            uo.subject_uri IS NULL AND scope.floor_at IS NOT NULL AND (
              \(timestampSQL) < scope.floor_at
              OR (\(timestampSQL) = scope.floor_at
                  AND (scope.floor_uri IS NULL OR ci.uri <= scope.floor_uri))))
          """
      }
      let membership = """
        scope.author_did = ci.author_did
        AND (scope.site IS NULL OR scope.site = ci.publication_site)
        AND (\(readPredicate))
        """
      var databaseCursor = cursor.flatMap(ThinAppViewCursor.decode)
      var grouped: [Int: [EntryListScanRow]] = [:]
      var candidates: [(entry: AppViewEntryListItem, position: Int)] = []
      var hasMore = false
      repeat {
        var arguments: [DatabaseValueConvertible?] = [viewerDid, viewerDid, now]
        var cursorSQL = ""
        if let databaseCursor {
          let timestamp = Self.feedTimestampKey(databaseCursor.createdAt)
          cursorSQL = " AND (\(timestampSQL) < ? OR (\(timestampSQL) = ? AND ci.uri < ?))"
          arguments.append(contentsOf: [timestamp, timestamp, databaseCursor.uri])
        }
        arguments.append(batchSize)
        // The global ordering index makes this a single seekable scan. Scope and
        // read filters run before LIMIT; only matching rows decode render JSON.
        let rows = try Row.fetchAll(database, sql: """
          SELECT ci.uri,ci.render_json,ci.created_at,ci.publication_site,
                 (SELECT json_group_array(scope.position) FROM (\(matchedScopesSQL)) scope
                  WHERE \(membership)) AS positions
          FROM content_items ci INDEXED BY idx_content_items_timestamp_uri
          LEFT JOIN read_marks rm ON rm.viewer_did=? AND rm.subject_uri=ci.uri
          LEFT JOIN appview_unread_overrides uo ON uo.viewer_did=? AND uo.subject_uri=ci.uri
          WHERE ci.expires_at > ? \(cursorSQL)
            AND EXISTS(SELECT 1 FROM (\(matchedScopesSQL)) scope WHERE \(membership))
          ORDER BY \(timestampSQL) DESC,ci.uri DESC LIMIT ?
          """, arguments: StatementArguments(arguments))
        hasMore = rows.count == batchSize
        for row in rows {
          let raw: String = row["created_at"]
          let createdAt = (try? ReadStateValidation.date(raw)) ?? .distantPast
          let uri: String = row["uri"]
          databaseCursor = (createdAt, uri)
          let scan = EntryListScanRow(uri: uri, renderJSON: row["render_json"],
                                     createdAt: createdAt, publicationSite: row["publication_site"])
          let positions: String = row["positions"]
          for position in try JSONDecoder().decode([Int].self, from: Data(positions.utf8)) {
            grouped[position, default: []].append(scan)
          }
        }
        candidates = grouped.flatMap { position, rows in
          // Suppress article duplicates within one publication, never across
          // overlapping publications (the aggregate feed has different rules).
          ThinAppViewQuerySupport.entryListItems(
            from: ThinAppViewQuerySupport.dedupeScanRows(rows).map { ($0.uri, $0.renderJSON, $0.createdAt) }
          ).map { (entry: $0.withPublicationId(scopes[position].publicationId), position: position) }
        }
        candidates.sort {
          if $0.entry.feedPositionAt != $1.entry.feedPositionAt {
            return $0.entry.feedPositionAt > $1.entry.feedPositionAt
          }
          if $0.entry.entryId != $1.entry.entryId { return $0.entry.entryId > $1.entry.entryId }
          return $0.position < $1.position
        }
      } while candidates.count <= pageLimit && hasMore
      let entries = candidates.prefix(pageLimit).map { $0.entry }
      return .init(entries: entries, cursor: (hasMore || candidates.count > pageLimit) ? entries.last.map {
        ThinAppViewCursor.encode(createdAt: $0.feedPositionAt, uri: $0.entryId)
      } : nil)
    }
  }

  private struct RequestedFeedScope: Encodable {
    let position: Int
    let authorDid: String
    let site: String?
    let floorAt: String?
    let floorUri: String?
  }

  private static func requestedFeedScopes(
    database: Database, viewerDid: String, scopes: [PublicationUnreadScope],
    filter: EntryListFilter, now: String
  ) throws -> [RequestedFeedScope] {
    let scopedAuthors = Set(scopes.filter {
      ThinAppViewQuerySupport.requiresPublicationSiteFilter(
        publicationAtUri: $0.publicationAtUri, publicationScopeAtUris: $0.publicationScopeAtUris,
        publicationSiteUrls: $0.publicationSiteUrls)
    }.map(\.authorDid))
    let authorsJSON = String(decoding: try JSONEncoder().encode(scopedAuthors.sorted()), as: UTF8.self)
    let sites = try Row.fetchAll(database, sql: """
      SELECT DISTINCT author_did,publication_site FROM content_items
      WHERE author_did IN (SELECT value FROM json_each(?))
        AND expires_at > ? AND publication_site IS NOT NULL
      """, arguments: [authorsJSON, now])
    var sitesByIdentity: [String: [String: Set<String>]] = [:]
    for row in sites {
      let author: String = row["author_did"], site: String = row["publication_site"]
      for key in feedSiteIdentities(site) { sitesByIdentity[author, default: [:]][key, default: []].insert(site) }
    }
    let floors = filter == .all ? [] : try Row.fetchAll(database, sql: """
      SELECT publication_id,read_floor_at,read_floor_uri
      FROM appview_publication_read_floors WHERE viewer_did=?
      """, arguments: [viewerDid])
    var boundaries: [String: (String, String?)] = [:]
    for row in floors {
      let raw: String = row["read_floor_at"]
      if let date = try? ReadStateValidation.date(raw) {
        boundaries[row["publication_id"]] = (feedTimestampKey(date), row["read_floor_uri"])
      }
    }
    return scopes.enumerated().flatMap { position, scope -> [RequestedFeedScope] in
      let floor = boundaries[scope.publicationId]
      let scoped = ThinAppViewQuerySupport.requiresPublicationSiteFilter(
        publicationAtUri: scope.publicationAtUri, publicationScopeAtUris: scope.publicationScopeAtUris,
        publicationSiteUrls: scope.publicationSiteUrls)
      if !scoped {
        return [.init(position: position, authorDid: scope.authorDid, site: nil, floorAt: floor?.0, floorUri: floor?.1)]
      }
      var candidates = Set<String>()
      for key in AppViewUnreadCounterSupport.scopeKeys(
        publicationAtUri: scope.publicationAtUri, publicationScopeAtUris: scope.publicationScopeAtUris,
        publicationSiteUrls: scope.publicationSiteUrls).flatMap(feedSiteIdentities) {
        candidates.formUnion(sitesByIdentity[scope.authorDid]?[key] ?? [])
      }
      return candidates.sorted().filter {
        ThinAppViewQuerySupport.publicationSiteMatches(
          siteField: $0, publicationAtUri: scope.publicationAtUri,
          publicationScopeAtUris: scope.publicationScopeAtUris, publicationSiteUrls: scope.publicationSiteUrls)
      }.map { .init(position: position, authorDid: scope.authorDid, site: $0, floorAt: floor?.0, floorUri: floor?.1) }
    }
  }

  private static func feedSiteIdentities(_ site: String) -> [String] {
    var keys: [String] = []
    if let value = RenderFieldExtractor.canonicalPublicationAtUriKey(site) { keys.append("at:\(value)") }
    if let value = RssFeedIdentity.normalizeFeedUrl(site) { keys.append("feed:\(value)") }
    if let value = RenderFieldExtractor.normalizePublicationSiteUrl(site) { keys.append("site:\(value)") }
    return keys
  }
}
