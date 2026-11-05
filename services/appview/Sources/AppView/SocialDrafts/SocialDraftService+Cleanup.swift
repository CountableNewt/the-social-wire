import Foundation
import PostgresNIO

extension SocialDraftService {
  func cleanup(limit: Int) async throws -> Int {
    guard (1...1000).contains(limit) else { throw SocialDraftStorageError.invalid }
    let lease = try admission.acquire(owner: "")
    defer { withExtendedLifetime(lease) {} }
    var completed = 0
    var retryFailed = false
    for _ in 0..<limit {
      try Task.checkCancellation()
      // A claimed job must persist its retry even if the sweep is cancelled.
      // This independent task is always joined before returning. S3 delete is
      // bounded to 5s; each SQL operation and the idle transaction are bounded.
      let job = Task.detached { try await self.cleanupOne() }
      let result = try await job.value
      if !result.claimed { break }
      if result.removed { completed += 1 }
      retryFailed = retryFailed || result.retryFailed
    }
    if retryFailed { throw SocialDraftStorageError.unavailable }
    return completed
  }

  func runCleanup() async {
    while !Task.isCancelled {
      do {
        try await Task.sleep(for: .seconds(900))
        _ = try await cleanup(limit: 100)
      } catch is CancellationError { return }
      catch { logger.warning("Private draft cleanup deferred") }
    }
  }

  private func cleanupOne() async throws -> (claimed: Bool, removed: Bool, retryFailed: Bool) {
    do {
      return try await pool.withTransaction(logger: logger) { connection in
        try await connection.query("SET LOCAL statement_timeout='2s'", logger: self.logger)
        try await connection.query("SET LOCAL idle_in_transaction_session_timeout='10s'", logger: self.logger)
        let owners = try await connection.query("""
          SELECT a.owner FROM private_social_draft_cleanup c
          JOIN private_social_draft_accounts a ON a.owner=c.owner
          WHERE c.available_at<=now() ORDER BY c.available_at,c.owner,c.asset_id
          FOR UPDATE OF a SKIP LOCKED LIMIT 1
          """, logger: self.logger)
        var owner: String?
        for try await row in owners { owner = try row.decode(String.self) }
        guard let owner else { return (false, false, false) }
        let jobs = try await connection.query("""
          SELECT c.asset_id::text,a.object_key FROM private_social_draft_cleanup c
          JOIN private_social_draft_assets a USING(owner,asset_id)
          WHERE c.owner=\(owner) AND c.available_at<=now()
          ORDER BY c.available_at,c.asset_id LIMIT 1 FOR UPDATE OF c,a
          """, logger: self.logger)
        var job: (String, String)?
        for try await row in jobs { job = try row.decode((String, String).self) }
        guard let (id, key) = job else { return (true, false, false) }
        let bindings = try await connection.query("""
          SELECT EXISTS(SELECT 1 FROM private_social_draft_bindings WHERE owner=\(owner) AND asset_id=\(id)::uuid)
          """, logger: self.logger)
        var referenced = false
        for try await row in bindings { referenced = try row.decode(Bool.self) }
        var failed = false
        if !referenced {
          try await connection.query("UPDATE private_social_draft_assets SET state='deleted' WHERE owner=\(owner) AND asset_id=\(id)::uuid", logger: self.logger)
          do { try await self.objects.delete(key: key) }
          catch { failed = true }
        }
        if failed {
          try await connection.query("""
            UPDATE private_social_draft_cleanup SET attempts=LEAST(attempts+1,30),
              available_at=now()+interval '1 minute'*LEAST(60,power(2,LEAST(attempts,6)))
            WHERE owner=\(owner) AND asset_id=\(id)::uuid
            """, logger: self.logger)
        } else {
          if !referenced {
            try await connection.query("UPDATE private_social_draft_assets SET state='purged',metadata=''::bytea WHERE owner=\(owner) AND asset_id=\(id)::uuid", logger: self.logger)
          }
          try await connection.query("DELETE FROM private_social_draft_cleanup WHERE owner=\(owner) AND asset_id=\(id)::uuid", logger: self.logger)
        }
        return (true, !referenced && !failed, failed)
      }
    } catch { throw SocialDraftStorageError.unavailable }
  }
}
