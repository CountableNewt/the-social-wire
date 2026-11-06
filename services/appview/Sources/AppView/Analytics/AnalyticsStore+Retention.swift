import Foundation
import PostgresNIO

extension AnalyticsStore {
  /// The table identifiers are a closed constant list, never request data.
  func cleanup(at: Date) async throws {
    for (table, days) in [("creator_analytics_events", 30), ("creator_analytics_tracking", 90), ("creator_analytics_snapshots", 90)] {
      let cutoff = AnalyticsCoding.day(at.addingTimeInterval(Double(-days) * 86400))
      try await pool.query("""
        DELETE FROM \(unescaped: table) WHERE ctid IN
          (SELECT ctid FROM \(unescaped: table) WHERE day<\(cutoff)::date ORDER BY day LIMIT 1000)
        """, logger: logger)
    }
    for table in ["creator_analytics_items", "creator_analytics_refresh"] {
      let cutoff = at.addingTimeInterval(-90 * 86400)
      try await pool.query("""
        DELETE FROM \(unescaped: table) WHERE ctid IN
          (SELECT ctid FROM \(unescaped: table) WHERE refreshed_at<\(cutoff) ORDER BY refreshed_at LIMIT 1000)
        """, logger: logger)
    }
  }
}
