import Foundation
import PostgresNIO
import Testing

@testable import IngressCore

@Suite(.serialized)
struct IngressDatabaseTests {
  @Test(.enabled(if: ProcessInfo.processInfo.environment["INGRESS_TEST_DATABASE_URL"] != nil))
  func fencedStageScopePoisonAndCheckpointAreAtomic() async throws {
    let url = try #require(ProcessInfo.processInfo.environment["INGRESS_TEST_DATABASE_URL"])
    guard let parsed = URLComponents(string: url),
      ["127.0.0.1", "localhost"].contains(parsed.host), parsed.path.hasPrefix("/tsw_")
    else {
      throw IngressError.invalidConfiguration(
        "Integration tests require a disposable local tsw_ database")
    }
    let generation = "swift-ingress-test-" + UUID().uuidString
    let config = try IngressConfiguration(environment: [
      "APP_ENV": "dev", "DATABASE_URL": url, "JETSTREAM_API_KEY": "fixture",
      "JETSTREAM_SOURCE_GENERATION": generation, "JETSTREAM_LEADER_LEASE_NAME": generation,
    ])
    let database = try IngressDatabase(configuration: config)
    try await withThrowingTaskGroup(of: Void.self) { group in
      group.addTask { await database.run() }
      defer { group.cancelAll() }
      try await database.ping()
      let author = "did:plc:author-" + UUID().uuidString
      let viewer = "did:plc:viewer-" + UUID().uuidString
      _ = try await database.client.query(
        "INSERT INTO appview_publication_scopes(viewer_did,publication_id,author_did) VALUES(\(viewer),\("publication"),\(author))"
      )
      let lease = try await database.acquire(owner: "owner-a")
      try await database.enablePoisonProtection(lease)
      try await database.renew(lease)
      #expect(try await database.trackedDIDs().contains(author))
      let event = try makeEvent(
        sequence: 100, author: author, record: .object(["title": .string("a\0b")]))
      let unscoped = try makeEvent(
        sequence: 101, author: "did:plc:unscoped",
        record: .object(["title": .string("outside scope")]))
      let progress = ReplayProgress(state: "replaying", after: 0, sealed: 200, bytes: 42)
      try await database.stage(
        [event, unscoped], checkpointSequence: 101, eventTime: unscoped.eventTime, lease: lease,
        progress: progress)
      #expect(try await database.checkpoint()?.sequence == 101)
      let inserted = try await database.client.query(
        "SELECT seq,payload #>> '{commit,record,title}' FROM appview_ingestion_inbox WHERE source_generation=\(generation)"
      )
      var rows: [(Int64, String)] = []
      for try await row in inserted { rows.append(try row.decode((Int64, String).self)) }
      #expect(rows.count == 1)
      #expect(rows.first?.0 == 100)
      #expect(rows.first?.1 == "a\u{fffd}b")
      // Replaying the inclusive overlap is idempotent and cannot regress checkpoint time.
      try await database.stage(
        [event], checkpointSequence: 100, eventTime: event.eventTime, lease: lease,
        progress: progress)
      #expect(try await database.checkpoint()?.sequence == 101)
      let stale = IngressLease(name: lease.name, owner: lease.owner, token: lease.token + 1)
      await #expect(throws: (any Error).self) {
        try await database.stage(
          [event], checkpointSequence: 102, eventTime: event.eventTime, lease: stale,
          progress: progress)
      }
      #expect(try await database.checkpoint()?.sequence == 101)
      // A checkpoint identity collision rolls back the preceding good inbox insert.
      _ = try await database.client.query(
        "UPDATE appview_jetstream_checkpoints SET source_host='mismatch.example' WHERE source_generation=\(generation)"
      )
      let next = try makeEvent(
        sequence: 103, author: author, record: .object(["title": .string("must roll back")]))
      await #expect(throws: (any Error).self) {
        try await database.stage(
          [next], checkpointSequence: 103, eventTime: next.eventTime, lease: lease,
          progress: progress)
      }
      let countRows = try await database.client.query(
        "SELECT COUNT(*) FROM appview_ingestion_inbox WHERE source_generation=\(generation) AND seq=103"
      )
      for try await row in countRows { #expect(try row.decode(Int64.self) == 0) }
      _ = try await database.client.query(
        "UPDATE appview_jetstream_checkpoints SET source_host=\(config.host) WHERE source_generation=\(generation)"
      )
      // A serialization conflict retries the entire fenced transaction, not just its INSERT.
      _ = try await database.client.query(
        "DROP TRIGGER IF EXISTS swift_ingress_retry ON appview_ingestion_inbox")
      _ = try await database.client.query("DROP FUNCTION IF EXISTS swift_ingress_retry()")
      _ = try await database.client.query("DROP SEQUENCE IF EXISTS swift_ingress_retry_count")
      _ = try await database.client.query("CREATE SEQUENCE swift_ingress_retry_count")
      _ = try await database.client.query(
        PostgresQuery(
          unsafeSQL:
            "CREATE FUNCTION swift_ingress_retry() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN IF nextval(''swift_ingress_retry_count'')=1 THEN RAISE EXCEPTION ''retry fixture'' USING ERRCODE=''40001''; END IF; RETURN NEW; END'"
        ))
      _ = try await database.client.query(
        PostgresQuery(
          unsafeSQL:
            "CREATE TRIGGER swift_ingress_retry BEFORE INSERT ON appview_ingestion_inbox FOR EACH ROW WHEN(NEW.seq=102) EXECUTE FUNCTION swift_ingress_retry()"
        ))
      let retried = try makeEvent(
        sequence: 102, author: author, record: .object(["title": .string("retry atomically")]))
      try await database.stage(
        [retried], checkpointSequence: 102, eventTime: retried.eventTime, lease: lease,
        progress: progress)
      #expect(try await database.checkpoint()?.sequence == 102)
      let attempts = try await database.client.query(
        "SELECT last_value FROM swift_ingress_retry_count")
      for try await row in attempts { #expect(try row.decode(Int64.self) == 2) }
      _ = try await database.client.query(
        "DROP TRIGGER swift_ingress_retry ON appview_ingestion_inbox")
      _ = try await database.client.query("DROP FUNCTION swift_ingress_retry()")
      _ = try await database.client.query("DROP SEQUENCE swift_ingress_retry_count")
      try await database.incident(category: "no_progress_24h", sequence: 102, progress: progress)
      try await database.incident(category: "no_progress_24h", sequence: 102, progress: progress)
      let incidents = try await database.client.query(
        "SELECT COUNT(*),MAX(occurrence_count) FROM appview_ingestion_incidents WHERE source_generation=\(generation)"
      )
      for try await row in incidents {
        let (count, occurrences) = try row.decode((Int64, Int64).self)
        #expect(count == 1)
        #expect(occurrences == 2)
      }
      // Only deterministic JSON representation failures quarantine; original evidence is retained.
      let poisoned = try makeEvent(
        sequence: 104, author: author,
        record: .object(["\0": .integer(1), "\u{fffd}": .integer(2)]))
      try await database.stage(
        [poisoned], checkpointSequence: 104, eventTime: poisoned.eventTime, lease: lease,
        progress: progress)
      try await database.stage(
        [poisoned], checkpointSequence: 104, eventTime: poisoned.eventTime, lease: lease,
        progress: progress)
      #expect(try await database.checkpoint()?.sequence == 104)
      let poisonRows = try await database.client.query(
        "SELECT COUNT(*) FROM ingestion_poison_records WHERE source_generation=\(generation) AND raw_payload IS NOT NULL"
      )
      for try await row in poisonRows { #expect(try row.decode(Int64.self) == 1) }
      try await database.release(lease)
      let replacement = try await database.acquire(owner: "owner-b")
      #expect(replacement.token > lease.token)
      await #expect(throws: (any Error).self) {
        try await database.stage(
          [next], checkpointSequence: 105, eventTime: next.eventTime, lease: lease,
          progress: progress)
      }
      #expect(try await database.checkpoint()?.sequence == 104)
      try await database.release(replacement)
    }
  }
  @Test(.enabled(if: ProcessInfo.processInfo.environment["INGRESS_TEST_DATABASE_URL"] != nil))
  func schemaReadinessRequiresOlderMissingReceipt() async throws {
    let url = try #require(ProcessInfo.processInfo.environment["INGRESS_TEST_DATABASE_URL"])
    try await SchemaReadiness.wait(url: url, versions: ["20260815190000"], timeout: .seconds(5))
    await #expect(throws: (any Error).self) {
      try await SchemaReadiness.wait(
        url: url, versions: ["19990101000001", "20260815190000"], timeout: .milliseconds(100))
    }
  }
  func makeEvent(sequence: Int64, author: String, record: JSONValue) throws -> IngressEvent {
    try IngressEvent(
      payload: .object([
        "cursor": .integer(sequence), "time_us": .integer(1_700_000_000_000_000 + sequence),
        "kind": .string("commit"), "did": .string(author),
        "commit": .object([
          "collection": .string("site.standard.document"), "operation": .string("create"),
          "rkey": .string("key-\(sequence)"), "rev": .string("rev-\(sequence)"), "record": record,
        ]),
      ]))
  }
}
