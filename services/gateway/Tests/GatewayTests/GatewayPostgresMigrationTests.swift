import Foundation
import Logging
import PostgresNIO
import Testing
import ThinAppViewCore
@testable import Gateway

@Test(.enabled(if: ProcessInfo.processInfo.environment["TEST_GATEWAY_DATABASE_URL"] != nil))
func gatewayCacheUsesTheCurrentMigratedDatabaseAndKeepsExpiredRecordsHidden() async throws {
  let url = try #require(ProcessInfo.processInfo.environment["TEST_GATEWAY_DATABASE_URL"])
  let logger = Logger(label: "gateway-migration-postgres")
  var configuration = try makePostgresConfig(from: url, logger: logger)
  configuration.tls = .disable
  let pool = PostgresClient(configuration: configuration, backgroundLogger: logger)
  let owner = "did:plc:cache-\(UUID().uuidString)"
  let scope = "site.standard.publication:migration"
  try await withThrowingTaskGroup(of: Void.self) { group in
    group.addTask { await pool.run() }
    defer { group.cancelAll() }
    let cache = PostgresCache(pool: pool, logger: logger)
    do {
      try await cache.storePdsRepoRecordPayload(ownerDid: owner, scopeKey: scope, cid: "bafyx", jsonBody: #"{"value":{"name":"Migration"}}"#, cachedAt: Date(), expiresAt: Date().addingTimeInterval(60))
      let stored = try #require(try await cache.cachedPdsRepoRecord(ownerDid: owner, scopeKey: scope))
      #expect(stored.cid == "bafyx")
      #expect(stored.jsonBody.contains("Migration"))
      #expect(try await cache.cachedPdsRepoRecord(ownerDid: owner + "-other", scopeKey: scope) == nil)
      try await cache.storePdsRepoRecordPayload(ownerDid: owner, scopeKey: scope, cid: nil, jsonBody: "{}", cachedAt: Date().addingTimeInterval(-60), expiresAt: Date().addingTimeInterval(-1))
      #expect(try await cache.cachedPdsRepoRecord(ownerDid: owner, scopeKey: scope) == nil)
      _ = try await pool.query("DELETE FROM pds_repo_record_cache WHERE owner_did=\(owner)", logger: logger)
    } catch {
      _ = try? await pool.query("DELETE FROM pds_repo_record_cache WHERE owner_did=\(owner)", logger: logger)
      throw error
    }
  }
}
