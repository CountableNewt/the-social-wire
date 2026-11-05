import Foundation
import PostgresNIO

extension IngressDatabase {
  func insertInbox(_ events: [(IngressEvent, String)], connection: PostgresConnection) async throws
  {
    let c = configuration
    var start = 0
    while start < events.count {
      var end = start
      var bytes = 0
      while end < events.count && end - start < 500 {
        let size = events[end].1.utf8.count
        if end > start && bytes + size > 4 << 20 { break }
        bytes += size
        end += 1
      }
      var values: [String] = []
      var bindings = PostgresBindings()
      for (event, payload) in events[start..<end] {
        let base = bindings.count + 1
        bindings.append(c.environment)
        bindings.append(c.generation)
        bindings.append(event.sequence)
        bindings.append(c.host)
        bindings.append(c.cursorKind)
        bindings.append(event.kind)
        bindings.append(event.did)
        if let value = event.collection { bindings.append(value) } else { bindings.appendNull() }
        if let value = event.operation { bindings.append(value) } else { bindings.appendNull() }
        if let value = event.revision { bindings.append(value) } else { bindings.appendNull() }
        if let value = event.recordKey { bindings.append(value) } else { bindings.appendNull() }
        if let value = event.recordCID { bindings.append(value) } else { bindings.appendNull() }
        bindings.append(payload)
        bindings.append(event.eventTime)
        let types = [
          "text", "text", "bigint", "text", "text", "text", "text", "text", "text", "text", "text",
          "text", "jsonb", "timestamptz",
        ]
        values.append(
          "("
            + types.enumerated().map { "$\(base + $0.offset)::\($0.element)" }.joined(
              separator: ",") + ")")
      }
      let columns =
        "environment,source_generation,seq,source_host,cursor_kind,event_kind,repo_did,collection,operation,repo_rev,record_key,record_cid,payload,event_time"
      let query = """
        INSERT INTO appview_ingestion_inbox(\(columns),status,attempt_count,next_attempt_at,staged_at)
        SELECT incoming.*,'pending',0,NOW(),NOW() FROM(VALUES \(values.joined(separator: ","))) AS incoming(\(columns))
        WHERE incoming.collection IS NULL
        OR(incoming.collection IN('site.standard.document','site.standard.entry','com.standard.document','com.standard.entry') AND EXISTS(SELECT 1 FROM appview_publication_scopes scope WHERE scope.author_did=incoming.repo_did))
        OR(incoming.collection IN('app.skyreader.feed.subscription','site.standard.graph.subscription','app.thesocialwire.readState','app.thesocialwire.finance.selection','app.thesocialwire.sports.selection') AND(EXISTS(SELECT 1 FROM appview_viewer_feeds feed WHERE feed.viewer_did=incoming.repo_did) OR EXISTS(SELECT 1 FROM appview_publication_scopes scope WHERE scope.viewer_did=incoming.repo_did) OR(incoming.collection='app.thesocialwire.finance.selection' AND EXISTS(SELECT 1 FROM finance_selection_sync sync WHERE sync.viewer_did=incoming.repo_did)) OR(incoming.collection='app.thesocialwire.sports.selection' AND EXISTS(SELECT 1 FROM sports_selection_sync sync WHERE sync.viewer_did=incoming.repo_did))))
        ON CONFLICT(environment,source_generation,seq) DO NOTHING
        """
      _ = try await connection.query(
        PostgresQuery(unsafeSQL: query, binds: bindings), logger: logger)
      start = end
    }
  }
}
