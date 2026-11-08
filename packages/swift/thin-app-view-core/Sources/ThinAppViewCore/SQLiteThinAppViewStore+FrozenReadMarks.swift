import Foundation
import GRDB

extension SQLiteThinAppViewStore: FrozenReadMarkStoring {
  public func applyFrozenReadMarks(viewerDid: String, subjectUris: [String], createdAt: Date) async throws -> [String] {
    let subjects = Array(Set(subjectUris)).sorted()
    guard !subjects.isEmpty else { return [] }
    let timestamp = ISO8601DateFormatter().string(from: createdAt)
    let generation = AppViewUnreadCounterSupport.generation(for: createdAt)
    return try await db.write { database in
      try Self.requireLegacyReadState(database, viewer: viewerDid)
      var applied: [String] = []
      for start in stride(from: 0, to: subjects.count, by: 500) {
        let batch = Array(subjects[start..<min(start + 500, subjects.count)])
        for subject in batch {
          let newerUnread = try Int.fetchOne(database, sql: """
            SELECT COUNT(*) FROM appview_unread_overrides
            WHERE viewer_did = ? AND subject_uri = ? AND created_at > ?
            """, arguments: [viewerDid, subject, timestamp]) ?? 0
          guard newerUnread == 0 else { continue }
          try database.execute(sql: """
            INSERT INTO read_marks(viewer_did, subject_uri, created_at) VALUES(?, ?, ?)
            ON CONFLICT(viewer_did, subject_uri) DO UPDATE SET created_at = MAX(read_marks.created_at, excluded.created_at)
            """, arguments: [viewerDid, subject, timestamp])
          try database.execute(sql: """
            DELETE FROM appview_unread_overrides
            WHERE viewer_did = ? AND subject_uri = ? AND created_at <= ?
            """, arguments: [viewerDid, subject, timestamp])
          try database.execute(sql: """
            INSERT INTO appview_unread_counters
              (viewer_did, publication_id, unread_count, generation, accuracy, dirty, counted_at)
            SELECT DISTINCT scope.viewer_did, scope.publication_id, 0, ?, 'estimated', 1, ?
            FROM appview_publication_scopes scope JOIN content_items ci ON ci.author_did = scope.author_did
              AND (json_array_length(scope.scope_keys) = 0
                OR EXISTS(SELECT 1 FROM json_each(scope.scope_keys) WHERE value = ci.publication_site))
            WHERE scope.viewer_did = ? AND ci.uri = ?
            ON CONFLICT(viewer_did, publication_id) DO UPDATE SET
              generation = MAX(appview_unread_counters.generation, excluded.generation),
              accuracy = excluded.accuracy, dirty = 1,
              counted_at = MAX(appview_unread_counters.counted_at, excluded.counted_at)
            """, arguments: [generation, timestamp, viewerDid, subject])
          applied.append(subject)
        }
      }
      return applied
    }
  }
}
