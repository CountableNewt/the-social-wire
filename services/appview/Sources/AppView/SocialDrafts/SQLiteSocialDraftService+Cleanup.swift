import Foundation
import GRDB

extension SQLiteSocialDraftService {
  func cleanup(limit: Int) async throws -> Int {
    guard (1...1000).contains(limit) else { throw SocialDraftStorageError.invalid }
    let deadline = ContinuousClock.now.advanced(by: .seconds(30))
    var completed = 0
    var retryFailed = false
    for _ in 0..<limit {
      try Task.checkCancellation()
      guard ContinuousClock.now < deadline else { break }
      let now = Date().timeIntervalSince1970
      let job = try await database.read { db in
        try Row.fetchOne(db, sql: "SELECT owner,asset_id FROM private_social_draft_cleanup WHERE available_at<=? ORDER BY available_at,owner,asset_id LIMIT 1", arguments: [now]).map { row -> (String, String) in (row["owner"], row["asset_id"]) }
      }
      guard let (owner, id) = job else { break }
      // The same owner lease used by mutations prevents cleanup racing a manifest
      // bind or transfer, without keeping a SQLite transaction open across S3 I/O.
      let lease: SocialDraftAdmissionLease
      do { lease = try admission.acquire(owner: owner) }
      catch { break }
      let work = Task.detached { try await self.cleanupOne(owner: owner, id: id) }
      let result = try await work.value
      withExtendedLifetime(lease) {}
      if result.removed { completed += 1 }
      retryFailed = retryFailed || result.failed
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

  private func cleanupOne(owner: String, id: String) async throws -> (removed: Bool, failed: Bool) {
    let key: String? = try await database.write { db in
      guard try !isReferenced(owner: owner, id: id, in: db) else {
        try db.execute(sql: "DELETE FROM private_social_draft_cleanup WHERE owner=? AND asset_id=?", arguments: [owner, id])
        return nil
      }
      let key = try String.fetchOne(db, sql: "SELECT object_key FROM private_social_draft_assets WHERE owner=? AND asset_id=?", arguments: [owner, id])
      try db.execute(sql: "UPDATE private_social_draft_assets SET state='deleted' WHERE owner=? AND asset_id=?", arguments: [owner, id])
      return key
    }
    guard let key else { return (false, false) }
    do { try await objects.delete(key: key) }
    catch {
      try await database.write { db in
        let attempts = try Int.fetchOne(db, sql: "SELECT attempts FROM private_social_draft_cleanup WHERE owner=? AND asset_id=?", arguments: [owner, id]) ?? 0
        let delay = min(60, pow(2, Double(min(attempts, 6)))) * 60
        try db.execute(sql: "UPDATE private_social_draft_cleanup SET attempts=?,available_at=? WHERE owner=? AND asset_id=?", arguments: [min(attempts + 1, 30), Date().addingTimeInterval(delay).timeIntervalSince1970, owner, id])
      }
      return (false, true)
    }
    try await database.write { db in
      try db.execute(sql: "UPDATE private_social_draft_assets SET state='purged',metadata=x'' WHERE owner=? AND asset_id=?", arguments: [owner, id])
      try db.execute(sql: "DELETE FROM private_social_draft_cleanup WHERE owner=? AND asset_id=?", arguments: [owner, id])
    }
    return (true, false)
  }
}
