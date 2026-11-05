import Foundation
import PostgresNIO

extension IngressDatabase {
  public func usage() async throws -> [(Date, Int64)] {
    let c = configuration
    let rows = try await client.query(
      "SELECT bucket_started_at,bytes_downloaded FROM appview_ingestion_replay_usage WHERE environment=\(c.environment) AND source_generation=\(c.generation) AND bucket_started_at>NOW()-INTERVAL '24 hours' ORDER BY bucket_started_at"
    )
    var output: [(Date, Int64)] = []
    for try await row in rows { output.append(try row.decode((Date, Int64).self)) }
    return output
  }
  public func recordBytes(_ bytes: Int64) async throws {
    guard bytes > 0 else { return }
    let c = configuration
    _ = try await client.query(
      "INSERT INTO appview_ingestion_replay_usage(environment,source_generation,bucket_started_at,bytes_downloaded,updated_at) VALUES(\(c.environment),\(c.generation),date_trunc('minute',NOW()),\(bytes),NOW()) ON CONFLICT(environment,source_generation,bucket_started_at) DO UPDATE SET bytes_downloaded=appview_ingestion_replay_usage.bytes_downloaded+EXCLUDED.bytes_downloaded,updated_at=NOW()"
    )
  }
  public func updateProgress(_ progress: ReplayProgress, lease: IngressLease) async throws {
    let c = configuration
    try await client.withTransaction(logger: logger) { connection in
      try await fence(connection, lease: lease)
      _ = try await connection.query(
        "UPDATE appview_jetstream_checkpoints SET replay_state=\(progress.state),replay_bytes_downloaded=\(progress.bytes),replay_retry_count=\(progress.retries),replay_range_resume_count=\(progress.resumes),replay_etag=\(progress.etag),updated_at=NOW() WHERE environment=\(c.environment) AND source_generation=\(c.generation)",
        logger: logger)
    }
  }
}
