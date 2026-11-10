import Foundation
@preconcurrency import GRDB

extension SQLiteAnalyticsStore {
  func articles(author: String) async throws -> [AnalyticsItem] {
    try await db.read { database in
      try String.fetchAll(database, sql: """
        SELECT item_json FROM creator_analytics_items
        WHERE author_did=? AND json_extract(item_json,'$.kind')='article'
        ORDER BY json_extract(item_json,'$.publishedAt') DESC, subject_uri LIMIT 1000
        """, arguments: [author]).map {
          try AnalyticsCoding.decoder().decode(AnalyticsItem.self, from: Data($0.utf8))
        }
    }
  }
}
