import Foundation
import Logging
import PostgresNIO

extension PostgresThinAppViewStore: FrozenReadMarkStoring {
  func legacyReadStateTransaction<Result: Sendable>(
    viewerDid: String, body: @Sendable (PostgresConnection) async throws -> Result
  ) async throws -> Result {
    do {
      return try await pool.withTransaction(logger: logger) { connection in
        try await Self.requireLegacyReadState(viewerDid: viewerDid, connection: connection, logger: logger)
        return try await body(connection)
      }
    } catch let error as PostgresTransactionError {
      if let storage = error.closureError as? PDSReadStateStorageError { throw storage }
      throw error
    }
  }

  public func applyFrozenReadMarks(viewerDid: String, subjectUris: [String], createdAt: Date) async throws -> [String] {
    let subjects = Array(Set(subjectUris)).sorted()
    guard !subjects.isEmpty else { return [] }
    do {
      return try await pool.withTransaction(logger: logger) { connection in
        try await Self.requireLegacyReadState(viewerDid: viewerDid, connection: connection, logger: logger)
        var applied: [String] = []
        for start in stride(from: 0, to: subjects.count, by: 500) {
          let batch = Array(subjects[start..<min(start + 500, subjects.count)])
          let rows = try await connection.query("""
            SELECT subject_uri FROM unnest(\(batch)::text[]) WITH ORDINALITY subjects(subject_uri, position)
            WHERE NOT EXISTS (SELECT 1 FROM appview_unread_overrides unread
              WHERE unread.viewer_did = \(viewerDid) AND unread.subject_uri = subjects.subject_uri
                AND unread.created_at > \(createdAt))
            ORDER BY position
            """, logger: logger)
          var eligible: [String] = []
          for try await row in rows { eligible.append(try row.decode(String.self)) }
          guard !eligible.isEmpty else { continue }
          try await connection.query("""
            INSERT INTO read_marks(viewer_did, subject_uri, created_at)
            SELECT \(viewerDid), subject_uri, \(createdAt) FROM unnest(\(eligible)::text[]) subjects(subject_uri)
            ON CONFLICT(viewer_did, subject_uri) DO UPDATE SET
              created_at = GREATEST(read_marks.created_at, EXCLUDED.created_at)
            """, logger: logger)
          try await connection.query("""
            DELETE FROM appview_unread_overrides
            WHERE viewer_did = \(viewerDid) AND subject_uri = ANY(\(eligible)) AND created_at <= \(createdAt)
            """, logger: logger)
          let generation = AppViewUnreadCounterSupport.generation(for: createdAt)
          try await connection.query("""
            INSERT INTO appview_unread_counters
              (viewer_did, publication_id, unread_count, generation, accuracy, dirty, counted_at)
            SELECT DISTINCT scope.viewer_did, scope.publication_id, 0,
              \(generation), 'estimated', TRUE, \(createdAt)
            FROM appview_publication_scopes scope JOIN content_items ci ON ci.author_did = scope.author_did
              AND (jsonb_array_length(scope.scope_keys) = 0
                OR (ci.publication_site IS NOT NULL AND scope.scope_keys ? ci.publication_site))
            WHERE scope.viewer_did = \(viewerDid) AND ci.uri = ANY(\(eligible))
            ON CONFLICT(viewer_did, publication_id) DO UPDATE SET
              generation = GREATEST(appview_unread_counters.generation, EXCLUDED.generation),
              accuracy = EXCLUDED.accuracy, dirty = TRUE,
              counted_at = GREATEST(appview_unread_counters.counted_at, EXCLUDED.counted_at)
            """, logger: logger)
          applied.append(contentsOf: eligible)
        }
        if !applied.isEmpty {
          try await connection.query("DELETE FROM unread_counts_cache WHERE viewer_did = \(viewerDid)", logger: logger)
        }
        return applied
      }
    } catch let error as PostgresTransactionError {
      if let storage = error.closureError as? PDSReadStateStorageError { throw storage }
      throw error
    }
  }

  static func requireLegacyReadState(viewerDid: String, connection: PostgresConnection, logger: Logger) async throws {
    try await connection.query("""
      INSERT INTO appview_pds_read_state_authority(viewer_did) VALUES(\(viewerDid))
      ON CONFLICT(viewer_did) DO NOTHING
      """, logger: logger)
    let rows = try await connection.query("""
      SELECT manifest_cid IS NOT NULL FROM appview_pds_read_state_authority
      WHERE viewer_did = \(viewerDid) FOR UPDATE
      """, logger: logger)
    for try await row in rows {
      if try row.decode(Bool.self) { throw PDSReadStateStorageError.alreadyMigrated }
      return
    }
    throw PDSReadStateStorageError.revisionChanged
  }
}
