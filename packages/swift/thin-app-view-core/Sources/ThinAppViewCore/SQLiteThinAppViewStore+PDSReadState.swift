import Crypto
import Foundation
import GRDB
import ReadStateCore

extension SQLiteThinAppViewStore: PDSReadStateLifecycleStoring {
  static func migratePDSReadState(_ database: Database) throws {
    try database.execute(
      sql: """
        CREATE TABLE IF NOT EXISTS appview_pds_read_state_authority(
          viewer_did TEXT PRIMARY KEY, legacy_revision INTEGER NOT NULL DEFAULT 0,
          manifest TEXT, manifest_cid TEXT, projection_ready INTEGER NOT NULL DEFAULT 1,
          last_accessed_at REAL NOT NULL DEFAULT 0);
        CREATE TABLE IF NOT EXISTS appview_pds_projection_write(singleton INTEGER PRIMARY KEY CHECK(singleton=1), active INTEGER NOT NULL);
        INSERT OR IGNORE INTO appview_pds_projection_write VALUES(1,0);
        CREATE TABLE IF NOT EXISTS appview_pds_read_state_exact(
          viewer_did TEXT NOT NULL, subject_uri TEXT NOT NULL, sequence INTEGER NOT NULL,
          is_read INTEGER NOT NULL, acted_at TEXT NOT NULL, PRIMARY KEY(viewer_did,subject_uri));
        CREATE TABLE IF NOT EXISTS appview_pds_read_state_boundaries(
          viewer_did TEXT NOT NULL, sequence INTEGER NOT NULL, rule_key TEXT NOT NULL,
          is_read INTEGER NOT NULL, acted_at TEXT NOT NULL, author_did TEXT NOT NULL,
          scope_keys TEXT NOT NULL, boundary_at TEXT NOT NULL, boundary_uri TEXT,
          PRIMARY KEY(viewer_did,sequence,rule_key));
        CREATE INDEX IF NOT EXISTS appview_pds_boundary_lookup ON appview_pds_read_state_boundaries(viewer_did,author_did,boundary_at);
        """)
    // Existing query paths consume a materialized effective projection. Keep it
    // inside the same transaction as manifests and newly indexed content.
    for table in ["read_marks", "appview_unread_overrides", "appview_publication_read_floors"] {
      for operation in ["INSERT", "UPDATE", "DELETE"] {
        let row = operation == "DELETE" ? "OLD" : "NEW"
        try database.execute(
          sql: """
            CREATE TRIGGER IF NOT EXISTS pds_fence_\(table)_\(operation) BEFORE \(operation) ON \(table)
            WHEN (SELECT active FROM appview_pds_projection_write WHERE singleton=1)=0
              AND EXISTS(SELECT 1 FROM appview_pds_read_state_authority WHERE viewer_did=\(row).viewer_did AND manifest_cid IS NOT NULL)
            BEGIN SELECT RAISE(ABORT,'PDSReadStateAlreadyMigrated'); END;
            CREATE TRIGGER IF NOT EXISTS pds_revision_\(table)_\(operation) AFTER \(operation) ON \(table)
            WHEN (SELECT active FROM appview_pds_projection_write WHERE singleton=1)=0
            BEGIN
              INSERT INTO appview_pds_read_state_authority(viewer_did,legacy_revision) VALUES(\(row).viewer_did,1)
              ON CONFLICT(viewer_did) DO UPDATE SET legacy_revision=legacy_revision+1;
            END;
            """)
      }
    }
    for operation in ["INSERT", "UPDATE", "DELETE"] {
      let row = operation == "DELETE" ? "OLD" : "NEW"
      try database.execute(
        sql: """
          CREATE TRIGGER IF NOT EXISTS pds_scope_revision_\(operation) AFTER \(operation) ON appview_publication_scopes
          WHEN (SELECT active FROM appview_pds_projection_write WHERE singleton=1)=0
          AND NOT EXISTS(SELECT 1 FROM appview_pds_read_state_authority WHERE viewer_did=\(row).viewer_did AND manifest_cid IS NOT NULL)
          BEGIN
            INSERT INTO appview_pds_read_state_authority(viewer_did,legacy_revision) VALUES(\(row).viewer_did,1)
            ON CONFLICT(viewer_did) DO UPDATE SET legacy_revision=legacy_revision+1;
          END;
          """)
    }
    try database.execute(sql: """
      CREATE TRIGGER IF NOT EXISTS pds_content_DELETE AFTER DELETE ON content_items
      WHEN (SELECT active FROM appview_pds_projection_write WHERE singleton=1)=0
      BEGIN
        UPDATE appview_pds_projection_write SET active=1 WHERE singleton=1;
        DELETE FROM read_marks WHERE subject_uri=OLD.uri AND viewer_did IN
          (SELECT viewer_did FROM appview_pds_read_state_authority WHERE manifest_cid IS NOT NULL);
        INSERT INTO read_marks(viewer_did,subject_uri,created_at)
        SELECT e.viewer_did,e.subject_uri,e.acted_at FROM appview_pds_read_state_exact e
          JOIN appview_pds_read_state_authority a ON a.viewer_did=e.viewer_did
        WHERE e.subject_uri=OLD.uri AND e.is_read=1 AND a.projection_ready=1;
        UPDATE appview_pds_projection_write SET active=0 WHERE singleton=1;
      END;
      """)
    for operation in ["INSERT", "UPDATE"] {
      try database.execute(
        sql: """
          CREATE TRIGGER IF NOT EXISTS pds_content_\(operation) AFTER \(operation) ON content_items
          WHEN (SELECT active FROM appview_pds_projection_write WHERE singleton=1)=0
          BEGIN
            UPDATE appview_pds_projection_write SET active=1 WHERE singleton=1;
            DELETE FROM read_marks WHERE subject_uri=NEW.uri AND viewer_did IN
              (SELECT viewer_did FROM appview_pds_read_state_authority WHERE manifest_cid IS NOT NULL);
            INSERT INTO read_marks(viewer_did,subject_uri,created_at)
            SELECT a.viewer_did,NEW.uri,COALESCE((SELECT acted_at FROM appview_pds_read_state_exact e WHERE e.viewer_did=a.viewer_did AND e.subject_uri=NEW.uri),NEW.created_at)
            FROM appview_pds_read_state_authority a
            WHERE a.manifest_cid IS NOT NULL AND a.projection_ready=1 AND \(pdsEffectiveReadSQL(viewer: "a.viewer_did", uri: "NEW.uri", author: "NEW.author_did", site: "NEW.publication_site", date: "NEW.created_at"));
            UPDATE appview_unread_counters SET dirty=1 WHERE viewer_did IN
              (SELECT viewer_did FROM appview_pds_read_state_authority WHERE manifest_cid IS NOT NULL);
            UPDATE appview_pds_projection_write SET active=0 WHERE singleton=1;
          END;
          """)
    }
  }

  private static func pdsEffectiveReadSQL(
    viewer: String, uri: String, author: String, site: String, date: String
  ) -> String {
    """
    COALESCE((SELECT state FROM (
      SELECT e.is_read AS state,e.sequence FROM appview_pds_read_state_exact e
      WHERE e.viewer_did=\(viewer) AND e.subject_uri=\(uri)
      UNION ALL
      SELECT b.is_read,b.sequence FROM appview_pds_read_state_boundaries b
      WHERE b.viewer_did=\(viewer) AND b.author_did=\(author)
        AND (json_array_length(b.scope_keys)=0 OR EXISTS(SELECT 1 FROM json_each(b.scope_keys) WHERE value=\(site)))
        AND (tsw_timestamp_us(\(date))<tsw_timestamp_us(b.boundary_at) OR (tsw_timestamp_us(\(date))=tsw_timestamp_us(b.boundary_at) AND (b.boundary_uri IS NULL OR \(uri)<=b.boundary_uri)))
      ) ORDER BY sequence DESC LIMIT 1),0)=1
    """
  }

  static func pdsTimestamp(_ date: Date) -> String {
    ThinAppViewCursor.timestamp(date)
  }

  static func requireLegacyReadState(_ database: Database, viewer: String) throws {
    if try pdsStatus(database, viewer: viewer).authority == .pds {
      throw PDSReadStateStorageError.alreadyMigrated
    }
  }

  private static func pdsStatus(_ database: Database, viewer: String) throws -> PDSReadStateStatus {
    guard
      let row = try Row.fetchOne(
        database, sql: "SELECT * FROM appview_pds_read_state_authority WHERE viewer_did=?",
        arguments: [viewer])
    else {
      return PDSReadStateStatus(authority: .appview, migrationState: .notStarted, legacyRevision: 0)
    }
    let cid: String? = row["manifest_cid"]
    let manifest: String? = row["manifest"]
    return PDSReadStateStatus(
      authority: cid == nil ? .appview : .pds,
      migrationState: cid == nil ? .notStarted : .verified, legacyRevision: row["legacy_revision"],
      manifest: try manifest.map {
        try JSONDecoder().decode(ReadStateManifest.self, from: Data($0.utf8))
      },
      manifestCid: cid, projectionReady: row["projection_ready"])
  }

  public func pdsReadStateStatus(viewerDid: String) async throws -> PDSReadStateStatus {
    try await db.read { try Self.pdsStatus($0, viewer: viewerDid) }
  }

  private static func legacyRows(_ database: Database, viewer: String) throws
    -> [ReadStateLegacyRow]
  {
    let rows = try Row.fetchAll(
      database,
      sql: """
        SELECT 0 AS phase,f.publication_id AS key,f.updated_at AS acted_at,f.read_floor_at AS boundary_at,
          f.read_floor_uri AS boundary_uri,s.author_did,s.scope_keys
        FROM appview_publication_read_floors f LEFT JOIN appview_publication_scopes s
          ON s.viewer_did=f.viewer_did AND s.publication_id=f.publication_id WHERE f.viewer_did=?
        UNION ALL SELECT 1,subject_uri,created_at,NULL,NULL,NULL,NULL FROM appview_unread_overrides WHERE viewer_did=?
        UNION ALL SELECT 2,subject_uri,created_at,NULL,NULL,NULL,NULL FROM read_marks WHERE viewer_did=?
        ORDER BY phase,key COLLATE BINARY
        """, arguments: [viewer, viewer, viewer])
    return try rows.map { row in
      let phase: Int = row["phase"]
      let time: String = row["acted_at"]
      if phase == 0 {
        guard let author: String = row["author_did"], let keys: String = row["scope_keys"] else {
          throw PDSReadStateStorageError.legacyScopeUnavailable
        }
        return ReadStateLegacyRow(
          kind: .boundary, actedAt: time,
          boundary: ReadStateBoundary(
            scope: ReadStateScope(
              publicationId: row["key"], authorDid: author,
              publicationSiteKeys: try JSONDecoder().decode([String].self, from: Data(keys.utf8))
                .sorted()),
            createdAt: row["boundary_at"], entryId: row["boundary_uri"]))
      }
      return ReadStateLegacyRow(
        kind: phase == 1 ? .unread : .read, actedAt: time, subjectUri: row["key"])
    }
  }

  public func exportPDSReadStatePage(
    viewerDid: String, cursor: String?, expectedLegacyRevision: Int64?, limit: Int
  ) async throws -> PDSReadStateExportPage {
    guard cursor == nil || expectedLegacyRevision != nil else {
      throw PDSReadStateStorageError.invalidCursor
    }
    let offset: Int
    if let cursor {
      guard let data = Data(base64Encoded: cursor), let text = String(data: data, encoding: .utf8),
        let value = Int(text), value >= 0
      else { throw PDSReadStateStorageError.invalidCursor }
      offset = value
    } else {
      offset = 0
    }
    return try await db.read { database in
      let status = try Self.pdsStatus(database, viewer: viewerDid)
      guard status.authority == .appview else { throw PDSReadStateStorageError.alreadyMigrated }
      guard expectedLegacyRevision == nil || expectedLegacyRevision == status.legacyRevision else {
        throw PDSReadStateStorageError.revisionChanged
      }
      let rows = try Self.legacyRows(database, viewer: viewerDid)
      guard offset <= rows.count else { throw PDSReadStateStorageError.invalidCursor }
      let end = min(rows.count, offset + max(1, min(limit, 1000)))
      return PDSReadStateExportPage(
        legacyRevision: status.legacyRevision, rows: Array(rows[offset..<end]),
        cursor: end < rows.count ? Data(String(end).utf8).base64EncodedString() : nil)
    }
  }

  public func activatePDSReadState(
    viewerDid: String, manifest: ReadStateManifest, manifestCid: String,
    projection: ReadStateProjection, expectedLegacyRevision: Int64?
  ) async throws -> PDSReadStateStatus {
    try ReadStateValidation.validate(manifest, viewerDid: viewerDid)
    guard !manifestCid.isEmpty, projection.lastSequence == manifest.lastSequence,
      projection.protocolVersion == manifest.version
    else { throw ReadStateError.incompleteGeneration }
    let json = String(decoding: try JSONEncoder().encode(manifest), as: UTF8.self)
    return try await db.write { database in
      let prior = try Self.pdsStatus(database, viewer: viewerDid)
      if prior.authority == .appview {
        guard manifest.version == 1 else { throw PDSReadStateStorageError.parityMismatch }
        guard expectedLegacyRevision == prior.legacyRevision else {
          throw PDSReadStateStorageError.revisionChanged
        }
        try Self.verifyLegacyParity(database, viewer: viewerDid, projection: projection)
      } else {
        guard let previous = prior.manifest, let cid = prior.manifestCid else {
          throw ReadStateError.incompleteGeneration
        }
        try PDSReadStateManifestTransition.validate(
          previous: previous, previousCID: cid, candidate: manifest, candidateCID: manifestCid)
      }
      if prior.authority == .pds, prior.projectionReady,
        prior.manifestCid == manifestCid
      {
        return prior
      }
      if prior.authority == .pds, prior.projectionReady,
        PDSReadStateManifestTransition.isMaintenance(previous: prior.manifest, candidate: manifest)
      {
        try database.execute(
          sql:
            "UPDATE appview_pds_read_state_authority SET manifest=?,manifest_cid=? WHERE viewer_did=?",
          arguments: [json, manifestCid, viewerDid])
        return try Self.pdsStatus(database, viewer: viewerDid)
      }
      try database.execute(
        sql: "UPDATE appview_pds_projection_write SET active=1 WHERE singleton=1")
      try database.execute(
        sql: "DELETE FROM appview_pds_read_state_exact WHERE viewer_did=?", arguments: [viewerDid])
      try database.execute(
        sql: "DELETE FROM appview_pds_read_state_boundaries WHERE viewer_did=?",
        arguments: [viewerDid])
      for operation in projection.operations.sorted(by: { $0.sequence < $1.sequence }) {
        try Task.checkCancellation()
        for uri in operation.subjectUris ?? [] {
          try database.execute(
            sql: "INSERT OR REPLACE INTO appview_pds_read_state_exact VALUES(?,?,?,?,?)",
            arguments: [
              viewerDid, uri, operation.sequence, operation.state == .read, operation.actedAt,
            ])
        }
        for boundary in operation.boundaries ?? [] {
          let key = String(decoding: try JSONEncoder().encode(boundary), as: UTF8.self)
          let keys = String(
            decoding: try JSONEncoder().encode(boundary.scope.publicationSiteKeys), as: UTF8.self)
          try database.execute(
            sql:
              "INSERT OR REPLACE INTO appview_pds_read_state_boundaries VALUES(?,?,?,?,?,?,?,?,?)",
            arguments: [
              viewerDid, operation.sequence, key, operation.state == .read, operation.actedAt,
              boundary.scope.authorDid, keys, boundary.createdAt, boundary.entryId,
            ])
        }
      }
      for table in ["read_marks", "appview_unread_overrides", "appview_publication_read_floors"] {
        try database.execute(sql: "DELETE FROM \(table) WHERE viewer_did=?", arguments: [viewerDid])
      }
      try database.execute(
        sql: """
          INSERT INTO read_marks SELECT ?,e.subject_uri,e.acted_at FROM appview_pds_read_state_exact e
          WHERE e.viewer_did=? AND e.is_read=1 AND NOT EXISTS(SELECT 1 FROM content_items c WHERE c.uri=e.subject_uri)
          """, arguments: [viewerDid, viewerDid])
      try database.execute(
        sql: """
          INSERT INTO read_marks SELECT ?,c.uri,c.created_at FROM content_items c
          WHERE \(Self.pdsEffectiveReadSQL(viewer:"?",uri:"c.uri",author:"c.author_did",site:"c.publication_site",date:"c.created_at"))
          """, arguments: [viewerDid, viewerDid, viewerDid])
      try database.execute(
        sql: """
          INSERT INTO appview_pds_read_state_authority(viewer_did,manifest,manifest_cid,projection_ready,last_accessed_at)
          VALUES(?,?,?,1,?) ON CONFLICT(viewer_did) DO UPDATE SET manifest=excluded.manifest,manifest_cid=excluded.manifest_cid,projection_ready=1
          """, arguments: [viewerDid, json, manifestCid, Date().timeIntervalSince1970])
      try database.execute(
        sql: "UPDATE appview_unread_counters SET dirty=1 WHERE viewer_did=?", arguments: [viewerDid]
      )
      try database.execute(
        sql: "UPDATE appview_pds_projection_write SET active=0 WHERE singleton=1")
      return try Self.pdsStatus(database, viewer: viewerDid)
    }
  }

  private static func verifyLegacyParity(
    _ database: Database, viewer: String, projection: ReadStateProjection
  ) throws {
    let overlap =
      try Bool.fetchOne(
        database,
        sql: """
          SELECT 1 FROM appview_publication_scopes a JOIN appview_publication_read_floors af
            ON af.viewer_did=a.viewer_did AND af.publication_id=a.publication_id
          JOIN appview_publication_scopes b ON b.viewer_did=a.viewer_did AND b.author_did=a.author_did AND b.publication_id<>a.publication_id
          LEFT JOIN appview_publication_read_floors bf ON bf.viewer_did=b.viewer_did AND bf.publication_id=b.publication_id
          WHERE a.viewer_did=? AND (json_array_length(a.scope_keys)=0 OR json_array_length(b.scope_keys)=0
            OR EXISTS(SELECT 1 FROM json_each(a.scope_keys) ak JOIN json_each(b.scope_keys) bk ON ak.value=bk.value))
            AND (af.read_floor_at IS NOT bf.read_floor_at OR af.read_floor_uri IS NOT bf.read_floor_uri) LIMIT 1
          """, arguments: [viewer]) ?? false
    guard !overlap else { throw PDSReadStateStorageError.legacyScopeOverlap }
    let rows = try legacyRows(database, viewer: viewer)
    let grouped = Dictionary(grouping: projection.operations, by: \.sequence)
    for (index, row) in rows.enumerated() {
      guard let parts = grouped[Int64(index + 1)], !parts.isEmpty else {
        throw PDSReadStateStorageError.parityMismatch
      }
      for part in parts {
        guard try ReadStateValidation.date(part.actedAt) == ReadStateValidation.date(row.actedAt),
          part.calendar == nil
        else { throw PDSReadStateStorageError.parityMismatch }
        if let boundary = row.boundary {
          guard part.state == .read, part.selection == .boundaries, part.boundaries == [boundary]
          else { throw PDSReadStateStorageError.parityMismatch }
        } else {
          guard part.state == (row.kind == .read ? .read : .unread), part.selection == .exact,
            part.subjectUris == [row.subjectUri!]
          else { throw PDSReadStateStorageError.parityMismatch }
        }
      }
    }
  }

  public func preparePDSReadStateBoundaries(
    viewerDid: String, scopes: [PublicationUnreadScope], at: Date
  ) async throws -> [ReadStateBoundary] {
    var result: [ReadStateBoundary] = []
    for scope in scopes.sorted(by: { $0.publicationId < $1.publicationId }) {
      let keys = AppViewProjectionCacheScopeKeys.publicationSiteKeys(
        publicationAtUri: scope.publicationAtUri,
        publicationScopeAtUris: scope.publicationScopeAtUris,
        publicationSiteUrls: scope.publicationSiteUrls)
      if keys.isEmpty
        && ThinAppViewQuerySupport.requiresPublicationSiteFilter(
          publicationAtUri: scope.publicationAtUri,
          publicationScopeAtUris: scope.publicationScopeAtUris,
          publicationSiteUrls: scope.publicationSiteUrls)
      {
        throw PDSReadStateStorageError.legacyScopeUnavailable
      }
      var requested = ReadWatermarkBoundary(
        publicationId: scope.publicationId, createdAt: at, entryId: nil)
      let newest = try await db.read { database -> (String, Date)? in
        guard
          let row = try Row.fetchOne(
            database,
            sql:
              "SELECT uri,created_at FROM content_items WHERE author_did=? AND tsw_timestamp_us(created_at)<=tsw_timestamp_us(?) AND (? OR EXISTS(SELECT 1 FROM json_each(?) WHERE value=publication_site)) ORDER BY created_at DESC,uri DESC LIMIT 1",
            arguments: [
              scope.authorDid, Self.pdsTimestamp(at), keys.isEmpty,
              String(decoding: try JSONEncoder().encode(keys), as: UTF8.self),
            ])
        else { return nil }
        return (row["uri"], try ReadStateValidation.date(row["created_at"]))
      }
      if let newest {
        requested = ReadWatermarkBoundary(
          publicationId: scope.publicationId,
          createdAt: newest.1, entryId: newest.0)
      }
      if let existing = try await readBoundary(
        viewerDid: viewerDid, publicationId: scope.publicationId), !requested.isAfter(existing)
      {
        requested = existing
      }
      result.append(
        ReadStateBoundary(
          scope: ReadStateScope(
            publicationId: scope.publicationId, authorDid: scope.authorDid,
            publicationSiteKeys: keys.sorted()), createdAt: Self.pdsTimestamp(requested.createdAt),
          entryId: requested.entryId))
    }
    return result
  }

  public func previewPDSReadStateBoundaries(
    viewerDid: String, boundaries: [ReadStateBoundary], subjectUris: [String]
  ) async throws -> [String] {
    guard boundaries.count <= 1000, subjectUris.count <= 1000 else {
      throw ReadStateError.sizeLimit
    }
    let operations = boundaries.enumerated().map {
      ReadStateOperation(
        actionId: "preview-\($0.offset)", sequence: Int64($0.offset + 1), state: .read,
        actedAt: $0.element.createdAt, boundaries: [$0.element])
    }
    let projection = try ReadStateProjection(
      operations: operations, lastSequence: Int64(operations.count))
    return try await db.read { database in
      try Array(Set(subjectUris)).sorted().filter { uri in
        guard
          let row = try Row.fetchOne(
            database,
            sql: "SELECT author_did,publication_site,created_at FROM content_items WHERE uri=?",
            arguments: [uri])
        else { return false }
        return projection.resolve(
          ReadStateSubject(
            uri: uri, authorDid: row["author_did"], publicationSite: row["publication_site"],
            createdAt: try ReadStateValidation.date(row["created_at"]))
        ).isRead
      }
    }
  }

  public func touchPDSReadStateAccess(viewerDid: String, at: Date) async throws {
    try await db.write { database in
      try database.execute(
        sql:
          "UPDATE appview_pds_read_state_authority SET last_accessed_at=? WHERE viewer_did=? AND manifest_cid IS NOT NULL AND last_accessed_at<=?",
        arguments: [
          at.timeIntervalSince1970, viewerDid, at.addingTimeInterval(-3600).timeIntervalSince1970,
        ])
    }
  }

  public func evictIdlePDSReadState(before: Date, at: Date, batchSize: Int) async throws
    -> PDSReadStateEvictionBatch
  {
    let limit = max(1, min(batchSize, 1000))
    return try await db.write { database in
      let candidates = try Row.fetchAll(
        database,
        sql: """
          SELECT a.viewer_did,a.projection_ready FROM appview_pds_read_state_authority a
          WHERE manifest_cid IS NOT NULL AND ((projection_ready=1 AND last_accessed_at<?)
            OR (projection_ready=0 AND (EXISTS(SELECT 1 FROM appview_pds_read_state_exact e WHERE e.viewer_did=a.viewer_did)
              OR EXISTS(SELECT 1 FROM appview_pds_read_state_boundaries b WHERE b.viewer_did=a.viewer_did)
              OR EXISTS(SELECT 1 FROM read_marks r WHERE r.viewer_did=a.viewer_did))))
            AND NOT EXISTS(SELECT 1 FROM appview_ingestion_inbox i WHERE i.repo_did=a.viewer_did AND i.status IN ('pending','retry','leased','dead_letter'))
            AND NOT EXISTS(SELECT 1 FROM appview_ingestion_reconciliation_requests r WHERE r.repo_did=a.viewer_did AND r.status IN ('pending','leased','failed'))
          ORDER BY projection_ready,last_accessed_at,viewer_did
          """, arguments: [before.timeIntervalSince1970])
      for candidate in candidates {
        let viewer: String = candidate["viewer_did"]
        let digest = SHA256.hash(data: Data(viewer.utf8)).map { String(format: "%02x", $0) }
          .joined()
        let leased =
          try Bool.fetchOne(
            database,
            sql:
              "SELECT 1 FROM appview_ingestion_leases WHERE lease_name=? AND released_at IS NULL AND julianday(lease_expires_at)>julianday(?) LIMIT 1",
            arguments: ["pds-read-state-rebuild:" + digest, Self.pdsTimestamp(at)]) ?? false
        if leased { continue }
        let wasReady: Bool = candidate["projection_ready"]
        try database.execute(
          sql: "UPDATE appview_pds_read_state_authority SET projection_ready=0 WHERE viewer_did=?",
          arguments: [viewer])
        try database.execute(
          sql: "UPDATE appview_pds_projection_write SET active=1 WHERE singleton=1")
        var deleted = 0
        for table in [
          "appview_pds_read_state_exact", "appview_pds_read_state_boundaries", "read_marks",
        ] {
          try database.execute(
            sql:
              "DELETE FROM \(table) WHERE rowid IN (SELECT rowid FROM \(table) WHERE viewer_did=? LIMIT ?)",
            arguments: [viewer, limit])
          deleted += database.changesCount
        }
        try database.execute(
          sql: "UPDATE appview_pds_projection_write SET active=0 WHERE singleton=1")
        return PDSReadStateEvictionBatch(
          viewerDid: wasReady ? viewer : nil, deletedRows: deleted, hasMore: true)
      }
      return PDSReadStateEvictionBatch(viewerDid: nil, deletedRows: 0, hasMore: false)
    }
  }
}
