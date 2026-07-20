import Foundation
import Logging
import PostgresNIO

public actor PostgresOperationsStore: OperationsStore {
  private let pool: PostgresClient
  private let logger: Logger
  private let encoder = JSONEncoder()
  private let decoder = JSONDecoder()

  public init(pool: PostgresClient, logger: Logger) {
    self.pool = pool
    self.logger = logger
  }

  public func ping() async throws {
    let rows = try await pool.query("SELECT 1", logger: logger)
    for try await _ in rows { return }
  }

  public func fetchDatabaseObservability() async throws -> DatabaseObservabilitySnapshot? {
    let summaryRows = try await pool.query(
      """
      SELECT
        pg_database_size(current_database())::bigint,
        numbackends::bigint,
        current_setting('max_connections')::bigint,
        (xact_commit + xact_rollback)::bigint,
        CASE
          WHEN (blks_hit + blks_read) = 0 THEN 1::double precision
          ELSE blks_hit::double precision / (blks_hit + blks_read)::double precision
        END,
        stats_reset
      FROM pg_stat_database
      WHERE datname = current_database()
      """,
      logger: logger
    )
    var summary: (Int64, Int64, Int64, Int64, Double, Date?)?
    for try await row in summaryRows {
      summary = try row.decode((Int64, Int64, Int64, Int64, Double, Date?).self)
      break
    }
    guard let summary else { return nil }

    let recordCountRows = try await pool.query(
      "SELECT COALESCE(SUM(n_live_tup), 0)::bigint FROM pg_stat_user_tables",
      logger: logger
    )
    var estimatedRecords: Int64 = 0
    for try await row in recordCountRows {
      estimatedRecords = try row.decode(Int64.self)
      break
    }

    let tableRows = try await pool.query(
      """
      SELECT schemaname, relname, n_live_tup::bigint
      FROM pg_stat_user_tables
      ORDER BY n_live_tup DESC, schemaname, relname
      LIMIT 10
      """,
      logger: logger
    )
    var topTables: [DatabaseTableRecordCount] = []
    for try await row in tableRows {
      let value = try row.decode((String, String, Int64).self)
      topTables.append(DatabaseTableRecordCount(schema: value.0, table: value.1, estimatedRecords: value.2))
    }

    return DatabaseObservabilitySnapshot(
      databaseSizeBytes: summary.0,
      activeConnections: summary.1,
      maxConnections: summary.2,
      transactionsTotal: summary.3,
      estimatedRecords: estimatedRecords,
      cacheHitRatio: summary.4,
      statsResetAt: summary.5,
      topTables: topTables
    )
  }

  public func upsertServiceState(_ state: OperationsServiceState) async throws {
    let dependencies = try json(state.dependencyState)
    try await pool.query(
      """
      INSERT INTO operations_service_state
        (service, environment, instance_id, liveness, readiness, freshness, completeness,
         dependency_state, version, started_at, heartbeat_at)
      VALUES
        (\(state.service), \(state.environment), \(state.instanceId), \(state.liveness.rawValue),
         \(state.readiness.rawValue), \(state.freshness.rawValue), \(state.completeness.rawValue),
         \(dependencies)::jsonb, \(state.version), \(state.startedAt), \(state.heartbeatAt))
      ON CONFLICT (service, environment, instance_id) DO UPDATE SET
        liveness = EXCLUDED.liveness,
        readiness = EXCLUDED.readiness,
        freshness = EXCLUDED.freshness,
        completeness = EXCLUDED.completeness,
        dependency_state = EXCLUDED.dependency_state,
        version = EXCLUDED.version,
        heartbeat_at = EXCLUDED.heartbeat_at
      """,
      logger: logger
    )
  }

  public func listServiceStates() async throws -> [OperationsServiceState] {
    let rows = try await pool.query(
      """
      SELECT service, environment, instance_id, liveness, readiness, freshness, completeness,
             dependency_state::text, version, started_at, heartbeat_at
      FROM operations_service_state
      ORDER BY service, environment, heartbeat_at DESC
      """,
      logger: logger
    )
    var result: [OperationsServiceState] = []
    for try await row in rows {
      let decoded = try row.decode(
        (String, String, String, String, String, String, String, String, String?, Date, Date).self
      )
      result.append(
        OperationsServiceState(
          service: decoded.0,
          environment: decoded.1,
          instanceId: decoded.2,
          liveness: OperationsHealthState(rawValue: decoded.3) ?? .unknown,
          readiness: OperationsHealthState(rawValue: decoded.4) ?? .unknown,
          freshness: OperationsHealthState(rawValue: decoded.5) ?? .unknown,
          completeness: OperationsHealthState(rawValue: decoded.6) ?? .unknown,
          dependencyState: decodeJSON([String: String].self, from: decoded.7) ?? [:],
          version: decoded.8,
          startedAt: decoded.9,
          heartbeatAt: decoded.10
        )
      )
    }
    return result
  }

  public func fetchStreamState(source: String) async throws -> IngestionStreamState? {
    let rows = try await pool.query(
      """
      SELECT source, connection_state, connected_at, last_disconnect_at, last_disconnect_reason,
             last_received_cursor, last_received_event_at, last_received_at,
             last_committed_cursor, last_committed_event_at, last_committed_at,
             queue_depth, heartbeat_at
      FROM appview_ingestion_stream_state
      WHERE source = \(source)
      LIMIT 1
      """,
      logger: logger
    )
    for try await row in rows {
      let value = try row.decode(
        (String, String, Date?, Date?, String?, Int64?, Date?, Date?, Int64?, Date?, Date?, Int, Date).self
      )
      return IngestionStreamState(
        source: value.0,
        connectionState: IngestionConnectionState(rawValue: value.1) ?? .unknown,
        connectedAt: value.2,
        lastDisconnectAt: value.3,
        lastDisconnectReason: value.4,
        lastReceivedCursor: value.5,
        lastReceivedEventAt: value.6,
        lastReceivedAt: value.7,
        lastCommittedCursor: value.8,
        lastCommittedEventAt: value.9,
        lastCommittedAt: value.10,
        queueDepth: value.11,
        heartbeatAt: value.12
      )
    }
    return nil
  }

  public func markStreamConnected(source: String, at: Date) async throws {
    try await pool.query(
      """
      INSERT INTO appview_ingestion_stream_state (source, connection_state, connected_at, heartbeat_at)
      VALUES (\(source), 'connected', \(at), \(at))
      ON CONFLICT (source) DO UPDATE SET
        connection_state = 'connected', connected_at = EXCLUDED.connected_at, heartbeat_at = EXCLUDED.heartbeat_at
      """,
      logger: logger
    )
  }

  public func markStreamDisconnected(source: String, reason: String, at: Date) async throws {
    try await pool.query(
      """
      INSERT INTO appview_ingestion_stream_state
        (source, connection_state, last_disconnect_at, last_disconnect_reason, heartbeat_at)
      VALUES (\(source), 'disconnected', \(at), \(String(reason.prefix(256))), \(at))
      ON CONFLICT (source) DO UPDATE SET
        connection_state = 'disconnected',
        last_disconnect_at = EXCLUDED.last_disconnect_at,
        last_disconnect_reason = EXCLUDED.last_disconnect_reason,
        heartbeat_at = EXCLUDED.heartbeat_at
      """,
      logger: logger
    )
  }

  public func markStreamReceived(
    source: String,
    cursor: Int64,
    eventAt: Date?,
    receivedAt: Date,
    queueDepth: Int
  ) async throws {
    try await pool.query(
      """
      INSERT INTO appview_ingestion_stream_state
        (source, connection_state, last_received_cursor, last_received_event_at, last_received_at, queue_depth, heartbeat_at)
      VALUES (\(source), 'connected', \(cursor), \(eventAt), \(receivedAt), \(queueDepth), \(receivedAt))
      ON CONFLICT (source) DO UPDATE SET
        connection_state = 'connected',
        last_received_cursor = GREATEST(COALESCE(appview_ingestion_stream_state.last_received_cursor, -1), EXCLUDED.last_received_cursor),
        last_received_event_at = CASE
          WHEN appview_ingestion_stream_state.last_received_cursor IS NULL
            OR EXCLUDED.last_received_cursor >= appview_ingestion_stream_state.last_received_cursor
          THEN EXCLUDED.last_received_event_at ELSE appview_ingestion_stream_state.last_received_event_at END,
        last_received_at = CASE
          WHEN appview_ingestion_stream_state.last_received_cursor IS NULL
            OR EXCLUDED.last_received_cursor >= appview_ingestion_stream_state.last_received_cursor
          THEN EXCLUDED.last_received_at ELSE appview_ingestion_stream_state.last_received_at END,
        queue_depth = EXCLUDED.queue_depth,
        heartbeat_at = EXCLUDED.heartbeat_at
      """,
      logger: logger
    )
  }

  public func markStreamCommitted(
    source: String,
    cursor: Int64,
    eventAt: Date?,
    committedAt: Date,
    queueDepth: Int
  ) async throws {
    try await pool.query(
      """
      INSERT INTO appview_ingestion_stream_state
        (source, last_committed_cursor, last_committed_event_at, last_committed_at, queue_depth, heartbeat_at)
      VALUES (\(source), \(cursor), \(eventAt), \(committedAt), \(queueDepth), \(committedAt))
      ON CONFLICT (source) DO UPDATE SET
        last_committed_cursor = GREATEST(COALESCE(appview_ingestion_stream_state.last_committed_cursor, -1), EXCLUDED.last_committed_cursor),
        last_committed_event_at = CASE
          WHEN appview_ingestion_stream_state.last_committed_cursor IS NULL
            OR EXCLUDED.last_committed_cursor >= appview_ingestion_stream_state.last_committed_cursor
          THEN EXCLUDED.last_committed_event_at ELSE appview_ingestion_stream_state.last_committed_event_at END,
        last_committed_at = CASE
          WHEN appview_ingestion_stream_state.last_committed_cursor IS NULL
            OR EXCLUDED.last_committed_cursor >= appview_ingestion_stream_state.last_committed_cursor
          THEN EXCLUDED.last_committed_at ELSE appview_ingestion_stream_state.last_committed_at END,
        queue_depth = EXCLUDED.queue_depth,
        heartbeat_at = EXCLUDED.heartbeat_at
      """,
      logger: logger
    )
  }

  public func recordRecoveryFailure(
    jobId: String?,
    identityHash: String,
    collection: String,
    operation: String,
    cursor: Int64?,
    errorCategory: String,
    at: Date
  ) async throws {
    let id = UUID().uuidString.lowercased()
    let expiresAt = at.addingTimeInterval(30 * 86_400)
    try await pool.query(
      """
      INSERT INTO appview_recovery_failures
        (id, job_id, source, record_identifier_hash, collection, operation, cursor, error_type,
         retry_count, first_failed_at, last_failed_at, expires_at)
      VALUES
        (\(id), \(jobId), 'jetstream', \(identityHash), \(String(collection.prefix(128))),
         \(String(operation.prefix(32))), \(cursor), \(String(errorCategory.prefix(64))), 0, \(at), \(at), \(expiresAt))
      """,
      logger: logger
    )
  }

  public func createGap(
    source: String,
    startCursor: Int64?,
    endCursor: Int64?,
    reason: String,
    collections: [String],
    detectedAt: Date
  ) async throws -> IngestionGap {
    let id = UUID().uuidString.lowercased()
    let collectionJSON = try json(collections)
    try await pool.query(
      """
      INSERT INTO appview_ingestion_gaps
        (id, source, start_cursor, end_cursor, reason, status, collections, detected_at, updated_at)
      VALUES
        (\(id), \(source), \(startCursor), \(endCursor), \(String(reason.prefix(128))), 'suspected',
         \(collectionJSON)::jsonb, \(detectedAt), \(detectedAt))
      """,
      logger: logger
    )
    guard let created = try await listGaps(limit: 250).first(where: { $0.id == id }) else {
      throw OperationsStoreError.missingCreatedRecord
    }
    return created
  }

  public func listGaps(limit: Int) async throws -> [IngestionGap] {
    let limit = max(1, min(limit, 250))
    let rows = try await pool.query(
      """
      SELECT id, source, start_cursor, end_cursor, start_time, end_time, reason, status,
             collections::text, detected_at, updated_at, backfill_job_id,
             discovered_count, processed_count, failed_count, reconciled_count
      FROM appview_ingestion_gaps
      ORDER BY detected_at DESC
      LIMIT \(limit)
      """,
      logger: logger
    )
    var result: [IngestionGap] = []
    for try await row in rows {
      let value = try row.decode(
        (String, String, Int64?, Int64?, Date?, Date?, String, String, String, Date, Date, String?, Int, Int, Int, Int).self
      )
      result.append(
        IngestionGap(
          id: value.0,
          source: value.1,
          startCursor: value.2,
          endCursor: value.3,
          startTime: value.4,
          endTime: value.5,
          reason: value.6,
          status: IngestionGapStatus(rawValue: value.7) ?? .suspected,
          collections: decodeJSON([String].self, from: value.8) ?? [],
          detectedAt: value.9,
          updatedAt: value.10,
          backfillJobId: value.11,
          discoveredCount: value.12,
          processedCount: value.13,
          failedCount: value.14,
          reconciledCount: value.15
        )
      )
    }
    return result
  }

  public func updateGap(id: String, status: IngestionGapStatus, operatorDid: String, at: Date) async throws {
    try await pool.query(
      "UPDATE appview_ingestion_gaps SET status = \(status.rawValue), updated_at = \(at) WHERE id = \(id)",
      logger: logger
    )
    try await recordAudit(
      operatorDid: operatorDid,
      action: "gap.status_changed",
      targetType: "gap",
      targetId: id,
      note: status.rawValue,
      at: at
    )
  }

  public func estimateBackfill(_ request: BackfillDryRunRequest) async throws -> BackfillDryRunResponse {
    let estimate: Int
    switch request.sourceMode {
    case .jetstreamReplay:
      let delta = max(0, (request.endCursor ?? 0) - (request.startCursor ?? 0))
      let seconds = Double(delta) / 1_000_000
      estimate = min(Int.max / 2, max(0, Int(seconds * 250)))
    case .pdsReconciliation:
      estimate = request.authorDids.count * max(1, request.collections.count) * 100
    }
    let duration = request.rateLimit > 0 ? Int(ceil(Double(estimate) / Double(request.rateLimit))) : 0
    return BackfillDryRunResponse(
      estimatedCount: estimate,
      estimatedDurationSeconds: duration,
      snapshotEndCursor: request.endCursor,
      conflicts: [],
      unresolvedDeletesWarning: request.sourceMode == .pdsReconciliation
    )
  }

  public func createBackfill(
    _ request: CreateBackfillRequest,
    operatorDid: String,
    at: Date
  ) async throws -> BackfillJob {
    let id = UUID().uuidString.lowercased()
    let collections = try json(request.dryRun.collections)
    let authorDids = try json(request.dryRun.authorDids)
    try await pool.query(
      """
      INSERT INTO appview_backfill_jobs
        (id, gap_id, source_mode, status, start_cursor, end_cursor, checkpoint_cursor,
         collections, author_dids, batch_size, rate_limit, max_concurrency, estimated_count,
         requested_by_did, audit_note, created_at, updated_at)
      VALUES
        (\(id), \(request.dryRun.gapId), \(request.dryRun.sourceMode.rawValue), 'queued',
         \(request.dryRun.startCursor), \(request.dryRun.endCursor), \(request.dryRun.startCursor),
         \(collections)::jsonb, \(authorDids)::jsonb, \(request.dryRun.batchSize),
         \(request.dryRun.rateLimit), \(request.dryRun.maxConcurrency), \(request.expectedEstimate),
         \(operatorDid), \(String(request.auditNote.prefix(280))), \(at), \(at))
      """,
      logger: logger
    )
    if let gapId = request.dryRun.gapId {
      try await pool.query(
        "UPDATE appview_ingestion_gaps SET status = 'backfill_queued', backfill_job_id = \(id), updated_at = \(at) WHERE id = \(gapId)",
        logger: logger
      )
    }
    try await recordAudit(
      operatorDid: operatorDid,
      action: "backfill.queued",
      targetType: "backfill",
      targetId: id,
      note: request.auditNote,
      at: at
    )
    guard let job = try await fetchBackfill(id: id) else { throw OperationsStoreError.missingCreatedRecord }
    return job
  }

  public func listBackfills(limit: Int) async throws -> [BackfillJob] {
    let limit = max(1, min(limit, 250))
    let rows = try await pool.query(
      """
      SELECT id, gap_id, source_mode, status, start_cursor, end_cursor, checkpoint_cursor,
             collections::text, author_dids::text, batch_size, rate_limit, max_concurrency,
             estimated_count, processed_count, failed_count, reconciled_count,
             requested_by_did, audit_note, lease_owner, lease_expires_at,
             created_at, updated_at, completed_at
      FROM appview_backfill_jobs
      ORDER BY created_at DESC
      LIMIT \(limit)
      """,
      logger: logger
    )
    return try await decodeBackfills(rows)
  }

  public func fetchBackfill(id: String) async throws -> BackfillJob? {
    let rows = try await pool.query(
      """
      SELECT id, gap_id, source_mode, status, start_cursor, end_cursor, checkpoint_cursor,
             collections::text, author_dids::text, batch_size, rate_limit, max_concurrency,
             estimated_count, processed_count, failed_count, reconciled_count,
             requested_by_did, audit_note, lease_owner, lease_expires_at,
             created_at, updated_at, completed_at
      FROM appview_backfill_jobs
      WHERE id = \(id)
      LIMIT 1
      """,
      logger: logger
    )
    return try await decodeBackfills(rows).first
  }

  public func updateBackfillStatus(
    id: String,
    status: BackfillJobStatus,
    operatorDid: String,
    at: Date
  ) async throws {
    let completedAt: Date? = [.completed, .failed, .cancelled].contains(status) ? at : nil
    try await pool.query(
      """
      UPDATE appview_backfill_jobs
      SET status = \(status.rawValue), updated_at = \(at), completed_at = COALESCE(\(completedAt), completed_at),
          lease_owner = CASE WHEN \(status.rawValue) = 'running' THEN lease_owner ELSE NULL END,
          lease_expires_at = CASE WHEN \(status.rawValue) = 'running' THEN lease_expires_at ELSE NULL END
      WHERE id = \(id)
      """,
      logger: logger
    )
    try await recordAudit(
      operatorDid: operatorDid,
      action: "backfill.\(status.rawValue)",
      targetType: "backfill",
      targetId: id,
      note: nil,
      at: at
    )
  }

  public func claimNextBackfill(workerId: String, leaseUntil: Date, at: Date) async throws -> BackfillJob? {
    let rows = try await pool.query(
      """
      UPDATE appview_backfill_jobs
      SET status = 'running', lease_owner = \(workerId), lease_expires_at = \(leaseUntil), updated_at = \(at)
      WHERE id = (
        SELECT id FROM appview_backfill_jobs
        WHERE status IN ('queued', 'running')
          AND (lease_expires_at IS NULL OR lease_expires_at < \(at))
        ORDER BY created_at
        FOR UPDATE SKIP LOCKED
        LIMIT 1
      )
      RETURNING id, gap_id, source_mode, status, start_cursor, end_cursor, checkpoint_cursor,
                collections::text, author_dids::text, batch_size, rate_limit, max_concurrency,
                estimated_count, processed_count, failed_count, reconciled_count,
                requested_by_did, audit_note, lease_owner, lease_expires_at,
                created_at, updated_at, completed_at
      """,
      logger: logger
    )
    return try await decodeBackfills(rows).first
  }

  public func checkpointBackfill(
    id: String,
    cursor: Int64?,
    processed: Int,
    failed: Int,
    reconciled: Int,
    leaseUntil: Date,
    at: Date
  ) async throws {
    try await pool.query(
      """
      UPDATE appview_backfill_jobs SET
        checkpoint_cursor = \(cursor), processed_count = \(processed), failed_count = \(failed),
        reconciled_count = \(reconciled), lease_expires_at = \(leaseUntil), updated_at = \(at)
      WHERE id = \(id) AND status = 'running'
      """,
      logger: logger
    )
  }

  public func listAlerts(limit: Int) async throws -> [OperationsAlert] {
    let limit = max(1, min(limit, 250))
    let rows = try await pool.query(
      """
      SELECT id, rule, severity, status, summary, evidence::text, runbook_slug,
             opened_at, updated_at, acknowledged_by_did, resolved_by_did,
             delivery_attempts, last_delivery_error
      FROM operations_alerts
      ORDER BY opened_at DESC
      LIMIT \(limit)
      """,
      logger: logger
    )
    var result: [OperationsAlert] = []
    for try await row in rows {
      let value = try row.decode(
        (String, String, String, String, String, String, String, Date, Date, String?, String?, Int, String?).self
      )
      result.append(
        OperationsAlert(
          id: value.0,
          rule: value.1,
          severity: value.2,
          status: OperationsAlertStatus(rawValue: value.3) ?? .open,
          summary: value.4,
          evidence: decodeJSON([String: String].self, from: value.5) ?? [:],
          runbookSlug: value.6,
          openedAt: value.7,
          updatedAt: value.8,
          acknowledgedByDid: value.9,
          resolvedByDid: value.10,
          deliveryAttempts: value.11,
          lastDeliveryError: value.12
        )
      )
    }
    return result
  }

  public func openAlert(
    rule: String,
    severity: String,
    summary: String,
    evidence: [String: String],
    runbookSlug: String,
    at: Date
  ) async throws -> OperationsAlert {
    if let existing = try await listAlerts(limit: 250).first(where: { $0.rule == rule && $0.status != .resolved }) {
      return existing
    }
    let id = UUID().uuidString.lowercased()
    let evidenceJSON = try json(OperationsRedactor.boundedAttributes(evidence))
    try await pool.query(
      """
      INSERT INTO operations_alerts
        (id, rule, severity, status, summary, evidence, runbook_slug, opened_at, updated_at)
      VALUES
        (\(id), \(String(rule.prefix(128))), \(String(severity.prefix(32))), 'open',
         \(String(summary.prefix(512))), \(evidenceJSON)::jsonb, \(String(runbookSlug.prefix(128))), \(at), \(at))
      """,
      logger: logger
    )
    guard let created = try await listAlerts(limit: 250).first(where: { $0.id == id }) else {
      throw OperationsStoreError.missingCreatedRecord
    }
    return created
  }

  public func updateAlertStatus(
    id: String,
    status: OperationsAlertStatus,
    operatorDid: String,
    at: Date
  ) async throws {
    let acknowledged: String? = status == .acknowledged ? operatorDid : nil
    let resolved: String? = status == .resolved ? operatorDid : nil
    try await pool.query(
      """
      UPDATE operations_alerts SET status = \(status.rawValue), updated_at = \(at),
        acknowledged_by_did = COALESCE(\(acknowledged), acknowledged_by_did),
        resolved_by_did = COALESCE(\(resolved), resolved_by_did)
      WHERE id = \(id)
      """,
      logger: logger
    )
    try await recordAudit(
      operatorDid: operatorDid,
      action: "alert.\(status.rawValue)",
      targetType: "alert",
      targetId: id,
      note: nil,
      at: at
    )
  }

  public func recordAlertDelivery(id: String, error: String?, at: Date) async throws {
    try await pool.query(
      """
      UPDATE operations_alerts SET delivery_attempts = delivery_attempts + 1,
        last_delivery_error = \(error.map { String($0.prefix(256)) }), updated_at = \(at)
      WHERE id = \(id)
      """,
      logger: logger
    )
  }

  public func listTraceSpans(limit: Int, traceId: String?) async throws -> [TraceSpan] {
    let limit = max(1, min(limit, 500))
    let rows: PostgresRowSequence
    if let traceId {
      rows = try await pool.query(
        """
        SELECT id, trace_id, parent_span_id, service, name, started_at, duration_ms, status,
               attributes::text, expires_at
        FROM operations_trace_spans
        WHERE trace_id = \(traceId)
        ORDER BY started_at DESC
        LIMIT \(limit)
        """,
        logger: logger
      )
    } else {
      rows = try await pool.query(
        """
        SELECT id, trace_id, parent_span_id, service, name, started_at, duration_ms, status,
               attributes::text, expires_at
        FROM operations_trace_spans
        ORDER BY started_at DESC
        LIMIT \(limit)
        """,
        logger: logger
      )
    }
    var result: [TraceSpan] = []
    for try await row in rows {
      let value = try row.decode((String, String, String?, String, String, Date, Double, String, String, Date).self)
      result.append(
        TraceSpan(
          id: value.0,
          traceId: value.1,
          parentSpanId: value.2,
          service: value.3,
          name: value.4,
          startedAt: value.5,
          durationMs: value.6,
          status: value.7,
          attributes: decodeJSON([String: String].self, from: value.8) ?? [:],
          expiresAt: value.9
        )
      )
    }
    return result
  }

  public func listTraceSpans(startAt: Date, endAt: Date, limit: Int) async throws -> [TraceSpan] {
    let limit = max(1, min(limit, 500))
    let rows = try await pool.query(
      """
      SELECT id, trace_id, parent_span_id, service, name, started_at, duration_ms, status,
             attributes::text, expires_at
      FROM operations_trace_spans
      WHERE started_at >= \(startAt) AND started_at <= \(endAt)
      ORDER BY started_at ASC
      LIMIT \(limit)
      """,
      logger: logger
    )
    var result: [TraceSpan] = []
    for try await row in rows {
      let value = try row.decode((String, String, String?, String, String, Date, Double, String, String, Date).self)
      result.append(
        TraceSpan(
          id: value.0,
          traceId: value.1,
          parentSpanId: value.2,
          service: value.3,
          name: value.4,
          startedAt: value.5,
          durationMs: value.6,
          status: value.7,
          attributes: decodeJSON([String: String].self, from: value.8) ?? [:],
          expiresAt: value.9
        )
      )
    }
    return result
  }

  public func recordTraceSpan(_ span: TraceSpan) async throws {
    let attributes = try json(OperationsRedactor.boundedAttributes(span.attributes))
    try await pool.query(
      """
      INSERT INTO operations_trace_spans
        (id, trace_id, parent_span_id, service, name, started_at, duration_ms, status, attributes, expires_at)
      VALUES
        (\(span.id), \(span.traceId), \(span.parentSpanId), \(span.service), \(span.name),
         \(span.startedAt), \(span.durationMs), \(span.status), \(attributes)::jsonb, \(span.expiresAt))
      ON CONFLICT (id) DO NOTHING
      """,
      logger: logger
    )
  }

  public func recordMetric(_ sample: OperationsMetricSample) async throws {
    let dimensions = OperationsRedactor.boundedAttributes(sample.dimensions)
    let dimensionsJSON = try json(dimensions)
    let dimensionsKey = dimensions.sorted { $0.key < $1.key }.map { "\($0.key)=\($0.value)" }.joined(separator: "&")
    let dimensionsHash = OperationsRedactor.hashIdentity(dimensionsKey)
    let bucket = Date(timeIntervalSince1970: floor(sample.recordedAt.timeIntervalSince1970 / 60) * 60)
    let expiresAt = bucket.addingTimeInterval(90 * 86_400)
    try await pool.query(
      """
      INSERT INTO operations_metric_rollups
        (bucket_start, metric_name, dimensions_hash, dimensions, sample_count, value_sum,
         value_min, value_max, histogram_buckets, expires_at)
      VALUES
        (\(bucket), \(String(sample.name.prefix(160))), \(dimensionsHash), \(dimensionsJSON)::jsonb,
         1, \(sample.value), \(sample.value), \(sample.value), '{}'::jsonb, \(expiresAt))
      ON CONFLICT (bucket_start, metric_name, dimensions_hash) DO UPDATE SET
        sample_count = operations_metric_rollups.sample_count + 1,
        value_sum = operations_metric_rollups.value_sum + EXCLUDED.value_sum,
        value_min = LEAST(operations_metric_rollups.value_min, EXCLUDED.value_min),
        value_max = GREATEST(operations_metric_rollups.value_max, EXCLUDED.value_max)
      """,
      logger: logger
    )
  }

  public func listGapInvestigationEvents(startAt: Date, endAt: Date, limit: Int) async throws -> [OperationsEvent] {
    let limit = max(1, min(limit, 500))
    let rows = try await pool.query(
      """
      SELECT id, service, environment, instance_id, event_name, occurred_at,
             request_id, trace_id, attributes::text
      FROM operations_events
      WHERE occurred_at >= \(startAt) AND occurred_at <= \(endAt)
        AND event_name IN ('jetstream.disconnected', 'jetstream.connected', 'commit.failed')
      ORDER BY occurred_at ASC
      LIMIT \(limit)
      """,
      logger: logger
    )
    var result: [OperationsEvent] = []
    for try await row in rows {
      let value = try row.decode((String, String, String, String, String, Date, String?, String?, String).self)
      result.append(
        OperationsEvent(
          id: value.0,
          service: value.1,
          environment: value.2,
          instanceId: value.3,
          name: value.4,
          occurredAt: value.5,
          requestId: value.6,
          traceId: value.7,
          attributes: decodeJSON([String: String].self, from: value.8) ?? [:]
        )
      )
    }
    return result
  }

  public func recordEvent(_ event: OperationsEvent) async throws {
    let attributes = try json(OperationsRedactor.boundedAttributes(event.attributes))
    let expiresAt = event.occurredAt.addingTimeInterval(30 * 86_400)
    try await pool.query(
      """
      INSERT INTO operations_events
        (id, service, environment, instance_id, event_name, occurred_at, request_id, trace_id, attributes, expires_at)
      VALUES
        (\(event.id), \(event.service), \(event.environment), \(event.instanceId),
         \(String(event.name.prefix(160))), \(event.occurredAt), \(event.requestId), \(event.traceId),
         \(attributes)::jsonb, \(expiresAt))
      ON CONFLICT (id) DO NOTHING
      """,
      logger: logger
    )
  }

  public func recordAudit(
    operatorDid: String,
    action: String,
    targetType: String,
    targetId: String?,
    note: String?,
    at: Date
  ) async throws {
    let id = UUID().uuidString.lowercased()
    let expiresAt = Calendar.current.date(byAdding: .day, value: 365, to: at) ?? at.addingTimeInterval(365 * 86_400)
    try await pool.query(
      """
      INSERT INTO operations_audit_events
        (id, operator_did, action, target_type, target_id, note, occurred_at, expires_at)
      VALUES
        (\(id), \(operatorDid), \(String(action.prefix(128))), \(String(targetType.prefix(64))),
         \(targetId), \(note.map { String($0.prefix(280)) }), \(at), \(expiresAt))
      """,
      logger: logger
    )
  }

  private func decodeBackfills(_ rows: PostgresRowSequence) async throws -> [BackfillJob] {
    var result: [BackfillJob] = []
    for try await row in rows {
      let value = try row.decode(
        (String, String?, String, String, Int64?, Int64?, Int64?, String, String, Int, Int, Int, Int, Int, Int, Int, String, String, String?, Date?, Date, Date, Date?).self
      )
      result.append(
        BackfillJob(
          id: value.0,
          gapId: value.1,
          sourceMode: BackfillSourceMode(rawValue: value.2) ?? .jetstreamReplay,
          status: BackfillJobStatus(rawValue: value.3) ?? .queued,
          startCursor: value.4,
          endCursor: value.5,
          checkpointCursor: value.6,
          collections: decodeJSON([String].self, from: value.7) ?? [],
          authorDids: decodeJSON([String].self, from: value.8) ?? [],
          batchSize: value.9,
          rateLimit: value.10,
          maxConcurrency: value.11,
          estimatedCount: value.12,
          processedCount: value.13,
          failedCount: value.14,
          reconciledCount: value.15,
          requestedByDid: value.16,
          auditNote: value.17,
          leaseOwner: value.18,
          leaseExpiresAt: value.19,
          createdAt: value.20,
          updatedAt: value.21,
          completedAt: value.22
        )
      )
    }
    return result
  }

  private func json<T: Encodable>(_ value: T) throws -> String {
    let data = try encoder.encode(value)
    guard let string = String(data: data, encoding: .utf8) else { throw OperationsStoreError.jsonEncoding }
    return string
  }

  private func decodeJSON<T: Decodable>(_ type: T.Type, from string: String) -> T? {
    guard let data = string.data(using: .utf8) else { return nil }
    return try? decoder.decode(type, from: data)
  }
}

public enum OperationsStoreError: Error {
  case missingCreatedRecord
  case jsonEncoding
}
