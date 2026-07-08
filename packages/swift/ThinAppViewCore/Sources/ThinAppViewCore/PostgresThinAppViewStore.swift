import Foundation
import Logging
import PostgresNIO

public actor PostgresThinAppViewStore: ThinAppViewStore {
  private let pool: PostgresClient
  private let logger: Logger

public init(pool: PostgresClient, logger: Logger) {
    self.pool = pool
    self.logger = logger
  }

  public func upsertContentItem(_ item: IndexedContentItem) async throws {
    let renderJSON = try item.render.encodedJSON()
    try await pool.query(
      """
      INSERT INTO content_items
        (uri, cid, author_did, collection, created_at, indexed_at, publication_site, render_json, expires_at)
      VALUES
        (\(item.uri), \(item.cid), \(item.authorDid), \(item.collection), \(item.createdAt), \(item.indexedAt), \(item.publicationSite), \(renderJSON)::jsonb, \(item.expiresAt))
      ON CONFLICT (uri) DO UPDATE SET
        cid = EXCLUDED.cid,
        author_did = EXCLUDED.author_did,
        collection = EXCLUDED.collection,
        created_at = EXCLUDED.created_at,
        indexed_at = EXCLUDED.indexed_at,
        publication_site = EXCLUDED.publication_site,
        render_json = EXCLUDED.render_json,
        expires_at = EXCLUDED.expires_at
      """,
      logger: logger
    )
  }

  public func deleteContentItem(uri: String) async throws {
    try await pool.query(
      "DELETE FROM content_items WHERE uri = \(uri)",
      logger: logger
    )
  }

  public func upsertReadMark(viewerDid: String, subjectUri: String, createdAt: Date) async throws {
    try await pool.query(
      """
      INSERT INTO read_marks (viewer_did, subject_uri, created_at)
      VALUES (\(viewerDid), \(subjectUri), \(createdAt))
      ON CONFLICT (viewer_did, subject_uri) DO UPDATE SET created_at = EXCLUDED.created_at
      """,
      logger: logger
    )
  }

  public func deleteReadMark(viewerDid: String, subjectUri: String) async throws {
    try await pool.query(
      """
      DELETE FROM read_marks
      WHERE viewer_did = \(viewerDid) AND subject_uri = \(subjectUri)
      """,
      logger: logger
    )
  }

  public func purgeReadMarks(viewerDid: String) async throws {
    try await pool.query(
      "DELETE FROM read_marks WHERE viewer_did = \(viewerDid)",
      logger: logger
    )
  }

  public func fetchContentItem(uri: String) async throws -> AppViewEntryListItem? {
    let now = Date()
    let rows = try await pool.query(
      """
      SELECT ci.uri, ci.render_json::text, ci.created_at
      FROM content_items ci
      WHERE ci.uri = \(uri) AND ci.expires_at > \(now)
      LIMIT 1
      """,
      logger: logger
    )
    for try await row in rows {
      let (uri, renderJSON, createdAt) = try row.decode((String, String, Date).self)
      return ThinAppViewQuerySupport.entryListItems(from: [(uri, renderJSON, createdAt)]).first
    }
    return nil
  }

  public func fetchContentRender(uri: String) async throws -> ContentRenderFields? {
    let now = Date()
    let rows = try await pool.query(
      """
      SELECT ci.render_json::text
      FROM content_items ci
      WHERE ci.uri = \(uri) AND ci.expires_at > \(now)
      LIMIT 1
      """,
      logger: logger
    )
    let decoder = JSONDecoder()
    for try await row in rows {
      let renderJSON: String = try row.decode(String.self)
      guard let data = renderJSON.data(using: .utf8) else { return nil }
      return try? decoder.decode(ContentRenderFields.self, from: data)
    }
    return nil
  }

  public func listContentItemsForPublicationSite(
    authorDid: String,
    publicationSite: String,
    limit: Int
  ) async throws -> [(uri: String, renderJSON: String)] {
    let capped = max(1, min(limit, 2_000))
    let now = Date()
    let rows = try await pool.query(
      """
      SELECT ci.uri, ci.render_json::text
      FROM content_items ci
      WHERE ci.author_did = \(authorDid)
        AND ci.publication_site = \(publicationSite)
        AND ci.expires_at > \(now)
      ORDER BY ci.created_at DESC, ci.uri DESC
      LIMIT \(capped)
      """,
      logger: logger
    )
    var items: [(uri: String, renderJSON: String)] = []
    for try await row in rows {
      let (uri, renderJSON): (String, String) = try row.decode((String, String).self)
      items.append((uri, renderJSON))
    }
    return items
  }

  public func hasReadMark(viewerDid: String, subjectUri: String) async throws -> Bool {
    let rows = try await pool.query(
      """
      SELECT 1 AS present
      FROM read_marks
      WHERE viewer_did = \(viewerDid) AND subject_uri = \(subjectUri)
      LIMIT 1
      """,
      logger: logger
    )
    for try await _ in rows { return true }
    return false
  }

  public func listEntries(
    viewerDid: String,
    authorDid: String,
    publicationAtUri: String?,
    publicationScopeAtUris: [String],
    publicationSiteUrls: [String],
    filter: EntryListFilter,
    cursor: String?,
    limit: Int,
    readFloorAt: Date?
  ) async throws -> AppViewEntryListResponse {
    let pageLimit = max(1, min(limit, 100))
    let now = Date()
    let scoped = ThinAppViewQuerySupport.requiresPublicationSiteFilter(
      publicationAtUri: publicationAtUri,
      publicationScopeAtUris: publicationScopeAtUris,
      publicationSiteUrls: publicationSiteUrls
    )
    let batchSize = ThinAppViewQuerySupport.scanBatchSize(
      pageLimit: pageLimit,
      scoped: scoped
    )
    var dbCursor = cursor.flatMap { ThinAppViewCursor.decode($0) }

    let siteKeys = AppViewProjectionCacheScopeKeys.publicationSiteKeys(
      publicationAtUri: publicationAtUri,
      publicationScopeAtUris: publicationScopeAtUris,
      publicationSiteUrls: publicationSiteUrls
    )
    if scoped, !siteKeys.isEmpty {
      let fetched = try await fetchSiteScopedContentBatch(
        viewerDid: viewerDid,
        authorDid: authorDid,
        siteKeys: siteKeys,
        filter: filter,
        cursor: dbCursor,
        limit: pageLimit + 1,
        now: now,
        readFloorAt: readFloorAt
      )
      return ThinAppViewQuerySupport.buildFilteredEntryListPage(
        pageLimit: pageLimit,
        matches: fetched.map {
          EntryListScanRow(
            uri: $0.uri,
            renderJSON: $0.renderJSON,
            createdAt: $0.createdAt,
            publicationSite: $0.publicationSite
          )
        },
        lastScannedCreatedAt: fetched.last?.createdAt,
        lastScannedUri: fetched.last?.uri,
        dbHasMore: fetched.count > pageLimit
      )
    }

    if !scoped {
      let fetched = try await fetchContentBatch(
        viewerDid: viewerDid,
        authorDid: authorDid,
        filter: filter,
        cursor: dbCursor,
        limit: batchSize,
        now: now,
        readFloorAt: readFloorAt
      )
      return ThinAppViewQuerySupport.buildFilteredEntryListPage(
        pageLimit: pageLimit,
        matches: fetched.map {
          EntryListScanRow(
            uri: $0.uri,
            renderJSON: $0.renderJSON,
            createdAt: $0.createdAt,
            publicationSite: $0.publicationSite
          )
        },
        lastScannedCreatedAt: fetched.last?.createdAt,
        lastScannedUri: fetched.last?.uri,
        dbHasMore: fetched.count == batchSize
      )
    }

    var matches: [EntryListScanRow] = []
    var lastScannedCreatedAt: Date?
    var lastScannedUri: String?
    var dbHasMore = false

    scanLoop: while matches.count < pageLimit + 1 {
      let fetched = try await fetchContentBatch(
        viewerDid: viewerDid,
        authorDid: authorDid,
        filter: filter,
        cursor: dbCursor,
        limit: batchSize,
        now: now,
        readFloorAt: readFloorAt
      )
      if fetched.isEmpty {
        dbHasMore = false
        break
      }

      dbHasMore = fetched.count == batchSize
      for row in fetched {
        lastScannedCreatedAt = row.createdAt
        lastScannedUri = row.uri
        guard
          ThinAppViewQuerySupport.publicationSiteMatches(
            siteField: row.publicationSite,
            publicationAtUri: publicationAtUri,
            publicationScopeAtUris: publicationScopeAtUris,
            publicationSiteUrls: publicationSiteUrls
          )
        else { continue }

        matches.append(
          EntryListScanRow(
            uri: row.uri,
            renderJSON: row.renderJSON,
            createdAt: row.createdAt,
            publicationSite: row.publicationSite
          )
        )
        if matches.count >= pageLimit + 1 {
          break scanLoop
        }
      }

      if !dbHasMore { break }
      guard let last = fetched.last else { break }
      dbCursor = (last.createdAt, last.uri)
    }

    return ThinAppViewQuerySupport.buildFilteredEntryListPage(
      pageLimit: pageLimit,
      matches: matches,
      lastScannedCreatedAt: lastScannedCreatedAt,
      lastScannedUri: lastScannedUri,
      dbHasMore: dbHasMore
    )
  }

  private func fetchSiteScopedContentBatch(
    viewerDid: String,
    authorDid: String,
    siteKeys: [String],
    filter: EntryListFilter,
    cursor: (createdAt: Date, uri: String)?,
    limit: Int,
    now: Date,
    readFloorAt: Date?
  ) async throws -> [(uri: String, renderJSON: String, createdAt: Date, publicationSite: String?)] {
    let rows: PostgresRowSequence
    let unreadFloor = readFloorAt ?? Date(timeIntervalSince1970: 0)
    switch (filter, cursor) {
    case (.all, nil):
      rows = try await pool.query(
        """
        SELECT ci.uri, ci.render_json::text, ci.created_at, ci.publication_site
        FROM content_items ci
        WHERE ci.author_did = \(authorDid)
          AND ci.expires_at > \(now)
          AND ci.publication_site = ANY(\(siteKeys))
        ORDER BY ci.created_at DESC, ci.uri DESC
        LIMIT \(limit)
        """,
        logger: logger
      )
    case (.unread, nil):
      rows = try await pool.query(
        """
        SELECT ci.uri, ci.render_json::text, ci.created_at, ci.publication_site
        FROM content_items ci
        LEFT JOIN read_marks rm ON rm.viewer_did = \(viewerDid) AND rm.subject_uri = ci.uri
        WHERE ci.author_did = \(authorDid)
          AND ci.expires_at > \(now)
          AND rm.subject_uri IS NULL
          AND ci.created_at > \(unreadFloor)
          AND ci.publication_site = ANY(\(siteKeys))
        ORDER BY ci.created_at DESC, ci.uri DESC
        LIMIT \(limit)
        """,
        logger: logger
      )
    case (.read, nil):
      rows = try await pool.query(
        """
        SELECT ci.uri, ci.render_json::text, ci.created_at, ci.publication_site
        FROM content_items ci
        INNER JOIN read_marks rm ON rm.viewer_did = \(viewerDid) AND rm.subject_uri = ci.uri
        WHERE ci.author_did = \(authorDid)
          AND ci.expires_at > \(now)
          AND ci.publication_site = ANY(\(siteKeys))
        ORDER BY ci.created_at DESC, ci.uri DESC
        LIMIT \(limit)
        """,
        logger: logger
      )
    case (.all, let decoded?):
      rows = try await pool.query(
        """
        SELECT ci.uri, ci.render_json::text, ci.created_at, ci.publication_site
        FROM content_items ci
        WHERE ci.author_did = \(authorDid)
          AND ci.expires_at > \(now)
          AND ci.publication_site = ANY(\(siteKeys))
          AND (ci.created_at < \(decoded.createdAt) OR (ci.created_at = \(decoded.createdAt) AND ci.uri < \(decoded.uri)))
        ORDER BY ci.created_at DESC, ci.uri DESC
        LIMIT \(limit)
        """,
        logger: logger
      )
    case (.unread, let decoded?):
      rows = try await pool.query(
        """
        SELECT ci.uri, ci.render_json::text, ci.created_at, ci.publication_site
        FROM content_items ci
        LEFT JOIN read_marks rm ON rm.viewer_did = \(viewerDid) AND rm.subject_uri = ci.uri
        WHERE ci.author_did = \(authorDid)
          AND ci.expires_at > \(now)
          AND rm.subject_uri IS NULL
          AND ci.created_at > \(unreadFloor)
          AND ci.publication_site = ANY(\(siteKeys))
          AND (ci.created_at < \(decoded.createdAt) OR (ci.created_at = \(decoded.createdAt) AND ci.uri < \(decoded.uri)))
        ORDER BY ci.created_at DESC, ci.uri DESC
        LIMIT \(limit)
        """,
        logger: logger
      )
    case (.read, let decoded?):
      rows = try await pool.query(
        """
        SELECT ci.uri, ci.render_json::text, ci.created_at, ci.publication_site
        FROM content_items ci
        INNER JOIN read_marks rm ON rm.viewer_did = \(viewerDid) AND rm.subject_uri = ci.uri
        WHERE ci.author_did = \(authorDid)
          AND ci.expires_at > \(now)
          AND ci.publication_site = ANY(\(siteKeys))
          AND (ci.created_at < \(decoded.createdAt) OR (ci.created_at = \(decoded.createdAt) AND ci.uri < \(decoded.uri)))
        ORDER BY ci.created_at DESC, ci.uri DESC
        LIMIT \(limit)
        """,
        logger: logger
      )
    }

    var fetched: [(uri: String, renderJSON: String, createdAt: Date, publicationSite: String?)] = []
    for try await row in rows {
      let (uri, renderJSON, createdAt, publicationSite) = try row.decode(
        (String, String, Date, String?).self
      )
      fetched.append((uri, renderJSON, createdAt, publicationSite))
    }
    return fetched
  }

  private func fetchContentBatch(
    viewerDid: String,
    authorDid: String,
    filter: EntryListFilter,
    cursor: (createdAt: Date, uri: String)?,
    limit: Int,
    now: Date,
    readFloorAt: Date?
  ) async throws -> [(uri: String, renderJSON: String, createdAt: Date, publicationSite: String?)] {
    let rows: PostgresRowSequence
    let unreadFloor = readFloorAt ?? Date(timeIntervalSince1970: 0)
    switch (filter, cursor) {
    case (.all, nil):
      rows = try await pool.query(
        """
        SELECT ci.uri, ci.render_json::text, ci.created_at, ci.publication_site
        FROM content_items ci
        WHERE ci.author_did = \(authorDid) AND ci.expires_at > \(now)
        ORDER BY ci.created_at DESC, ci.uri DESC
        LIMIT \(limit)
        """,
        logger: logger
      )
    case (.unread, nil):
      rows = try await pool.query(
        """
        SELECT ci.uri, ci.render_json::text, ci.created_at, ci.publication_site
        FROM content_items ci
        LEFT JOIN read_marks rm ON rm.viewer_did = \(viewerDid) AND rm.subject_uri = ci.uri
        WHERE ci.author_did = \(authorDid) AND ci.expires_at > \(now) AND rm.subject_uri IS NULL
          AND ci.created_at > \(unreadFloor)
        ORDER BY ci.created_at DESC, ci.uri DESC
        LIMIT \(limit)
        """,
        logger: logger
      )
    case (.read, nil):
      rows = try await pool.query(
        """
        SELECT ci.uri, ci.render_json::text, ci.created_at, ci.publication_site
        FROM content_items ci
        INNER JOIN read_marks rm ON rm.viewer_did = \(viewerDid) AND rm.subject_uri = ci.uri
        WHERE ci.author_did = \(authorDid) AND ci.expires_at > \(now)
        ORDER BY ci.created_at DESC, ci.uri DESC
        LIMIT \(limit)
        """,
        logger: logger
      )
    case (.all, let decoded?):
      rows = try await pool.query(
        """
        SELECT ci.uri, ci.render_json::text, ci.created_at, ci.publication_site
        FROM content_items ci
        WHERE ci.author_did = \(authorDid) AND ci.expires_at > \(now)
          AND (ci.created_at < \(decoded.createdAt) OR (ci.created_at = \(decoded.createdAt) AND ci.uri < \(decoded.uri)))
        ORDER BY ci.created_at DESC, ci.uri DESC
        LIMIT \(limit)
        """,
        logger: logger
      )
    case (.unread, let decoded?):
      rows = try await pool.query(
        """
        SELECT ci.uri, ci.render_json::text, ci.created_at, ci.publication_site
        FROM content_items ci
        LEFT JOIN read_marks rm ON rm.viewer_did = \(viewerDid) AND rm.subject_uri = ci.uri
        WHERE ci.author_did = \(authorDid) AND ci.expires_at > \(now) AND rm.subject_uri IS NULL
          AND ci.created_at > \(unreadFloor)
          AND (ci.created_at < \(decoded.createdAt) OR (ci.created_at = \(decoded.createdAt) AND ci.uri < \(decoded.uri)))
        ORDER BY ci.created_at DESC, ci.uri DESC
        LIMIT \(limit)
        """,
        logger: logger
      )
    case (.read, let decoded?):
      rows = try await pool.query(
        """
        SELECT ci.uri, ci.render_json::text, ci.created_at, ci.publication_site
        FROM content_items ci
        INNER JOIN read_marks rm ON rm.viewer_did = \(viewerDid) AND rm.subject_uri = ci.uri
        WHERE ci.author_did = \(authorDid) AND ci.expires_at > \(now)
          AND (ci.created_at < \(decoded.createdAt) OR (ci.created_at = \(decoded.createdAt) AND ci.uri < \(decoded.uri)))
        ORDER BY ci.created_at DESC, ci.uri DESC
        LIMIT \(limit)
        """,
        logger: logger
      )
    }

    var fetched: [(uri: String, renderJSON: String, createdAt: Date, publicationSite: String?)] = []
    for try await row in rows {
      let (uri, renderJSON, createdAt, publicationSite) = try row.decode(
        (String, String, Date, String?).self
      )
      fetched.append((uri, renderJSON, createdAt, publicationSite))
    }
    return fetched
  }

  public func countUnreadEntries(
    viewerDid: String,
    authorDid: String,
    publicationAtUri: String?,
    publicationScopeAtUris: [String],
    publicationSiteUrls: [String]
  ) async throws -> Int {
    let now = Date()
    let scoped = ThinAppViewQuerySupport.requiresPublicationSiteFilter(
      publicationAtUri: publicationAtUri,
      publicationScopeAtUris: publicationScopeAtUris,
      publicationSiteUrls: publicationSiteUrls
    )

    if !scoped {
      let rows = try await pool.query(
        """
        SELECT COUNT(*)::int
        FROM content_items ci
        LEFT JOIN read_marks rm ON rm.viewer_did = \(viewerDid) AND rm.subject_uri = ci.uri
        WHERE ci.author_did = \(authorDid) AND ci.expires_at > \(now) AND rm.subject_uri IS NULL
        """,
        logger: logger
      )
      for try await row in rows {
        return try row.decode(Int.self)
      }
      return 0
    }

    let siteKeys = AppViewProjectionCacheScopeKeys.publicationSiteKeys(
      publicationAtUri: publicationAtUri,
      publicationScopeAtUris: publicationScopeAtUris,
      publicationSiteUrls: publicationSiteUrls
    )
    if !siteKeys.isEmpty {
      let rows = try await pool.query(
        """
        SELECT COUNT(*)::int
        FROM content_items ci
        LEFT JOIN read_marks rm ON rm.viewer_did = \(viewerDid) AND rm.subject_uri = ci.uri
        WHERE ci.author_did = \(authorDid)
          AND ci.expires_at > \(now)
          AND rm.subject_uri IS NULL
          AND ci.publication_site = ANY(\(siteKeys))
        """,
        logger: logger
      )
      for try await row in rows {
        return try row.decode(Int.self)
      }
      return 0
    }

    let rows = try await pool.query(
      """
      SELECT ci.publication_site
      FROM content_items ci
      LEFT JOIN read_marks rm ON rm.viewer_did = \(viewerDid) AND rm.subject_uri = ci.uri
      WHERE ci.author_did = \(authorDid) AND ci.expires_at > \(now) AND rm.subject_uri IS NULL
      """,
      logger: logger
    )
    var siteFields: [String?] = []
    for try await row in rows {
      let site: String? = try row.decode(String?.self)
      siteFields.append(site)
    }
    return ThinAppViewQuerySupport.countMatchingPublicationSites(
      siteFields: siteFields,
      publicationAtUri: publicationAtUri,
      publicationScopeAtUris: publicationScopeAtUris,
      publicationSiteUrls: publicationSiteUrls
    )
  }

  public func countUnreadEntriesBatch(
    viewerDid: String,
    scopes: [PublicationUnreadScope]
  ) async throws -> [String: Int] {
    guard !scopes.isEmpty else { return [:] }

    let authorDids = Array(Set(scopes.map(\.authorDid)))
    let now = Date()
    let rows = try await pool.query(
      """
      SELECT ci.author_did, ci.publication_site, COUNT(*)::int
      FROM content_items ci
      LEFT JOIN read_marks rm ON rm.viewer_did = \(viewerDid) AND rm.subject_uri = ci.uri
      WHERE ci.author_did = ANY(\(authorDids))
        AND ci.expires_at > \(now)
        AND rm.subject_uri IS NULL
      GROUP BY ci.author_did, ci.publication_site
      """,
      logger: logger
    )

    var unreadSiteCountsByAuthor: [String: [UnreadSiteCount]] = Dictionary(
      uniqueKeysWithValues: authorDids.map { ($0, []) }
    )
    for try await row in rows {
      let (authorDid, site, count): (String, String?, Int) = try row.decode((String, String?, Int).self)
      unreadSiteCountsByAuthor[authorDid, default: []].append(
        UnreadSiteCount(site: site, count: count)
      )
    }

    return ThinAppViewQuerySupport.batchUnreadCounts(
      scopes: scopes,
      unreadSiteCountsByAuthor: unreadSiteCountsByAuthor
    )
  }

  public func upsertPublicationScopes(_ scopes: [AppViewPublicationScope]) async throws {
    guard !scopes.isEmpty else { return }
    for scope in scopes {
      let publicationScopeAtUrisJSON = try Self.jsonString(scope.publicationScopeAtUris)
      let publicationSiteUrlsJSON = try Self.jsonString(scope.publicationSiteUrls)
      let scopeKeysJSON = try Self.jsonString(scope.scopeKeys)
      let sectionKeysJSON = try Self.jsonString(scope.sectionKeys)
      try await pool.query(
        """
        INSERT INTO appview_publication_scopes
          (viewer_did, publication_id, author_did, publication_at_uri,
           publication_scope_at_uris, publication_site_urls, scope_keys,
           section_keys, updated_at)
        VALUES
          (\(scope.viewerDid), \(scope.publicationId), \(scope.authorDid), \(scope.publicationAtUri),
           \(publicationScopeAtUrisJSON)::jsonb, \(publicationSiteUrlsJSON)::jsonb,
           \(scopeKeysJSON)::jsonb, \(sectionKeysJSON)::jsonb, \(scope.updatedAt))
        ON CONFLICT (viewer_did, publication_id)
        DO UPDATE SET
          author_did = EXCLUDED.author_did,
          publication_at_uri = EXCLUDED.publication_at_uri,
          publication_scope_at_uris = EXCLUDED.publication_scope_at_uris,
          publication_site_urls = EXCLUDED.publication_site_urls,
          scope_keys = EXCLUDED.scope_keys,
          section_keys = EXCLUDED.section_keys,
          updated_at = EXCLUDED.updated_at
        """,
        logger: logger
      )
    }
  }

  public func replacePublicationScopes(
    viewerDid: String,
    scopes: [AppViewPublicationScope]
  ) async throws {
    try await pool.query(
      "DELETE FROM appview_publication_scopes WHERE viewer_did = \(viewerDid)",
      logger: logger
    )
    try await upsertPublicationScopes(scopes)
  }

  public func fetchUnreadCounters(
    viewerDid: String,
    publicationIds: [String]?
  ) async throws -> [AppViewUnreadCounter] {
    let rows: PostgresRowSequence
    if let publicationIds, !publicationIds.isEmpty {
      rows = try await pool.query(
        """
        SELECT publication_id, unread_count, generation, accuracy, dirty, counted_at
        FROM appview_unread_counters
        WHERE viewer_did = \(viewerDid)
          AND publication_id = ANY(\(publicationIds))
        """,
        logger: logger
      )
    } else {
      rows = try await pool.query(
        """
        SELECT publication_id, unread_count, generation, accuracy, dirty, counted_at
        FROM appview_unread_counters
        WHERE viewer_did = \(viewerDid)
        """,
        logger: logger
      )
    }
    var counters: [AppViewUnreadCounter] = []
    for try await row in rows {
      if let counter = try Self.unreadCounter(from: row) {
        counters.append(counter)
      }
    }
    return counters
  }

  public func refreshUnreadCounters(
    viewerDid: String,
    scopes: [PublicationUnreadScope]
  ) async throws -> [AppViewUnreadCounter] {
    guard !scopes.isEmpty else { return [] }
    let exactCounts = try await countUnreadEntriesBatch(viewerDid: viewerDid, scopes: scopes)
    let floors = try await readFloors(viewerDid: viewerDid, publicationIds: scopes.map(\.publicationId))
    let countedAt = Date()
    let generation = AppViewUnreadCounterSupport.generation(for: countedAt)
    var counters: [AppViewUnreadCounter] = []

    for scope in scopes {
      let count: Int
      if let floor = floors[scope.publicationId] {
        count = try await countUnreadEntriesAfterFloor(
          viewerDid: viewerDid,
          scope: scope,
          readFloorAt: floor
        )
      } else {
        count = exactCounts[scope.publicationId] ?? 0
      }
      let counter = AppViewUnreadCounter(
        publicationId: scope.publicationId,
        unreadCount: count,
        generation: generation,
        accuracy: .exact,
        dirty: false,
        countedAt: countedAt
      )
      try await upsertUnreadCounter(counter, viewerDid: viewerDid)
      counters.append(counter)
    }
    return counters
  }

  public func incrementUnreadCountersForContentItem(_ item: IndexedContentItem) async throws {
    let scopes = try await publicationScopes(authorDid: item.authorDid, viewerDid: nil)
      .filter {
        AppViewUnreadCounterSupport.contentMatchesScope(
          authorDid: item.authorDid,
          publicationSite: item.publicationSite,
          scope: $0
        )
      }
    guard !scopes.isEmpty else { return }
    let generation = AppViewUnreadCounterSupport.generation()
    let countedAt = Date()
    for scope in scopes {
      if let floor = try await readFloor(viewerDid: scope.viewerDid, publicationId: scope.publicationId),
         item.createdAt <= floor
      {
        continue
      }
      let alreadyRead = try await hasReadMark(viewerDid: scope.viewerDid, subjectUri: item.uri)
      guard !alreadyRead else { continue }
      try await adjustUnreadCounter(
        viewerDid: scope.viewerDid,
        publicationId: scope.publicationId,
        delta: 1,
        generation: generation,
        countedAt: countedAt
      )
    }
  }

  public func markUnreadCountersDirtyForContent(authorDid: String, publicationSite: String?) async throws {
    let scopes = try await publicationScopes(authorDid: authorDid, viewerDid: nil)
      .filter {
        AppViewUnreadCounterSupport.contentMatchesScope(
          authorDid: authorDid,
          publicationSite: publicationSite,
          scope: $0
        )
      }
    guard !scopes.isEmpty else { return }
    let generation = AppViewUnreadCounterSupport.generation()
    let countedAt = Date()
    for scope in scopes {
      try await markUnreadCounterDirty(
        viewerDid: scope.viewerDid,
        publicationId: scope.publicationId,
        generation: generation,
        countedAt: countedAt
      )
    }
  }

  public func adjustUnreadCountersForReadState(
    viewerDid: String,
    subjectUri: String,
    delta: Int
  ) async throws {
    guard delta != 0 else { return }
    guard let content = try await contentCounterFields(uri: subjectUri) else { return }
    let scopes = try await publicationScopes(authorDid: content.authorDid, viewerDid: viewerDid)
      .filter {
        AppViewUnreadCounterSupport.contentMatchesScope(
          authorDid: content.authorDid,
          publicationSite: content.publicationSite,
          scope: $0
        )
      }
    guard !scopes.isEmpty else { return }
    let generation = AppViewUnreadCounterSupport.generation()
    let countedAt = Date()
    for scope in scopes {
      if let floor = try await readFloor(viewerDid: viewerDid, publicationId: scope.publicationId),
         content.createdAt <= floor
      {
        continue
      }
      try await adjustUnreadCounter(
        viewerDid: viewerDid,
        publicationId: scope.publicationId,
        delta: delta,
        generation: generation,
        countedAt: countedAt
      )
    }
  }

  public func markAllReadCounters(
    viewerDid: String,
    publicationIds: [String],
    readAt: Date
  ) async throws -> [AppViewUnreadCounter] {
    let uniqueIds = Array(Set(publicationIds)).sorted()
    guard !uniqueIds.isEmpty else { return [] }
    let generation = AppViewUnreadCounterSupport.generation(for: readAt)
    var counters: [AppViewUnreadCounter] = []
    for publicationId in uniqueIds {
      try await pool.query(
        """
        INSERT INTO appview_publication_read_floors
          (viewer_did, publication_id, read_floor_at, generation, updated_at)
        VALUES
          (\(viewerDid), \(publicationId), \(readAt), \(generation), \(readAt))
        ON CONFLICT (viewer_did, publication_id)
        DO UPDATE SET
          read_floor_at = EXCLUDED.read_floor_at,
          generation = EXCLUDED.generation,
          updated_at = EXCLUDED.updated_at
        """,
        logger: logger
      )
      let counter = AppViewUnreadCounter(
        publicationId: publicationId,
        unreadCount: 0,
        generation: generation,
        accuracy: .estimated,
        dirty: true,
        countedAt: readAt
      )
      try await upsertUnreadCounter(counter, viewerDid: viewerDid)
      counters.append(counter)
    }
    return counters
  }

  public func deleteExpiredContent(before: Date) async throws -> Int {
    let rows = try await pool.query(
      "DELETE FROM content_items WHERE expires_at <= \(before) RETURNING uri",
      logger: logger
    )
    var count = 0
    for try await _ in rows { count += 1 }
    return count
  }

  public func deleteExpiredReadMarks(before: Date) async throws -> Int {
    let rows = try await pool.query(
      "DELETE FROM read_marks WHERE created_at <= \(before) RETURNING subject_uri",
      logger: logger
    )
    var count = 0
    for try await _ in rows { count += 1 }
    return count
  }

  public func recordIngestionCheckpoint(
    source: String,
    repoDid: String,
    collection: String,
    cursor: String?,
    eventTime: Date?,
    observedAt: Date
  ) async throws {
    try await pool.query(
      """
      INSERT INTO appview_ingestion_checkpoints
        (source, repo_did, collection, cursor, event_time, observed_at)
      VALUES
        (\(source), \(repoDid), \(collection), \(cursor), \(eventTime), \(observedAt))
      ON CONFLICT (source, repo_did, collection)
      DO UPDATE SET
        cursor = EXCLUDED.cursor,
        event_time = EXCLUDED.event_time,
        observed_at = EXCLUDED.observed_at
      """,
      logger: logger
    )
  }

  public func listAuthorDidsForProactiveBackfill(limit: Int) async throws -> [String] {
    let capped = max(1, min(limit, 500))
    let rows = try await pool.query(
      """
      SELECT author_did
      FROM content_items
      WHERE author_did LIKE 'did:%' AND author_did NOT LIKE 'did:web:%'
      GROUP BY author_did
      ORDER BY MAX(indexed_at) ASC
      LIMIT \(capped)
      """,
      logger: logger
    )
    var authorDids: [String] = []
    for try await row in rows {
      let did: String = try row.decode(String.self)
      authorDids.append(did)
    }
    return authorDids
  }

  public func listRssPublicationSites(limit: Int) async throws -> [String] {
    let capped = max(1, min(limit, 200))
    let now = Date()
    let rows = try await pool.query(
      """
      SELECT publication_site
      FROM content_items
      WHERE author_did = \(RssFeedLexicons.rssAuthorDid)
        AND publication_site IS NOT NULL
        AND expires_at > \(now)
      GROUP BY publication_site
      ORDER BY MIN(indexed_at) ASC
      LIMIT \(capped)
      """,
      logger: logger
    )
    var sites: [String] = []
    for try await row in rows {
      let site: String = try row.decode(String.self)
      sites.append(site)
    }
    return sites
  }

  public func fetchRssFeedMetadata(normalizedFeedUrl: String) async throws -> RssFeedFetchMetadata? {
    let rows = try await pool.query(
      """
      SELECT etag, last_modified, last_poll_at, backoff_until, consecutive_error_count
      FROM rss_feed_fetch_metadata
      WHERE feed_url = \(normalizedFeedUrl)
      LIMIT 1
      """,
      logger: logger
    )
    for try await row in rows {
      let (etag, lastModified, lastPollAt, backoffUntil, consecutiveErrorCount) = try row.decode(
        (String?, String?, Date?, Date?, Int).self
      )
      return RssFeedFetchMetadata(
        normalizedFeedUrl: normalizedFeedUrl,
        etag: etag,
        lastModified: lastModified,
        lastPollAt: lastPollAt,
        backoffUntil: backoffUntil,
        consecutiveErrorCount: consecutiveErrorCount
      )
    }
    return nil
  }

  public func storeRssFeedMetadata(_ metadata: RssFeedFetchMetadata) async throws {
    try await pool.query(
      """
      INSERT INTO rss_feed_fetch_metadata
        (feed_url, etag, last_modified, last_poll_at, backoff_until, consecutive_error_count)
      VALUES
        (\(metadata.normalizedFeedUrl), \(metadata.etag), \(metadata.lastModified), \(metadata.lastPollAt), \(metadata.backoffUntil), \(metadata.consecutiveErrorCount))
      ON CONFLICT (feed_url)
      DO UPDATE SET
        etag = EXCLUDED.etag,
        last_modified = EXCLUDED.last_modified,
        last_poll_at = EXCLUDED.last_poll_at,
        backoff_until = EXCLUDED.backoff_until,
        consecutive_error_count = EXCLUDED.consecutive_error_count
      """,
      logger: logger
    )
  }

  private func publicationScopes(
    authorDid: String,
    viewerDid: String?
  ) async throws -> [AppViewPublicationScope] {
    let rows: PostgresRowSequence
    if let viewerDid {
      rows = try await pool.query(
        """
        SELECT viewer_did, publication_id, author_did, publication_at_uri,
               publication_scope_at_uris::text, publication_site_urls::text,
               scope_keys::text, section_keys::text, updated_at
        FROM appview_publication_scopes
        WHERE author_did = \(authorDid)
          AND viewer_did = \(viewerDid)
        """,
        logger: logger
      )
    } else {
      rows = try await pool.query(
        """
        SELECT viewer_did, publication_id, author_did, publication_at_uri,
               publication_scope_at_uris::text, publication_site_urls::text,
               scope_keys::text, section_keys::text, updated_at
        FROM appview_publication_scopes
        WHERE author_did = \(authorDid)
        """,
        logger: logger
      )
    }
    var scopes: [AppViewPublicationScope] = []
    for try await row in rows {
      if let scope = try Self.publicationScope(from: row) {
        scopes.append(scope)
      }
    }
    return scopes
  }

  private func readFloors(
    viewerDid: String,
    publicationIds: [String]
  ) async throws -> [String: Date] {
    let uniqueIds = Array(Set(publicationIds)).sorted()
    guard !uniqueIds.isEmpty else { return [:] }
    let rows = try await pool.query(
      """
      SELECT publication_id, read_floor_at
      FROM appview_publication_read_floors
      WHERE viewer_did = \(viewerDid)
        AND publication_id = ANY(\(uniqueIds))
      """,
      logger: logger
    )
    var floors: [String: Date] = [:]
    for try await row in rows {
      let (publicationId, readFloorAt): (String, Date) = try row.decode((String, Date).self)
      floors[publicationId] = readFloorAt
    }
    return floors
  }

  public func readFloor(viewerDid: String, publicationId: String) async throws -> Date? {
    let rows = try await pool.query(
      """
      SELECT read_floor_at
      FROM appview_publication_read_floors
      WHERE viewer_did = \(viewerDid)
        AND publication_id = \(publicationId)
      LIMIT 1
      """,
      logger: logger
    )
    for try await row in rows {
      return try row.decode(Date.self)
    }
    return nil
  }

  private func countUnreadEntriesAfterFloor(
    viewerDid: String,
    scope: PublicationUnreadScope,
    readFloorAt: Date
  ) async throws -> Int {
    let now = Date()
    let scoped = ThinAppViewQuerySupport.requiresPublicationSiteFilter(
      publicationAtUri: scope.publicationAtUri,
      publicationScopeAtUris: scope.publicationScopeAtUris,
      publicationSiteUrls: scope.publicationSiteUrls
    )
    let siteKeys = AppViewProjectionCacheScopeKeys.publicationSiteKeys(
      publicationAtUri: scope.publicationAtUri,
      publicationScopeAtUris: scope.publicationScopeAtUris,
      publicationSiteUrls: scope.publicationSiteUrls
    )
    if scoped, !siteKeys.isEmpty {
      let rows = try await pool.query(
        """
        SELECT COUNT(*)::int
        FROM content_items ci
        LEFT JOIN read_marks rm ON rm.viewer_did = \(viewerDid) AND rm.subject_uri = ci.uri
        WHERE ci.author_did = \(scope.authorDid)
          AND ci.expires_at > \(now)
          AND ci.created_at > \(readFloorAt)
          AND ci.publication_site = ANY(\(siteKeys))
          AND rm.subject_uri IS NULL
        """,
        logger: logger
      )
      for try await row in rows {
        return try row.decode(Int.self)
      }
      return 0
    }

    let rows = try await pool.query(
      """
      SELECT ci.publication_site
      FROM content_items ci
      LEFT JOIN read_marks rm ON rm.viewer_did = \(viewerDid) AND rm.subject_uri = ci.uri
      WHERE ci.author_did = \(scope.authorDid)
        AND ci.expires_at > \(now)
        AND ci.created_at > \(readFloorAt)
        AND rm.subject_uri IS NULL
      """,
      logger: logger
    )
    var siteFields: [String?] = []
    for try await row in rows {
      let site: String? = try row.decode(String?.self)
      siteFields.append(site)
    }
    return scoped
      ? ThinAppViewQuerySupport.countMatchingPublicationSites(
        siteFields: siteFields,
        publicationAtUri: scope.publicationAtUri,
        publicationScopeAtUris: scope.publicationScopeAtUris,
        publicationSiteUrls: scope.publicationSiteUrls
      )
      : siteFields.count
  }

  private func contentCounterFields(
    uri: String
  ) async throws -> (authorDid: String, publicationSite: String?, createdAt: Date)? {
    let rows = try await pool.query(
      """
      SELECT author_did, publication_site, created_at
      FROM content_items
      WHERE uri = \(uri)
      LIMIT 1
      """,
      logger: logger
    )
    for try await row in rows {
      return try row.decode((String, String?, Date).self)
    }
    return nil
  }

  private func upsertUnreadCounter(
    _ counter: AppViewUnreadCounter,
    viewerDid: String
  ) async throws {
    try await pool.query(
      """
      INSERT INTO appview_unread_counters
        (viewer_did, publication_id, unread_count, generation, accuracy, dirty, counted_at)
      VALUES
        (\(viewerDid), \(counter.publicationId), \(counter.unreadCount), \(counter.generation), \(counter.accuracy.rawValue), \(counter.dirty), \(counter.countedAt))
      ON CONFLICT (viewer_did, publication_id)
      DO UPDATE SET
        unread_count = EXCLUDED.unread_count,
        generation = EXCLUDED.generation,
        accuracy = EXCLUDED.accuracy,
        dirty = EXCLUDED.dirty,
        counted_at = EXCLUDED.counted_at
      """,
      logger: logger
    )
  }

  private func adjustUnreadCounter(
    viewerDid: String,
    publicationId: String,
    delta: Int,
    generation: Int64,
    countedAt: Date
  ) async throws {
    let current = try await fetchUnreadCounters(
      viewerDid: viewerDid,
      publicationIds: [publicationId]
    ).first?.unreadCount ?? 0
    let counter = AppViewUnreadCounter(
      publicationId: publicationId,
      unreadCount: max(0, current + delta),
      generation: generation,
      accuracy: .estimated,
      dirty: true,
      countedAt: countedAt
    )
    try await upsertUnreadCounter(counter, viewerDid: viewerDid)
  }

  private func markUnreadCounterDirty(
    viewerDid: String,
    publicationId: String,
    generation: Int64,
    countedAt: Date
  ) async throws {
    let current = try await fetchUnreadCounters(
      viewerDid: viewerDid,
      publicationIds: [publicationId]
    ).first?.unreadCount ?? 0
    let counter = AppViewUnreadCounter(
      publicationId: publicationId,
      unreadCount: current,
      generation: generation,
      accuracy: .estimated,
      dirty: true,
      countedAt: countedAt
    )
    try await upsertUnreadCounter(counter, viewerDid: viewerDid)
  }

  private static func publicationScope(from row: PostgresRow) throws -> AppViewPublicationScope? {
    let (
      viewerDid,
      publicationId,
      authorDid,
      publicationAtUri,
      publicationScopeAtUrisJSON,
      publicationSiteUrlsJSON,
      scopeKeysJSON,
      sectionKeysJSON,
      updatedAt
    ) = try row.decode((String, String, String, String?, String, String, String, String, Date).self)
    return AppViewPublicationScope(
      viewerDid: viewerDid,
      publicationId: publicationId,
      authorDid: authorDid,
      publicationAtUri: publicationAtUri,
      publicationScopeAtUris: try stringArray(fromJSON: publicationScopeAtUrisJSON),
      publicationSiteUrls: try stringArray(fromJSON: publicationSiteUrlsJSON),
      scopeKeys: try stringArray(fromJSON: scopeKeysJSON),
      sectionKeys: try stringArray(fromJSON: sectionKeysJSON),
      updatedAt: updatedAt
    )
  }

  private static func unreadCounter(from row: PostgresRow) throws -> AppViewUnreadCounter? {
    let (publicationId, unreadCount, generation, accuracyRaw, dirty, countedAt) = try row.decode(
      (String, Int, Int64, String, Bool, Date).self
    )
    guard let accuracy = AppViewUnreadCounterAccuracy(rawValue: accuracyRaw) else { return nil }
    return AppViewUnreadCounter(
      publicationId: publicationId,
      unreadCount: unreadCount,
      generation: generation,
      accuracy: accuracy,
      dirty: dirty,
      countedAt: countedAt
    )
  }

  private static func jsonString(_ values: [String]) throws -> String {
    let data = try JSONEncoder().encode(values)
    guard let string = String(data: data, encoding: .utf8) else {
      throw ThinAppViewStoreError.encodingFailed
    }
    return string
  }

  private static func stringArray(fromJSON raw: String) throws -> [String] {
    guard let data = raw.data(using: .utf8) else {
      throw ThinAppViewStoreError.encodingFailed
    }
    return try JSONDecoder().decode([String].self, from: data)
  }
}
