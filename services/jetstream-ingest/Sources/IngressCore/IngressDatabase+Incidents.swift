import Foundation
import PostgresNIO

extension IngressDatabase {
  public func incident(category: String, sequence: Int64, progress: ReplayProgress) async throws {
    let c = configuration
    let key = c.environment + "|" + c.generation + "|" + c.host + "|" + category
    try await client.withTransaction(logger: logger) { connection in
      _ = try await connection.query(
        "SELECT pg_advisory_xact_lock(hashtext(\(key)))", logger: logger)
      let rows = try await connection.query(
        "SELECT id FROM appview_ingestion_incidents WHERE environment=\(c.environment) AND source_generation=\(c.generation) AND source_host=\(c.host) AND source='jetstream-v2' AND cursor_kind=\(c.cursorKind) AND category=\(category) AND status IN('open','recovering','verification_required') ORDER BY last_detected_at DESC,id DESC LIMIT 1 FOR UPDATE",
        logger: logger)
      var id: String?
      for try await row in rows { id = try row.decode(String.self) }
      let cursor = sequence == 0 ? nil : sequence
      if let id {
        _ = try await connection.query(
          "UPDATE appview_ingestion_incidents SET occurrence_count=occurrence_count+1,start_cursor=COALESCE(start_cursor,\(cursor)),end_cursor=GREATEST(end_cursor,\(cursor)),last_detected_at=NOW(),last_error=\(category),replay_state=\(progress.state),replay_bytes_downloaded=GREATEST(replay_bytes_downloaded,\(progress.bytes)),replay_retry_count=GREATEST(replay_retry_count,\(progress.retries)),replay_range_resume_count=GREATEST(replay_range_resume_count,\(progress.resumes)),replay_sealed_seq=GREATEST(replay_sealed_seq,\(progress.sealed)),updated_at=NOW(),version=version+1 WHERE environment=\(c.environment) AND id=\(id)",
          logger: logger)
      } else {
        _ = try await connection.query(
          "INSERT INTO appview_ingestion_incidents(environment,id,source_generation,source_host,source,cursor_kind,start_cursor,end_cursor,category,status,occurrence_count,first_detected_at,last_detected_at,last_error,replay_state,replay_bytes_downloaded,replay_retry_count,replay_range_resume_count,replay_sealed_seq,verification_evidence,updated_at) VALUES(\(c.environment),\(UUID().uuidString),\(c.generation),\(c.host),'jetstream-v2',\(c.cursorKind),\(cursor),\(cursor),\(category),'open',1,NOW(),NOW(),\(category),\(progress.state),\(progress.bytes),\(progress.retries),\(progress.resumes),\(progress.sealed),'{}'::jsonb,NOW())",
          logger: logger)
      }
    }
  }
  public func pruneReplayUsage() async throws {
    let c = configuration
    _ = try await client.query(
      "DELETE FROM appview_ingestion_replay_usage WHERE environment=\(c.environment) AND source_generation=\(c.generation) AND bucket_started_at<=NOW()-INTERVAL '48 hours'"
    )
  }
}
