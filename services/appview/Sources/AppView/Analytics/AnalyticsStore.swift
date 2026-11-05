import Crypto
import Foundation
import Logging
import PostgresNIO

struct AnalyticsStore: Sendable {
  let pool: PostgresClient
  let secret: String
  let logger: Logger

  func save(author: String, items: [AnalyticsItem], complete: Bool, at: Date) async throws {
    try Task.checkCancellation()
    try await pool.withTransaction(logger: logger) { connection in
      let lock = try await connection.query("SELECT pg_advisory_xact_lock(hashtextextended(\(author),1))", logger: logger)
      for try await _ in lock {}
      let previous = try await connection.query("SELECT refreshed_at FROM creator_analytics_refresh WHERE author_did=\(author)", logger: logger)
      for try await row in previous { if try row.decode(Date.self) > at { return } }
      if complete {
        let subjects = items.map(\.uri)
        try await connection.query("DELETE FROM creator_analytics_items WHERE author_did=\(author) AND NOT(subject_uri=ANY(\(subjects)::text[]))", logger: logger)
      }
      for item in items {
        let raw = String(decoding: try AnalyticsCoding.encoder().encode(item), as: UTF8.self)
        try await connection.query("""
          INSERT INTO creator_analytics_items(author_did,subject_uri,item_json,refreshed_at)
          VALUES(\(author),\(item.uri),\(raw)::jsonb,\(at))
          ON CONFLICT(author_did,subject_uri) DO UPDATE SET item_json=EXCLUDED.item_json,refreshed_at=EXCLUDED.refreshed_at
          """, logger: logger)
        if let engagement = item.engagement {
          try await connection.query("""
            INSERT INTO creator_analytics_snapshots(author_did,subject_uri,day,likes,replies,reposts,quotes)
            VALUES(\(author),\(item.uri),\(AnalyticsCoding.day(at))::date,\(engagement.likes),\(engagement.replies),\(engagement.reposts),\(engagement.quotes))
            ON CONFLICT(author_did,subject_uri,day) DO UPDATE SET likes=EXCLUDED.likes,replies=EXCLUDED.replies,reposts=EXCLUDED.reposts,quotes=EXCLUDED.quotes
            """, logger: logger)
        }
      }
      try await connection.query("""
        INSERT INTO creator_analytics_refresh(author_did,refreshed_at,inventory_complete) VALUES(\(author),\(at),\(complete))
        ON CONFLICT(author_did) DO UPDATE SET refreshed_at=EXCLUDED.refreshed_at,inventory_complete=EXCLUDED.inventory_complete
        """, logger: logger)
    }
  }

  func record(viewer: String, author: String, event: AnalyticsEvent, at: Date) async throws {
    try Task.checkCancellation()
    if viewer == author { return }
    guard secret.utf8.count >= 32 else { throw AnalyticsError.unavailable }
    let day = AnalyticsCoding.day(at)
    let material = Data("creator-analytics\0\(day)\0\(viewer)".utf8)
    let digest = HMAC<SHA256>.authenticationCode(for: material, using: SymmetricKey(data: Data(secret.utf8)))
    let key = AnalyticsCoding.hex(digest)
    do {
      try await pool.withTransaction(logger: logger) { connection in
        let lock = try await connection.query("SELECT pg_advisory_xact_lock(hashtextextended(\(key),0))", logger: logger)
        for try await _ in lock {}
        let budget = try await connection.query("SELECT count(*) FROM creator_analytics_events WHERE day=\(day)::date AND viewer_key=\(key)", logger: logger)
        for try await row in budget { if try row.decode(Int.self) >= 1000 { throw AnalyticsError.rateLimit } }
        if event.event == "outbound_click" {
          let seen = try await connection.query("""
            SELECT EXISTS(SELECT 1 FROM creator_analytics_events WHERE day=\(day)::date AND viewer_key=\(key)
              AND subject_uri=\(event.subjectUri) AND event='impression')
            """, logger: logger)
          for try await row in seen { if try !row.decode(Bool.self) { return } }
        }
        let inserted = try await connection.query("""
          INSERT INTO creator_analytics_events(author_did,subject_uri,day,viewer_key,event,surface)
          VALUES(\(author),\(event.subjectUri),\(day)::date,\(key),\(event.event),\(event.surface))
          ON CONFLICT DO NOTHING RETURNING subject_uri
          """, logger: logger)
        var changed = false
        for try await _ in inserted { changed = true }
        guard changed else { return }
        try await connection.query("""
          INSERT INTO creator_analytics_tracking(author_did,subject_uri,day,impressions,views,outbound_clicks)
          VALUES(\(author),\(event.subjectUri),\(day)::date,\(event.event == "impression" ? 1 : 0),\(event.event == "view" ? 1 : 0),\(event.event == "outbound_click" ? 1 : 0))
          ON CONFLICT(author_did,subject_uri,day) DO UPDATE SET
            impressions=creator_analytics_tracking.impressions+EXCLUDED.impressions,
            views=creator_analytics_tracking.views+EXCLUDED.views,
            outbound_clicks=creator_analytics_tracking.outbound_clicks+EXCLUDED.outbound_clicks
          """, logger: logger)
      }
    } catch let error as PostgresTransactionError {
      if let analytics = error.closureError as? AnalyticsError { throw analytics }
      throw error
    }
  }

  func summary(author: String, days: Int, limit: Int, cursor: String, at: Date) async throws -> AnalyticsSummary {
    try Task.checkCancellation()
    guard (1...90).contains(days), (1...100).contains(limit) else { throw AnalyticsError.invalid }
    let since = AnalyticsCoding.day(at.addingTimeInterval(Double(-days + 1) * 86400))
    return try await pool.withTransaction(logger: logger) { connection in
      try await connection.query("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY", logger: logger)
      var output = AnalyticsSummary()
      let refresh = try await connection.query("SELECT inventory_complete,refreshed_at FROM creator_analytics_refresh WHERE author_did=\(author)", logger: logger)
      for try await row in refresh {
        let (complete, sampled) = try row.decode((Bool, Date).self)
        output.coverage.inventoryComplete = complete; output.coverage.sampledAt = sampled
      }
      let itemRows = try await connection.query("""
        SELECT item_json::text FROM creator_analytics_items WHERE author_did=\(author) AND subject_uri>\(cursor)
        ORDER BY subject_uri LIMIT \(limit + 1)
        """, logger: logger)
      for try await row in itemRows { output.items.append(try AnalyticsCoding.decoder().decode(AnalyticsItem.self, from: Data(row.decode(String.self).utf8))) }
      if output.items.count > limit {
        output.items = Array(output.items.prefix(limit)); output.cursor = output.items.last?.uri
      }
      var tracking: [String: AnalyticsTracking] = [:]
      let trackingRows = try await connection.query("""
        SELECT subject_uri,sum(impressions)::bigint,sum(views)::bigint,sum(outbound_clicks)::bigint
        FROM creator_analytics_tracking WHERE author_did=\(author) AND day>=\(since)::date GROUP BY subject_uri
        """, logger: logger)
      for try await row in trackingRows {
        let (uri, impressions, views, clicks) = try row.decode((String, Int, Int, Int).self)
        var value = AnalyticsTracking(impressions: impressions, views: views, outboundClicks: clicks)
        value.calculateCTR(); tracking[uri] = value
        output.totals.tracking.impressions += impressions; output.totals.tracking.views += views; output.totals.tracking.outboundClicks += clicks
      }
      output.totals.tracking.calculateCTR()
      for index in output.items.indices { output.items[index].tracking = tracking[output.items[index].uri] ?? .init() }
      let allRows = try await connection.query("SELECT item_json::text FROM creator_analytics_items WHERE author_did=\(author)", logger: logger)
      for try await row in allRows {
        let item = try AnalyticsCoding.decoder().decode(AnalyticsItem.self, from: Data(row.decode(String.self).utf8))
        output.coverage.totalItems += 1
        if let engagement = item.engagement {
          output.coverage.engagementKnownItems += 1
          var total = output.totals.engagement ?? .init()
          total.likes += engagement.likes; total.replies += engagement.replies
          total.reposts += engagement.reposts; total.quotes += engagement.quotes
          output.totals.engagement = total
        }
      }
      let firstSnapshot = try await connection.query("SELECT min(day)::text FROM creator_analytics_snapshots WHERE author_did=\(author)", logger: logger)
      for try await row in firstSnapshot { output.coverage.snapshotStart = try row.decode(String?.self) }
      let firstTracking = try await connection.query("SELECT min(day)::text FROM creator_analytics_tracking WHERE author_did=\(author)", logger: logger)
      for try await row in firstTracking { output.coverage.trackingStart = try row.decode(String?.self) }
      let series = try await connection.query("""
        WITH days AS (SELECT day FROM creator_analytics_snapshots WHERE author_did=\(author) AND day>=\(since)::date
          UNION SELECT day FROM creator_analytics_tracking WHERE author_did=\(author) AND day>=\(since)::date),
        engagement AS (SELECT day,sum(likes)::bigint likes,sum(replies)::bigint replies,sum(reposts)::bigint reposts,sum(quotes)::bigint quotes
          FROM creator_analytics_snapshots WHERE author_did=\(author) AND day>=\(since)::date GROUP BY day),
        tracking AS (SELECT day,sum(impressions)::bigint impressions,sum(views)::bigint views,sum(outbound_clicks)::bigint clicks
          FROM creator_analytics_tracking WHERE author_did=\(author) AND day>=\(since)::date GROUP BY day)
        SELECT d.day::text,e.likes,e.replies,e.reposts,e.quotes,t.impressions,t.views,t.clicks FROM days d
          LEFT JOIN engagement e USING(day) LEFT JOIN tracking t USING(day) ORDER BY d.day
        """, logger: logger)
      for try await row in series {
        let values = try row.decode((String, Int?, Int?, Int?, Int?, Int?, Int?, Int?).self)
        var day = AnalyticsDay(day: values.0, likes: values.1, replies: values.2, reposts: values.3,
          quotes: values.4, impressions: values.5, views: values.6, outboundClicks: values.7)
        if let impressions = day.impressions, let clicks = day.outboundClicks {
          var value = AnalyticsTracking(impressions: impressions, outboundClicks: clicks); value.calculateCTR(); day.ctr = value.ctr
        }
        output.series.append(day)
      }
      return output
    }
  }
}
