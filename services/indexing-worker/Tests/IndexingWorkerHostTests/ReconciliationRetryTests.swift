import Foundation
import IndexingWorkerCore
import Logging
import PostgresNIO
import Testing
import ThinAppViewCore

@testable import IndexingWorkerHost

struct ReconciliationRetryTests {
  @Test func rejectsMissingExplicitDatabaseBeforeConnecting() async throws {
    let scope = try ReconciliationRetryScope(
      environment: "dev", generation: "fixture", expectedCount: 1)
    for url in ["", "private credentials are not a URL", "https://database.example/pg"] {
      await #expect(throws: ReconciliationRetryError.invalidScope) {
        try await ReconciliationRetryRunner.run(scope: scope, databaseURL: url)
      }
    }
  }

  @Test(.enabled(if: ProcessInfo.processInfo.environment["THIN_APPVIEW_TEST_DATABASE_URL"] != nil))
  func retriesOnlyExactFailedScopeAndPreservesHistoricalEvidence() async throws {
    let url = try #require(ProcessInfo.processInfo.environment["THIN_APPVIEW_TEST_DATABASE_URL"])
    let logger = ReconciliationRetryRunner.logger
    var configuration = try makePostgresConfig(from: url, logger: logger)
    configuration.options.maximumConnections = 3
    let pool = PostgresClient(configuration: configuration, backgroundLogger: logger)
    let run = Task { await pool.run() }
    let generation = "operator-retry-" + UUID().uuidString.lowercased()
    do {
      let at = Date(timeIntervalSince1970: 1_700_000_000)
      for sequence in 1...9 {
        let environment = sequence == 8 ? "dev" : "prod"
        let did = "did:plc:operator-fixture"
        let inboxDID = sequence == 5 ? did + "-different" : did
        let inboxStatus = sequence == 4 ? "pending" : "dead_letter"
        let reconciled: Date? = sequence == 3 ? at : nil
        try await pool.query(
          """
          INSERT INTO appview_ingestion_inbox
            (environment,source_generation,seq,source_host,cursor_kind,event_kind,repo_did,payload,
             event_time,status,attempt_count,dead_lettered_at,reconciled_at,recovery_state)
          VALUES (\(environment),\(generation),\(sequence),'fixture','jetstream_v2_seq','commit',\(inboxDID),
            '{}',\(at),\(inboxStatus),12,\(at),\(reconciled),'inbox-checkpoint-byte-for-byte')
          """, logger: logger)
        let status = sequence == 6 ? "pending" : sequence == 7 ? "completed" : "failed"
        let trigger = sequence == 9 ? sequence + 100 : sequence
        try await pool.query(
          """
          INSERT INTO appview_ingestion_reconciliation_requests
            (environment,id,source_generation,repo_did,reason,trigger_seq,status,attempt_count,
             next_attempt_at,lease_owner,lease_token,lease_expires_at,created_at,updated_at,recovery_state)
          VALUES (\(environment),\("request-" + String(sequence)),\(generation),\(did),
            'historical:reconciliation_failed:safe-category',\(trigger),\(status),11,\(at),
            'old-owner','old-token',\(at),\(at),\(at),'{"completed":false,"cursor":"private-checkpoint"}')
          """, logger: logger)
      }
      try await pool.query(
        """
        INSERT INTO appview_repository_recovery_records(environment,source_generation,recovery_key,request_id,uri)
        VALUES ('prod',\(generation),'recovery-fixture','request-1','at://did:plc:fixture/site.standard.document/retained')
        """, logger: logger)
      try await pool.query(
        """
        INSERT INTO appview_jetstream_checkpoints(environment,source_generation,source_host,stream_nsid,
          filter_fingerprint,cursor_kind,last_staged_seq,last_applied_seq,updated_at)
        VALUES ('prod',\(generation),'fixture','fixture','fixture','jetstream_v2_seq',500,100,\(at))
        """, logger: logger)
      let before = try await evidence(pool, generation: generation)
      let allRequestsBefore = try await requests(
        pool, generation: generation, excludeSelected: false)
      let unselectedBefore = try await requests(pool, generation: generation, excludeSelected: true)
      for expected in [1, 3] {
        let scope = try ReconciliationRetryScope(
          environment: "prod", generation: generation, expectedCount: expected)
        await #expect(throws: ReconciliationRetryError.expectedCountMismatch) {
          try await ReconciliationRetryRunner.apply(
            scope: scope, pool: pool, at: at.addingTimeInterval(60))
        }
      }
      #expect(
        try await requests(pool, generation: generation, excludeSelected: false)
          == allRequestsBefore)
      let scope = try ReconciliationRetryScope(
        environment: "prod", generation: generation, expectedCount: 2)
      // A candidate lock must block the whole exact scope; cancellation cannot
      // turn the remaining unlocked subset into an authorized partial retry.
      try await pool.withConnection { lock in
        try await lock.query("BEGIN", logger: logger)
        try await lock.query(
          """
          SELECT id FROM appview_ingestion_reconciliation_requests
          WHERE environment='prod' AND source_generation=\(generation) AND id='request-1' FOR UPDATE
          """, logger: logger)
        let retry = Task {
          try await ReconciliationRetryRunner.apply(scope: scope, pool: pool, at: at)
        }
        try await Task.sleep(for: .milliseconds(50))
        retry.cancel()
        if case .success = await retry.result { Issue.record("Locked candidate was skipped") }
        try await lock.query("ROLLBACK", logger: logger)
      }
      #expect(
        try await requests(pool, generation: generation, excludeSelected: false)
          == allRequestsBefore)
      #expect(
        try await ReconciliationRetryRunner.apply(
          scope: scope, pool: pool, at: at.addingTimeInterval(60)) == 2)
      #expect(try await evidence(pool, generation: generation) == before)
      #expect(
        try await requests(pool, generation: generation, excludeSelected: true) == unselectedBefore)
      let valid = try await pool.query(
        """
        SELECT COUNT(*) FROM appview_ingestion_reconciliation_requests
        WHERE environment='prod' AND source_generation=\(generation) AND id=ANY(\(["request-1", "request-2"])::text[])
          AND status='pending' AND attempt_count=11 AND reason='historical:reconciliation_failed:safe-category'
          AND recovery_state='{"completed":false,"cursor":"private-checkpoint"}' AND completed_at IS NULL
          AND lease_owner IS NULL AND lease_token IS NULL AND lease_expires_at IS NULL
          AND next_attempt_at=\(at.addingTimeInterval(60)) AND updated_at=\(at.addingTimeInterval(60))
        """, logger: logger)
      for try await row in valid { #expect(try row.decode(Int.self) == 2) }
      await #expect(throws: ReconciliationRetryError.expectedCountMismatch) {
        try await ReconciliationRetryRunner.apply(scope: scope, pool: pool, at: at)
      }
      try await cleanup(pool, generation: generation)
    } catch {
      try? await cleanup(pool, generation: generation)
      run.cancel()
      await run.value
      throw error
    }
    run.cancel()
    await run.value
  }

  private func evidence(_ pool: PostgresClient, generation: String) async throws -> String {
    let rows = try await pool.query(
      """
      SELECT json_build_object(
        'inbox',(SELECT json_agg(row_to_json(i) ORDER BY i.environment,i.seq) FROM appview_ingestion_inbox i WHERE source_generation=\(generation)),
        'recovery',(SELECT json_agg(row_to_json(r) ORDER BY r.uri) FROM appview_repository_recovery_records r WHERE source_generation=\(generation)),
        'checkpoint',(SELECT json_agg(row_to_json(c)) FROM appview_jetstream_checkpoints c WHERE source_generation=\(generation)))::text
      """, logger: ReconciliationRetryRunner.logger)
    for try await row in rows { return try row.decode(String.self) }
    throw ReconciliationRetryError.expectedCountMismatch
  }

  private func requests(_ pool: PostgresClient, generation: String, excludeSelected: Bool)
    async throws -> String
  {
    let rows = try await pool.query(
      """
      SELECT COALESCE(json_agg(row_to_json(r) ORDER BY r.environment,r.id)::text,'[]')
      FROM appview_ingestion_reconciliation_requests r WHERE source_generation=\(generation)
        AND (NOT \(excludeSelected) OR id<>ALL(\(["request-1", "request-2"])::text[]))
      """, logger: ReconciliationRetryRunner.logger)
    for try await row in rows { return try row.decode(String.self) }
    throw ReconciliationRetryError.expectedCountMismatch
  }

  private func cleanup(_ pool: PostgresClient, generation: String) async throws {
    try await pool.query(
      "DELETE FROM appview_repository_recovery_records WHERE source_generation=\(generation)")
    try await pool.query(
      "DELETE FROM appview_ingestion_reconciliation_requests WHERE source_generation=\(generation)")
    try await pool.query(
      "DELETE FROM appview_ingestion_inbox WHERE source_generation=\(generation)")
    try await pool.query(
      "DELETE FROM appview_jetstream_checkpoints WHERE source_generation=\(generation)")
  }
}
