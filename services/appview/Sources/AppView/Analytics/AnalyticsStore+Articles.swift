import Foundation
import PostgresNIO

extension AnalyticsStore {
  func articles(author: String) async throws -> [AnalyticsItem] {
    let rows = try await pool.query("""
      SELECT item_json::text FROM creator_analytics_items
      WHERE author_did=\(author) AND item_json->>'kind'='article'
      ORDER BY item_json->>'publishedAt' DESC, subject_uri LIMIT 1000
      """, logger: logger)
    var items: [AnalyticsItem] = []
    for try await row in rows {
      let raw = try row.decode(String.self)
      items.append(try AnalyticsCoding.decoder().decode(AnalyticsItem.self, from: Data(raw.utf8)))
    }
    return items
  }
}
