import Crypto
import Foundation
import PostgresNIO

extension IngressDatabase {
  func quarantine(_ records: [(IngressEvent, Data)], connection: PostgresConnection) async throws {
    guard !records.isEmpty else { return }
    let c = configuration
    _ = try await connection.query(
      "SELECT TRUE FROM ingestion_recovery_controllers WHERE environment=\(c.environment) AND source_generation=\(c.generation) FOR UPDATE",
      logger: logger)
    for (event, raw) in records {
      let digest = SHA256.hash(data: raw).map { String(format: "%02x", $0) }.joined()
      let existing = try await connection.query(
        "SELECT payload_sha256,source_host FROM ingestion_poison_records WHERE environment=\(c.environment) AND source_generation=\(c.generation) AND seq=\(event.sequence)",
        logger: logger)
      var exists = false
      for try await row in existing {
        let (prior, host) = try row.decode((String, String).self)
        guard prior == digest, host == c.host else { throw IngressError.sourceDiscontinuity }
        exists = true
      }
      if !exists {
        let counts = try await connection.query(
          "SELECT COUNT(*),COALESCE(SUM(OCTET_LENGTH(raw_payload)),0)::bigint FROM ingestion_poison_records WHERE environment=\(c.environment) AND source_generation=\(c.generation)",
          logger: logger)
        for try await row in counts {
          let (count, bytes) = try row.decode((Int64, Int64).self)
          guard count < 10000, bytes + Int64(raw.count) <= 64 << 20 else {
            throw IngressError.poisonCapacity
          }
        }
      }
      _ = try await connection.query(
        "INSERT INTO ingestion_poison_records(environment,source_generation,seq,source_host,repo_did,collection,record_key,record_cid,payload_sha256,raw_payload,reason) VALUES(\(c.environment),\(c.generation),\(event.sequence),\(c.host),\(event.did),\(event.collection),\(event.recordKey),\(event.recordCID),\(digest),\(raw),\("Deterministic JSON payload normalization failed")) ON CONFLICT(environment,source_generation,seq) DO UPDATE SET updated_at=NOW()",
        logger: logger)
    }
    _ = try await connection.query(
      "UPDATE ingestion_recovery_controllers SET status='degraded',last_error='Deterministic poison record quarantined',updated_at=NOW() WHERE environment=\(c.environment) AND source_generation=\(c.generation) AND status<>'blocked_discontinuity'",
      logger: logger)
  }
  public func prunePoison() async throws {
    let c = configuration
    _ = try await client.query(
      "WITH expired AS(SELECT environment,source_generation,seq FROM ingestion_poison_records WHERE environment=\(c.environment) AND source_generation=\(c.generation) AND status='quarantined' AND expires_at<=NOW() ORDER BY seq FOR UPDATE SKIP LOCKED LIMIT 500) UPDATE ingestion_poison_records poison SET raw_payload=NULL,status='expired',updated_at=NOW() FROM expired WHERE poison.environment=expired.environment AND poison.source_generation=expired.source_generation AND poison.seq=expired.seq"
    )
  }
  public func blockDiscontinuity(_ lease: IngressLease) async throws {
    let c = configuration
    try await client.withTransaction(logger: logger) { connection in
      try await fence(connection, lease: lease)
      _ = try await connection.query(
        "UPDATE ingestion_recovery_controllers SET status='blocked_discontinuity',last_error='Source identity or cursor discontinuity',updated_at=NOW() WHERE environment=\(c.environment) AND source_generation=\(c.generation)",
        logger: logger)
    }
  }
}
