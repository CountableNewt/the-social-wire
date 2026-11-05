import Crypto
import Foundation
import Logging
import PostgresNIO

public struct IngressDatabase: Sendable {
  public let client: PostgresClient
  public let configuration: IngressConfiguration
  let logger = Logger(label: "socialwire.ingress.database")
  public init(configuration: IngressConfiguration) throws {
    self.configuration = configuration
    let name =
      "socialwire-appview-ingress-"
      + (ProcessInfo.processInfo.environment["RAILWAY_SERVICE_NAME"] ?? "local").map {
        $0.isLetter || $0.isNumber || $0 == "-" ? $0 : "-"
      }
    client = PostgresClient(
      configuration: try PostgresConfiguration.make(
        url: configuration.databaseURL, name: name, maxOpen: configuration.poolMaxOpen,
        minIdle: configuration.poolMaxIdle, idleTimeout: configuration.poolIdleTimeout))
  }
  public func run() async { await client.run() }
  public func ping() async throws { _ = try await client.query("SELECT 1") }
  public func checkpoint() async throws -> IngressCheckpoint? {
    let c = configuration
    let rows = try await client.query(
      "SELECT source_host, stream_nsid, filter_fingerprint, cursor_kind, last_staged_seq, replay_bytes_downloaded, replay_state, replay_retry_count, replay_range_resume_count, replay_etag, GREATEST(last_staged_at,replay_last_progress_at) FROM appview_jetstream_checkpoints WHERE environment=\(c.environment) AND source_generation=\(c.generation)"
    )
    for try await row in rows {
      let (
        host, stream, fingerprint, cursor, seq, bytes, state, retries, resumes, etag, lastProgress
      ) = try row.decode(
        (String, String, String, String, Int64?, Int64, String, Int, Int, String?, Date?).self)
      guard host == c.host, stream == c.streamNSID, fingerprint == c.fingerprint,
        cursor == c.cursorKind, seq == nil || seq! >= 0, state != "snapshot_complete"
      else { throw IngressError.sourceDiscontinuity }
      return IngressCheckpoint(
        sequence: seq, replayBytes: bytes, state: state, retries: retries, resumes: resumes,
        etag: etag, lastProgressAt: lastProgress)
    }
    return nil
  }
  public func acquire(owner: String) async throws -> IngressLease {
    let c = configuration
    let ttl = "\(configuration.leaseTTL) seconds"
    let rows = try await client.query(
      """
      INSERT INTO appview_ingestion_leases(environment,lease_name,source_generation,owner_id,fencing_token,acquired_at,lease_expires_at,updated_at)
      SELECT \(c.environment),\(c.leaseName),\(c.generation),\(owner),1,NOW(),NOW()+\(ttl)::interval,NOW()
      WHERE NOT EXISTS(SELECT 1 FROM appview_ingestion_leases held WHERE held.environment=\(c.environment) AND held.lease_name=\(c.leaseName) AND held.owner_id<>\(owner) AND held.released_at IS NULL AND held.lease_expires_at>NOW())
      ON CONFLICT(environment,lease_name) DO UPDATE SET source_generation=EXCLUDED.source_generation,owner_id=EXCLUDED.owner_id,fencing_token=appview_ingestion_leases.fencing_token+1,acquired_at=NOW(),lease_expires_at=EXCLUDED.lease_expires_at,released_at=NULL,updated_at=NOW()
      WHERE appview_ingestion_leases.released_at IS NOT NULL OR appview_ingestion_leases.lease_expires_at<=NOW() OR appview_ingestion_leases.owner_id=EXCLUDED.owner_id
      RETURNING fencing_token
      """)
    for try await row in rows {
      return IngressLease(name: c.leaseName, owner: owner, token: try row.decode(Int64.self))
    }
    throw IngressError.leaseUnavailable
  }
  public func renew(_ lease: IngressLease) async throws {
    let c = configuration
    let ttl = "\(configuration.leaseTTL) seconds"
    let rows = try await client.query(
      "UPDATE appview_ingestion_leases SET lease_expires_at=NOW()+\(ttl)::interval,updated_at=NOW() WHERE environment=\(c.environment) AND lease_name=\(lease.name) AND source_generation=\(c.generation) AND owner_id=\(lease.owner) AND fencing_token=\(lease.token) AND released_at IS NULL AND lease_expires_at>NOW() RETURNING 1"
    )
    for try await _ in rows { return }
    throw IngressError.leaseUnavailable
  }
  public func release(_ lease: IngressLease) async throws {
    let c = configuration
    _ = try await client.query(
      "UPDATE appview_ingestion_leases SET released_at=NOW(),lease_expires_at=NOW(),updated_at=NOW() WHERE environment=\(c.environment) AND lease_name=\(lease.name) AND source_generation=\(c.generation) AND owner_id=\(lease.owner) AND fencing_token=\(lease.token) AND released_at IS NULL"
    )
  }
  func fence(_ connection: PostgresConnection, lease: IngressLease) async throws {
    let c = configuration
    let rows = try await connection.query(
      "SELECT TRUE FROM appview_ingestion_leases WHERE environment=\(c.environment) AND lease_name=\(lease.name) AND source_generation=\(c.generation) AND owner_id=\(lease.owner) AND fencing_token=\(lease.token) AND released_at IS NULL AND lease_expires_at>NOW() FOR SHARE",
      logger: logger)
    for try await _ in rows { return }
    throw IngressError.leaseUnavailable
  }
  public func trackedDIDs() async throws -> Set<String> {
    let rows = try await client.query(
      "SELECT author_did FROM appview_publication_scopes WHERE author_did<>'' UNION SELECT viewer_did FROM appview_viewer_feeds WHERE viewer_did<>'' UNION SELECT viewer_did FROM appview_publication_scopes WHERE viewer_did<>'' UNION SELECT viewer_did FROM finance_selection_sync WHERE viewer_did<>'' UNION SELECT viewer_did FROM sports_selection_sync WHERE viewer_did<>''"
    )
    var output: Set<String> = []
    for try await row in rows { output.insert(try row.decode(String.self)) }
    return output
  }
  public func enablePoisonProtection(_ lease: IngressLease) async throws {
    let c = configuration
    try await client.withTransaction(logger: logger) { connection in
      try await fence(connection, lease: lease)
      let rows = try await connection.query(
        """
        INSERT INTO ingestion_recovery_controllers(environment,source_generation,source_host,stream_nsid,filter_fingerprint,cursor_kind,status)
        VALUES(\(c.environment),\(c.generation),\(c.host),\(c.streamNSID),\(c.fingerprint),\(c.cursorKind),'healthy')
        ON CONFLICT(environment,source_generation) DO UPDATE SET updated_at=NOW()
        WHERE ingestion_recovery_controllers.source_host=EXCLUDED.source_host AND ingestion_recovery_controllers.stream_nsid=EXCLUDED.stream_nsid AND ingestion_recovery_controllers.filter_fingerprint=EXCLUDED.filter_fingerprint AND ingestion_recovery_controllers.cursor_kind=EXCLUDED.cursor_kind AND ingestion_recovery_controllers.status<>'blocked_discontinuity'
        RETURNING 1
        """, logger: logger)
      for try await _ in rows { return }
      throw IngressError.sourceDiscontinuity
    }
  }
  public func stage(
    _ events: [IngressEvent], checkpointSequence: Int64, eventTime: Date, lease: IngressLease,
    progress: ReplayProgress
  ) async throws {
    guard checkpointSequence > 0, events.allSatisfy({ $0.sequence <= checkpointSequence }) else {
      throw IngressError.invalidEvent("Invalid staging checkpoint")
    }
    var good: [(IngressEvent, String)] = []
    var poison: [(IngressEvent, Data)] = []
    for event in events {
      do {
        good.append(
          (event, String(decoding: try event.encodedPayload(normalize: true), as: UTF8.self)))
      } catch { poison.append((event, try event.encodedPayload())) }
    }
    // The whole fenced unit is retried, never just the insert or checkpoint.
    for attempt in 0..<8 {
      do {
        try await stageTransaction(
          good, poison: poison, sequence: checkpointSequence, eventTime: eventTime, lease: lease,
          progress: progress)
        return
      } catch {
        let cause = Self.transactionCause(error)
        guard let sql = cause as? PSQLError,
          ["40001", "40P01"].contains(sql.serverInfo?[.sqlState] ?? ""), attempt < 7
        else { throw cause }
        try await Task.sleep(for: .milliseconds(min(320, 10 << attempt)))
      }
    }
  }
  static func transactionCause(_ error: any Error) -> any Error {
    if let transaction = error as? PostgresTransactionError {
      return transaction.closureError ?? transaction.commitError ?? transaction.beginError ?? error
    }
    return error
  }
  private func stageTransaction(
    _ events: [(IngressEvent, String)], poison: [(IngressEvent, Data)], sequence: Int64,
    eventTime: Date, lease: IngressLease, progress: ReplayProgress
  ) async throws {
    let c = configuration
    try await client.withTransaction(logger: logger) { connection in
      try await fence(connection, lease: lease)
      try await quarantine(poison, connection: connection)
      try await insertInbox(events, connection: connection)
      let rows = try await connection.query(
        """
        INSERT INTO appview_jetstream_checkpoints(environment,source_generation,source_host,stream_nsid,filter_fingerprint,cursor_kind,last_staged_seq,last_staged_event_at,last_staged_at,replay_state,replay_after_seq,replay_sealed_seq,replay_bytes_downloaded,replay_retry_count,replay_range_resume_count,replay_etag,replay_last_progress_at,updated_at)
        VALUES(\(c.environment),\(c.generation),\(c.host),\(c.streamNSID),\(c.fingerprint),\(c.cursorKind),\(sequence),\(eventTime),NOW(),\(progress.state),\(progress.after),\(progress.sealed),\(progress.bytes),\(progress.retries),\(progress.resumes),\(progress.etag),NOW(),NOW())
        ON CONFLICT(environment,source_generation) DO UPDATE SET
          last_staged_seq=GREATEST(appview_jetstream_checkpoints.last_staged_seq,EXCLUDED.last_staged_seq),
          last_staged_event_at=CASE WHEN appview_jetstream_checkpoints.last_staged_seq IS NULL OR EXCLUDED.last_staged_seq>=appview_jetstream_checkpoints.last_staged_seq THEN EXCLUDED.last_staged_event_at ELSE appview_jetstream_checkpoints.last_staged_event_at END,
          last_staged_at=CASE WHEN appview_jetstream_checkpoints.last_staged_seq IS NULL OR EXCLUDED.last_staged_seq>appview_jetstream_checkpoints.last_staged_seq THEN NOW() ELSE appview_jetstream_checkpoints.last_staged_at END,
          replay_state=EXCLUDED.replay_state,replay_after_seq=EXCLUDED.replay_after_seq,replay_sealed_seq=EXCLUDED.replay_sealed_seq,replay_bytes_downloaded=EXCLUDED.replay_bytes_downloaded,replay_retry_count=EXCLUDED.replay_retry_count,replay_range_resume_count=EXCLUDED.replay_range_resume_count,replay_etag=EXCLUDED.replay_etag,
          replay_last_progress_at=CASE WHEN appview_jetstream_checkpoints.last_staged_seq IS NULL OR EXCLUDED.last_staged_seq>appview_jetstream_checkpoints.last_staged_seq THEN NOW() ELSE appview_jetstream_checkpoints.replay_last_progress_at END,updated_at=NOW()
        WHERE appview_jetstream_checkpoints.source_host=EXCLUDED.source_host AND appview_jetstream_checkpoints.stream_nsid=EXCLUDED.stream_nsid AND appview_jetstream_checkpoints.filter_fingerprint=EXCLUDED.filter_fingerprint AND appview_jetstream_checkpoints.cursor_kind=EXCLUDED.cursor_kind AND appview_jetstream_checkpoints.replay_state<>'snapshot_complete' AND appview_jetstream_checkpoints.replay_before_seq IS NULL
        RETURNING 1
        """, logger: logger)
      for try await _ in rows { return }
      throw IngressError.sourceDiscontinuity
    }
  }
}
