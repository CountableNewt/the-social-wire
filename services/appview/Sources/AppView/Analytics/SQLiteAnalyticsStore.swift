import Crypto
import Foundation
@preconcurrency import GRDB

/// Serializes observation budgets and inventory replacement in SQLite write transactions.
struct SQLiteAnalyticsStore: AnalyticsStoring {
  let db: DatabasePool
  let secret: String

  init(path: String, secret: String) throws {
    self.secret = secret
    var configuration = Configuration()
    configuration.busyMode = .timeout(10)
    db = try DatabasePool(path: path, configuration: configuration)
    var migrator = DatabaseMigrator()
    migrator.registerMigration("creator-analytics-v1") { database in
      try database.execute(sql: """
        CREATE TABLE creator_analytics_items (
          author_did TEXT NOT NULL, subject_uri TEXT NOT NULL, item_json TEXT NOT NULL,
          refreshed_at REAL NOT NULL, PRIMARY KEY(author_did,subject_uri));
        CREATE TABLE creator_analytics_refresh (
          author_did TEXT PRIMARY KEY, refreshed_at REAL NOT NULL, inventory_complete INTEGER NOT NULL);
        CREATE TABLE creator_analytics_snapshots (
          author_did TEXT NOT NULL, subject_uri TEXT NOT NULL, day TEXT NOT NULL,
          likes INTEGER NOT NULL, replies INTEGER NOT NULL, reposts INTEGER NOT NULL, quotes INTEGER NOT NULL,
          PRIMARY KEY(author_did,subject_uri,day));
        CREATE TABLE creator_analytics_events (
          author_did TEXT NOT NULL, subject_uri TEXT NOT NULL, day TEXT NOT NULL, viewer_key TEXT NOT NULL,
          event TEXT NOT NULL CHECK(event IN ('impression','view','outbound_click')),
          surface TEXT NOT NULL CHECK(surface IN ('social-feed','article-feed','post-viewer','article-reader','messages')),
          PRIMARY KEY(subject_uri,day,viewer_key,event));
        CREATE INDEX creator_analytics_events_budget ON creator_analytics_events(day,viewer_key);
        CREATE INDEX creator_analytics_events_retention ON creator_analytics_events(day);
        CREATE TABLE creator_analytics_tracking (
          author_did TEXT NOT NULL, subject_uri TEXT NOT NULL, day TEXT NOT NULL,
          impressions INTEGER NOT NULL, views INTEGER NOT NULL, outbound_clicks INTEGER NOT NULL,
          PRIMARY KEY(author_did,subject_uri,day));
        """)
    }
    try migrator.migrate(db)
  }

  func save(author: String, items: [AnalyticsItem], complete: Bool, at: Date) async throws {
    try Task.checkCancellation()
    try await db.write { database in
      let previous = try Double.fetchOne(database, sql: "SELECT refreshed_at FROM creator_analytics_refresh WHERE author_did=?", arguments: [author])
      if let previous, previous > at.timeIntervalSince1970 { return }
      if complete {
        // Reinsert the complete inventory atomically; snapshots and observations remain independent.
        try database.execute(sql: "DELETE FROM creator_analytics_items WHERE author_did=?", arguments: [author])
      }
      for item in items {
        let raw = String(decoding: try AnalyticsCoding.encoder().encode(item), as: UTF8.self)
        try database.execute(sql: """
          INSERT INTO creator_analytics_items(author_did,subject_uri,item_json,refreshed_at) VALUES(?,?,?,?)
          ON CONFLICT(author_did,subject_uri) DO UPDATE SET item_json=excluded.item_json,refreshed_at=excluded.refreshed_at
          """, arguments: [author, item.uri, raw, at.timeIntervalSince1970])
        if let value = item.engagement {
          try database.execute(sql: """
            INSERT INTO creator_analytics_snapshots(author_did,subject_uri,day,likes,replies,reposts,quotes) VALUES(?,?,?,?,?,?,?)
            ON CONFLICT(author_did,subject_uri,day) DO UPDATE SET likes=excluded.likes,replies=excluded.replies,reposts=excluded.reposts,quotes=excluded.quotes
            """, arguments: [author, item.uri, AnalyticsCoding.day(at), value.likes, value.replies, value.reposts, value.quotes])
        }
      }
      try database.execute(sql: """
        INSERT INTO creator_analytics_refresh(author_did,refreshed_at,inventory_complete) VALUES(?,?,?)
        ON CONFLICT(author_did) DO UPDATE SET refreshed_at=excluded.refreshed_at,inventory_complete=excluded.inventory_complete
        """, arguments: [author, at.timeIntervalSince1970, complete])
    }
  }

  func record(viewer: String, author: String, event: AnalyticsEvent, at: Date) async throws {
    try Task.checkCancellation()
    if viewer == author { return }
    guard secret.utf8.count >= 32 else { throw AnalyticsError.unavailable }
    let day = AnalyticsCoding.day(at)
    let digest = HMAC<SHA256>.authenticationCode(for: Data("creator-analytics\0\(day)\0\(viewer)".utf8),
      using: SymmetricKey(data: Data(secret.utf8)))
    let key = AnalyticsCoding.hex(digest)
    try await db.write { database in
      let budget = try Int.fetchOne(database, sql: "SELECT count(*) FROM creator_analytics_events WHERE day=? AND viewer_key=?", arguments: [day, key]) ?? 0
      guard budget < 1000 else { throw AnalyticsError.rateLimit }
      if event.event == "outbound_click" {
        let seen = try Bool.fetchOne(database, sql: "SELECT EXISTS(SELECT 1 FROM creator_analytics_events WHERE day=? AND viewer_key=? AND subject_uri=? AND event='impression')", arguments: [day, key, event.subjectUri]) ?? false
        guard seen else { return }
      }
      try database.execute(sql: """
        INSERT INTO creator_analytics_events(author_did,subject_uri,day,viewer_key,event,surface) VALUES(?,?,?,?,?,?)
        ON CONFLICT DO NOTHING
        """, arguments: [author, event.subjectUri, day, key, event.event, event.surface])
      guard database.changesCount > 0 else { return }
      try database.execute(sql: """
        INSERT INTO creator_analytics_tracking(author_did,subject_uri,day,impressions,views,outbound_clicks) VALUES(?,?,?,?,?,?)
        ON CONFLICT(author_did,subject_uri,day) DO UPDATE SET
          impressions=creator_analytics_tracking.impressions+excluded.impressions,
          views=creator_analytics_tracking.views+excluded.views,
          outbound_clicks=creator_analytics_tracking.outbound_clicks+excluded.outbound_clicks
        """, arguments: [author, event.subjectUri, day, event.event == "impression" ? 1 : 0,
          event.event == "view" ? 1 : 0, event.event == "outbound_click" ? 1 : 0])
    }
  }

  func summary(author: String, days: Int, limit: Int, cursor: String, at: Date) async throws -> AnalyticsSummary {
    try Task.checkCancellation()
    guard (1...90).contains(days), (1...100).contains(limit) else { throw AnalyticsError.invalid }
    let since = AnalyticsCoding.day(at.addingTimeInterval(Double(-days + 1) * 86400))
    return try await db.read { database in
      var output = AnalyticsSummary()
      if let row = try Row.fetchOne(database, sql: "SELECT inventory_complete,refreshed_at FROM creator_analytics_refresh WHERE author_did=?", arguments: [author]) {
        output.coverage.inventoryComplete = row["inventory_complete"]
        output.coverage.sampledAt = Date(timeIntervalSince1970: row["refreshed_at"])
      }
      output.items = try String.fetchAll(database, sql: "SELECT item_json FROM creator_analytics_items WHERE author_did=? AND subject_uri>? ORDER BY subject_uri LIMIT ?", arguments: [author, cursor, limit + 1])
        .map { try AnalyticsCoding.decoder().decode(AnalyticsItem.self, from: Data($0.utf8)) }
      if output.items.count > limit {
        output.items = Array(output.items.prefix(limit)); output.cursor = output.items.last?.uri
      }
      var tracking: [String: AnalyticsTracking] = [:]
      for row in try Row.fetchAll(database, sql: """
        SELECT subject_uri,sum(impressions) impressions,sum(views) views,sum(outbound_clicks) clicks
        FROM creator_analytics_tracking WHERE author_did=? AND day>=? GROUP BY subject_uri
        """, arguments: [author, since]) {
        let uri: String = row["subject_uri"]
        var value = AnalyticsTracking(impressions: row["impressions"], views: row["views"], outboundClicks: row["clicks"])
        value.calculateCTR(); tracking[uri] = value
        output.totals.tracking.impressions += value.impressions
        output.totals.tracking.views += value.views
        output.totals.tracking.outboundClicks += value.outboundClicks
      }
      output.totals.tracking.calculateCTR()
      for index in output.items.indices { output.items[index].tracking = tracking[output.items[index].uri] ?? .init() }
      for raw in try String.fetchAll(database, sql: "SELECT item_json FROM creator_analytics_items WHERE author_did=?", arguments: [author]) {
        let item = try AnalyticsCoding.decoder().decode(AnalyticsItem.self, from: Data(raw.utf8))
        output.coverage.totalItems += 1
        if let engagement = item.engagement {
          output.coverage.engagementKnownItems += 1
          var total = output.totals.engagement ?? .init()
          total.likes += engagement.likes; total.replies += engagement.replies
          total.reposts += engagement.reposts; total.quotes += engagement.quotes
          output.totals.engagement = total
        }
      }
      output.coverage.snapshotStart = try String.fetchOne(database, sql: "SELECT min(day) FROM creator_analytics_snapshots WHERE author_did=?", arguments: [author])
      output.coverage.trackingStart = try String.fetchOne(database, sql: "SELECT min(day) FROM creator_analytics_tracking WHERE author_did=?", arguments: [author])
      let rows = try Row.fetchAll(database, sql: """
        WITH days AS (SELECT day FROM creator_analytics_snapshots WHERE author_did=? AND day>=?
          UNION SELECT day FROM creator_analytics_tracking WHERE author_did=? AND day>=?),
        engagement AS (SELECT day,sum(likes) likes,sum(replies) replies,sum(reposts) reposts,sum(quotes) quotes
          FROM creator_analytics_snapshots WHERE author_did=? AND day>=? GROUP BY day),
        tracking AS (SELECT day,sum(impressions) impressions,sum(views) views,sum(outbound_clicks) clicks
          FROM creator_analytics_tracking WHERE author_did=? AND day>=? GROUP BY day)
        SELECT d.day,e.likes,e.replies,e.reposts,e.quotes,t.impressions,t.views,t.clicks FROM days d
          LEFT JOIN engagement e USING(day) LEFT JOIN tracking t USING(day) ORDER BY d.day
        """, arguments: [author, since, author, since, author, since, author, since])
      for row in rows {
        var day = AnalyticsDay(day: row["day"], likes: row["likes"], replies: row["replies"], reposts: row["reposts"],
          quotes: row["quotes"], impressions: row["impressions"], views: row["views"], outboundClicks: row["clicks"])
        if let impressions = day.impressions, let clicks = day.outboundClicks {
          var value = AnalyticsTracking(impressions: impressions, outboundClicks: clicks); value.calculateCTR(); day.ctr = value.ctr
        }
        output.series.append(day)
      }
      return output
    }
  }

  func cleanup(at: Date) async throws {
    try Task.checkCancellation()
    try await db.write { database in
      for (table, days) in [("creator_analytics_events", 30), ("creator_analytics_tracking", 90), ("creator_analytics_snapshots", 90)] {
        let cutoff = AnalyticsCoding.day(at.addingTimeInterval(Double(-days) * 86400))
        try database.execute(sql: "DELETE FROM \(table) WHERE rowid IN (SELECT rowid FROM \(table) WHERE day<? ORDER BY day LIMIT 1000)", arguments: [cutoff])
      }
      for table in ["creator_analytics_items", "creator_analytics_refresh"] {
        try database.execute(sql: "DELETE FROM \(table) WHERE rowid IN (SELECT rowid FROM \(table) WHERE refreshed_at<? ORDER BY refreshed_at LIMIT 1000)", arguments: [at.addingTimeInterval(-90 * 86400).timeIntervalSince1970])
      }
    }
  }
}
