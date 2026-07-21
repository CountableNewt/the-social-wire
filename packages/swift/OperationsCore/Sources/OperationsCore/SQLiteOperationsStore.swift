import Foundation
@preconcurrency import GRDB
import Logging

public actor SQLiteOperationsStore: OperationsStore {
  private let db: DatabasePool
  private let logger: Logger
  private let encoder = JSONEncoder()
  private let decoder = JSONDecoder()

  public init(path: String, logger: Logger) throws {
    self.logger = logger
    var configuration = Configuration()
    configuration.label = "com.thesocialwire.operations"
    db = try DatabasePool(path: path, configuration: configuration)
    try db.write { database in try Self.migrate(database) }
  }

  public func ping() async throws {
    _ = try await db.read { database in try Int.fetchOne(database, sql: "SELECT 1") }
  }

  public func upsertServiceState(_ state: OperationsServiceState) async throws {
    let dependencies = try json(state.dependencyState)
    try await db.write { database in
      try database.execute(
        sql: """
          INSERT INTO operations_service_state
            (service, environment, instance_id, liveness, readiness, freshness, completeness,
             dependency_state, version, started_at, heartbeat_at)
          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
          ON CONFLICT (service, environment, instance_id) DO UPDATE SET
            liveness = excluded.liveness, readiness = excluded.readiness,
            freshness = excluded.freshness, completeness = excluded.completeness,
            dependency_state = excluded.dependency_state, version = excluded.version,
            heartbeat_at = excluded.heartbeat_at
          """,
        arguments: [
          state.service, state.environment, state.instanceId, state.liveness.rawValue,
          state.readiness.rawValue, state.freshness.rawValue, state.completeness.rawValue,
          dependencies, state.version, Self.iso(state.startedAt), Self.iso(state.heartbeatAt),
        ]
      )
    }
  }

  public func listServiceStates() async throws -> [OperationsServiceState] {
    try await db.read { database in
      try Row.fetchAll(
        database,
        sql:
          "SELECT * FROM operations_service_state ORDER BY service, environment, heartbeat_at DESC"
      ).compactMap { row in
        guard
          let liveness = OperationsHealthState(rawValue: row["liveness"]),
          let readiness = OperationsHealthState(rawValue: row["readiness"]),
          let freshness = OperationsHealthState(rawValue: row["freshness"]),
          let completeness = OperationsHealthState(rawValue: row["completeness"]),
          let startedAt = Self.date(row["started_at"]),
          let heartbeatAt = Self.date(row["heartbeat_at"])
        else { return nil }
        let dependencyJSON: String = row["dependency_state"]
        return OperationsServiceState(
          service: row["service"],
          environment: row["environment"],
          instanceId: row["instance_id"],
          liveness: liveness,
          readiness: readiness,
          freshness: freshness,
          completeness: completeness,
          dependencyState: Self.decode([String: String].self, dependencyJSON) ?? [:],
          version: row["version"],
          startedAt: startedAt,
          heartbeatAt: heartbeatAt
        )
      }
    }
  }

  public func fetchStreamState(source: String) async throws -> IngestionStreamState? {
    try await db.read { database in
      guard
        let row = try Row.fetchOne(
          database,
          sql: "SELECT * FROM appview_ingestion_stream_state WHERE source = ? LIMIT 1",
          arguments: [source]
        )
      else { return nil }
      return Self.streamState(row)
    }
  }

  public func markStreamConnected(source: String, at: Date) async throws {
    try await db.write { database in
      try database.execute(
        sql: """
          INSERT INTO appview_ingestion_stream_state (source, connection_state, connected_at, heartbeat_at)
          VALUES (?, 'connected', ?, ?)
          ON CONFLICT (source) DO UPDATE SET
            connection_state = 'connected', connected_at = excluded.connected_at, heartbeat_at = excluded.heartbeat_at
          """,
        arguments: [source, Self.iso(at), Self.iso(at)]
      )
    }
  }

  public func markStreamDisconnected(source: String, reason: String, at: Date) async throws {
    try await db.write { database in
      try database.execute(
        sql: """
          INSERT INTO appview_ingestion_stream_state
            (source, connection_state, last_disconnect_at, last_disconnect_reason, heartbeat_at)
          VALUES (?, 'disconnected', ?, ?, ?)
          ON CONFLICT (source) DO UPDATE SET
            connection_state = 'disconnected', last_disconnect_at = excluded.last_disconnect_at,
            last_disconnect_reason = excluded.last_disconnect_reason, heartbeat_at = excluded.heartbeat_at
          """,
        arguments: [source, Self.iso(at), String(reason.prefix(256)), Self.iso(at)]
      )
    }
  }

  public func markStreamReceived(
    source: String,
    cursor: Int64,
    eventAt: Date?,
    receivedAt: Date,
    queueDepth: Int
  ) async throws {
    try await db.write { database in
      try database.execute(
        sql: """
          INSERT INTO appview_ingestion_stream_state
            (source, connection_state, last_received_cursor, last_received_event_at,
             last_received_at, queue_depth, heartbeat_at)
          VALUES (?, 'connected', ?, ?, ?, ?, ?)
          ON CONFLICT (source) DO UPDATE SET
            connection_state = 'connected',
            last_received_event_at = CASE WHEN excluded.last_received_cursor >= COALESCE(last_received_cursor, -1)
              THEN excluded.last_received_event_at ELSE last_received_event_at END,
            last_received_at = CASE WHEN excluded.last_received_cursor >= COALESCE(last_received_cursor, -1)
              THEN excluded.last_received_at ELSE last_received_at END,
            last_received_cursor = MAX(COALESCE(last_received_cursor, -1), excluded.last_received_cursor),
            queue_depth = excluded.queue_depth, heartbeat_at = excluded.heartbeat_at
          """,
        arguments: [
          source, cursor, eventAt.map(Self.iso), Self.iso(receivedAt), queueDepth,
          Self.iso(receivedAt),
        ]
      )
    }
  }

  public func markStreamCommitted(
    source: String,
    cursor: Int64,
    eventAt: Date?,
    committedAt: Date,
    queueDepth: Int
  ) async throws {
    try await db.write { database in
      try database.execute(
        sql: """
          INSERT INTO appview_ingestion_stream_state
            (source, last_committed_cursor, last_committed_event_at, last_committed_at,
             queue_depth, heartbeat_at)
          VALUES (?, ?, ?, ?, ?, ?)
          ON CONFLICT (source) DO UPDATE SET
            last_committed_event_at = CASE WHEN excluded.last_committed_cursor >= COALESCE(last_committed_cursor, -1)
              THEN excluded.last_committed_event_at ELSE last_committed_event_at END,
            last_committed_at = CASE WHEN excluded.last_committed_cursor >= COALESCE(last_committed_cursor, -1)
              THEN excluded.last_committed_at ELSE last_committed_at END,
            last_committed_cursor = MAX(COALESCE(last_committed_cursor, -1), excluded.last_committed_cursor),
            queue_depth = excluded.queue_depth, heartbeat_at = excluded.heartbeat_at
          """,
        arguments: [
          source, cursor, eventAt.map(Self.iso), Self.iso(committedAt), queueDepth,
          Self.iso(committedAt),
        ]
      )
    }
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
    try await db.write { database in
      try database.execute(
        sql: """
          INSERT INTO appview_recovery_failures
            (id, job_id, source, record_identifier_hash, collection, operation, cursor, error_type,
             retry_count, first_failed_at, last_failed_at, expires_at)
          VALUES (?, ?, 'jetstream', ?, ?, ?, ?, ?, 0, ?, ?, ?)
          """,
        arguments: [
          UUID().uuidString.lowercased(), jobId, identityHash, String(collection.prefix(128)),
          String(operation.prefix(32)), cursor, String(errorCategory.prefix(64)), Self.iso(at),
          Self.iso(at), Self.iso(at.addingTimeInterval(30 * 86_400)),
        ]
      )
    }
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
    try await db.write { database in
      try database.execute(
        sql: """
          INSERT INTO appview_ingestion_gaps
            (id, source, start_cursor, end_cursor, reason, status, collections, detected_at, updated_at)
          VALUES (?, ?, ?, ?, ?, 'suspected', ?, ?, ?)
          """,
        arguments: [
          id, source, startCursor, endCursor, String(reason.prefix(128)), collectionJSON,
          Self.iso(detectedAt), Self.iso(detectedAt),
        ]
      )
    }
    guard let gap = try await listGaps(limit: 250).first(where: { $0.id == id }) else {
      throw OperationsStoreError.missingCreatedRecord
    }
    return gap
  }

  public func listGaps(limit: Int) async throws -> [IngestionGap] {
    let limit = max(1, min(limit, 250))
    return try await db.read { database in
      try Row.fetchAll(
        database,
        sql: "SELECT * FROM appview_ingestion_gaps ORDER BY detected_at DESC LIMIT ?",
        arguments: [limit]
      ).compactMap(Self.gap)
    }
  }

  public func updateGap(id: String, status: IngestionGapStatus, operatorDid: String, at: Date)
    async throws
  {
    try await db.write { database in
      try database.execute(
        sql: "UPDATE appview_ingestion_gaps SET status = ?, updated_at = ? WHERE id = ?",
        arguments: [status.rawValue, Self.iso(at), id]
      )
    }
    try await recordAudit(
      operatorDid: operatorDid,
      action: "gap.status_changed",
      targetType: "gap",
      targetId: id,
      note: status.rawValue,
      at: at
    )
  }

  public func estimateBackfill(_ request: BackfillDryRunRequest) async throws
    -> BackfillDryRunResponse
  {
    let estimate: Int
    switch request.sourceMode {
    case .jetstreamReplay:
      let delta = max(0, (request.endCursor ?? 0) - (request.startCursor ?? 0))
      estimate = max(0, Int(Double(delta) / 1_000_000 * 250))
    case .pdsReconciliation:
      estimate = request.authorDids.count * max(1, request.collections.count) * 100
    }
    return BackfillDryRunResponse(
      estimatedCount: estimate,
      estimatedDurationSeconds: request.rateLimit > 0
        ? Int(ceil(Double(estimate) / Double(request.rateLimit))) : 0,
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
    let collectionsJSON = try json(request.dryRun.collections)
    let authorDidsJSON = try json(request.dryRun.authorDids)
    try await db.write { database in
      try database.execute(
        sql: """
          INSERT INTO appview_backfill_jobs
            (id, gap_id, source_mode, status, start_cursor, end_cursor, checkpoint_cursor,
             collections, author_dids, batch_size, rate_limit, max_concurrency, estimated_count,
             requested_by_did, audit_note, created_at, updated_at)
          VALUES (?, ?, ?, 'queued', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
          """,
        arguments: [
          id, request.dryRun.gapId, request.dryRun.sourceMode.rawValue,
          request.dryRun.startCursor, request.dryRun.endCursor, request.dryRun.startCursor,
          collectionsJSON, authorDidsJSON,
          request.dryRun.batchSize, request.dryRun.rateLimit, request.dryRun.maxConcurrency,
          request.expectedEstimate, operatorDid, String((request.auditNote ?? "").prefix(280)),
          Self.iso(at), Self.iso(at),
        ]
      )
      if let gapId = request.dryRun.gapId {
        try database.execute(
          sql:
            "UPDATE appview_ingestion_gaps SET status = 'backfill_queued', backfill_job_id = ?, updated_at = ? WHERE id = ?",
          arguments: [id, Self.iso(at), gapId]
        )
      }
    }
    try await recordAudit(
      operatorDid: operatorDid,
      action: "backfill.queued",
      targetType: "backfill",
      targetId: id,
      note: request.auditNote,
      at: at
    )
    guard let job = try await fetchBackfill(id: id) else {
      throw OperationsStoreError.missingCreatedRecord
    }
    return job
  }

  public func listBackfills(limit: Int) async throws -> [BackfillJob] {
    let limit = max(1, min(limit, 250))
    return try await db.read { database in
      try Row.fetchAll(
        database,
        sql: "SELECT * FROM appview_backfill_jobs ORDER BY created_at DESC LIMIT ?",
        arguments: [limit]
      ).compactMap(Self.backfill)
    }
  }

  public func fetchBackfill(id: String) async throws -> BackfillJob? {
    try await db.read { database in
      try Row.fetchOne(
        database,
        sql: "SELECT * FROM appview_backfill_jobs WHERE id = ? LIMIT 1",
        arguments: [id]
      ).flatMap(Self.backfill)
    }
  }

  public func updateBackfillStatus(
    id: String,
    status: BackfillJobStatus,
    operatorDid: String,
    failureReason: String?,
    at: Date
  ) async throws {
    let completed =
      [BackfillJobStatus.completed, .failed, .cancelled].contains(status) ? Self.iso(at) : nil
    try await db.write { database in
      try database.execute(
        sql: """
          UPDATE appview_backfill_jobs SET status = ?, updated_at = ?,
            completed_at = COALESCE(?, completed_at),
            failure_reason = CASE WHEN ? = 'failed' THEN ? ELSE failure_reason END,
            lease_owner = CASE WHEN ? = 'running' THEN lease_owner ELSE NULL END,
            lease_expires_at = CASE WHEN ? = 'running' THEN lease_expires_at ELSE NULL END
          WHERE id = ?
          """,
        arguments: [
          status.rawValue, Self.iso(at), completed, status.rawValue,
          failureReason.map { String($0.prefix(160)) }, status.rawValue, status.rawValue, id,
        ]
      )
    }
    try await recordAudit(
      operatorDid: operatorDid,
      action: "backfill.\(status.rawValue)",
      targetType: "backfill",
      targetId: id,
      note: nil,
      at: at
    )
  }

  public func claimNextBackfill(workerId: String, leaseUntil: Date, at: Date) async throws
    -> BackfillJob?
  {
    try await db.write { database in
      guard
        let id = try String.fetchOne(
          database,
          sql: """
            SELECT id FROM appview_backfill_jobs
            WHERE status IN ('queued', 'running') AND (lease_expires_at IS NULL OR lease_expires_at < ?)
            ORDER BY created_at LIMIT 1
            """,
          arguments: [Self.iso(at)]
        )
      else { return nil }
      try database.execute(
        sql: """
          UPDATE appview_backfill_jobs SET status = 'running', lease_owner = ?, lease_expires_at = ?, updated_at = ?
          WHERE id = ?
          """,
        arguments: [workerId, Self.iso(leaseUntil), Self.iso(at), id]
      )
      return try Row.fetchOne(
        database,
        sql: "SELECT * FROM appview_backfill_jobs WHERE id = ?",
        arguments: [id]
      ).flatMap(Self.backfill)
    }
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
    try await db.write { database in
      try database.execute(
        sql: """
          UPDATE appview_backfill_jobs SET checkpoint_cursor = ?, processed_count = ?, failed_count = ?,
            reconciled_count = ?, lease_expires_at = ?, updated_at = ? WHERE id = ? AND status = 'running'
          """,
        arguments: [cursor, processed, failed, reconciled, Self.iso(leaseUntil), Self.iso(at), id]
      )
    }
  }

  public func listAlerts(limit: Int) async throws -> [OperationsAlert] {
    let limit = max(1, min(limit, 250))
    return try await db.read { database in
      try Row.fetchAll(
        database,
        sql: "SELECT * FROM operations_alerts ORDER BY opened_at DESC LIMIT ?",
        arguments: [limit]
      ).compactMap(Self.alert)
    }
  }

  public func openAlert(
    rule: String,
    severity: String,
    summary: String,
    evidence: [String: String],
    runbookSlug: String,
    at: Date
  ) async throws -> OperationsAlert {
    if let existing = try await listAlerts(limit: 250).first(where: {
      $0.rule == rule && $0.status != .resolved
    }) {
      return existing
    }
    let id = UUID().uuidString.lowercased()
    let evidenceJSON = try json(OperationsRedactor.boundedAttributes(evidence))
    try await db.write { database in
      try database.execute(
        sql: """
          INSERT INTO operations_alerts
            (id, rule, severity, status, summary, evidence, runbook_slug, opened_at, updated_at)
          VALUES (?, ?, ?, 'open', ?, ?, ?, ?, ?)
          """,
        arguments: [
          id, String(rule.prefix(128)), String(severity.prefix(32)), String(summary.prefix(512)),
          evidenceJSON, String(runbookSlug.prefix(128)),
          Self.iso(at), Self.iso(at),
        ]
      )
    }
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
    try await db.write { database in
      try database.execute(
        sql: """
          UPDATE operations_alerts SET status = ?, updated_at = ?,
            acknowledged_by_did = COALESCE(?, acknowledged_by_did),
            resolved_by_did = COALESCE(?, resolved_by_did)
          WHERE id = ?
          """,
        arguments: [
          status.rawValue, Self.iso(at), status == .acknowledged ? operatorDid : nil,
          status == .resolved ? operatorDid : nil, id,
        ]
      )
    }
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
    try await db.write { database in
      try database.execute(
        sql: """
          UPDATE operations_alerts SET delivery_attempts = delivery_attempts + 1,
            last_delivery_error = ?, updated_at = ? WHERE id = ?
          """,
        arguments: [error.map { String($0.prefix(256)) }, Self.iso(at), id]
      )
    }
  }

  public func listTraceSpans(limit: Int, traceId: String?) async throws -> [TraceSpan] {
    let limit = max(1, min(limit, 500))
    return try await db.read { database in
      let rows: [Row]
      if let traceId {
        rows = try Row.fetchAll(
          database,
          sql:
            "SELECT * FROM operations_trace_spans WHERE trace_id = ? ORDER BY started_at DESC LIMIT ?",
          arguments: [traceId, limit]
        )
      } else {
        rows = try Row.fetchAll(
          database,
          sql: "SELECT * FROM operations_trace_spans ORDER BY started_at DESC LIMIT ?",
          arguments: [limit]
        )
      }
      return rows.compactMap(Self.span)
    }
  }

  public func listTraceSpans(startAt: Date, endAt: Date, limit: Int) async throws -> [TraceSpan] {
    let limit = max(1, min(limit, 500))
    return try await db.read { database in
      try Row.fetchAll(
        database,
        sql: """
          SELECT * FROM operations_trace_spans
          WHERE started_at >= ? AND started_at <= ?
          ORDER BY started_at ASC
          LIMIT ?
          """,
        arguments: [Self.iso(startAt), Self.iso(endAt), limit]
      ).compactMap(Self.span)
    }
  }

  public func recordTraceSpan(_ span: TraceSpan) async throws {
    let attributes = try json(OperationsRedactor.boundedAttributes(span.attributes))
    try await db.write { database in
      try database.execute(
        sql: """
          INSERT OR IGNORE INTO operations_trace_spans
            (id, trace_id, parent_span_id, service, name, started_at, duration_ms, status, attributes, expires_at)
          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
          """,
        arguments: [
          span.id, span.traceId, span.parentSpanId, span.service, span.name,
          Self.iso(span.startedAt),
          span.durationMs, span.status, attributes, Self.iso(span.expiresAt),
        ]
      )
    }
  }

  public func recordMetric(_ sample: OperationsMetricSample) async throws {
    let dimensions = OperationsRedactor.boundedAttributes(sample.dimensions)
    let dimensionsJSON = try json(dimensions)
    let dimensionsKey = dimensions.sorted { $0.key < $1.key }.map { "\($0.key)=\($0.value)" }
      .joined(separator: "&")
    let dimensionsHash = OperationsRedactor.hashIdentity(dimensionsKey)
    let bucket = Date(
      timeIntervalSince1970: floor(sample.recordedAt.timeIntervalSince1970 / 60) * 60)
    try await db.write { database in
      try database.execute(
        sql: """
          INSERT INTO operations_metric_rollups
            (bucket_start, metric_name, dimensions_hash, dimensions, sample_count, value_sum,
             value_min, value_max, histogram_buckets, expires_at)
          VALUES (?, ?, ?, ?, 1, ?, ?, ?, '{}', ?)
          ON CONFLICT (bucket_start, metric_name, dimensions_hash) DO UPDATE SET
            sample_count = sample_count + 1, value_sum = value_sum + excluded.value_sum,
            value_min = MIN(value_min, excluded.value_min), value_max = MAX(value_max, excluded.value_max)
          """,
        arguments: [
          Self.iso(bucket), String(sample.name.prefix(160)), dimensionsHash, dimensionsJSON,
          sample.value, sample.value, sample.value,
          Self.iso(bucket.addingTimeInterval(90 * 86_400)),
        ]
      )
    }
  }

  public func listMetricRollups(startAt: Date, endAt: Date, limit: Int) async throws
    -> [OperationsMetricRollup]
  {
    let limit = max(1, min(limit, 10_000))
    return try await db.read { database in
      try Row.fetchAll(
        database,
        sql: """
          SELECT bucket_start, metric_name, dimensions, sample_count, value_sum, value_min, value_max
          FROM operations_metric_rollups
          WHERE bucket_start >= ? AND bucket_start <= ?
          ORDER BY bucket_start ASC, metric_name ASC, dimensions_hash ASC
          LIMIT ?
          """,
        arguments: [Self.iso(startAt), Self.iso(endAt), limit]
      ).compactMap { row in
        guard let bucketStart = Self.date(row["bucket_start"]) else { return nil }
        let dimensionsJSON: String = row["dimensions"]
        return OperationsMetricRollup(
          bucketStart: bucketStart,
          metricName: row["metric_name"],
          dimensions: Self.decode([String: String].self, dimensionsJSON) ?? [:],
          sampleCount: row["sample_count"],
          valueSum: row["value_sum"],
          valueMin: row["value_min"],
          valueMax: row["value_max"]
        )
      }
    }
  }

  public func listGapInvestigationEvents(startAt: Date, endAt: Date, limit: Int) async throws
    -> [OperationsEvent]
  {
    let limit = max(1, min(limit, 500))
    return try await db.read { database in
      try Row.fetchAll(
        database,
        sql: """
          SELECT * FROM operations_events
          WHERE occurred_at >= ? AND occurred_at <= ?
            AND event_name IN ('jetstream.disconnected', 'jetstream.connected', 'commit.failed')
          ORDER BY occurred_at ASC
          LIMIT ?
          """,
        arguments: [Self.iso(startAt), Self.iso(endAt), limit]
      ).compactMap(Self.event)
    }
  }

  public func recordEvent(_ event: OperationsEvent) async throws {
    let attributes = try json(OperationsRedactor.boundedAttributes(event.attributes))
    try await db.write { database in
      try database.execute(
        sql: """
          INSERT OR IGNORE INTO operations_events
            (id, service, environment, instance_id, event_name, occurred_at, request_id, trace_id, attributes, expires_at)
          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
          """,
        arguments: [
          event.id, event.service, event.environment, event.instanceId,
          String(event.name.prefix(160)),
          Self.iso(event.occurredAt), event.requestId, event.traceId, attributes,
          Self.iso(event.occurredAt.addingTimeInterval(30 * 86_400)),
        ]
      )
    }
  }

  public func recordAudit(
    operatorDid: String,
    action: String,
    targetType: String,
    targetId: String?,
    note: String?,
    at: Date
  ) async throws {
    try await db.write { database in
      try database.execute(
        sql: """
          INSERT INTO operations_audit_events
            (id, operator_did, action, target_type, target_id, note, occurred_at, expires_at)
          VALUES (?, ?, ?, ?, ?, ?, ?, ?)
          """,
        arguments: [
          UUID().uuidString.lowercased(), operatorDid, String(action.prefix(128)),
          String(targetType.prefix(64)), targetId, note.map { String($0.prefix(280)) },
          Self.iso(at),
          Self.iso(at.addingTimeInterval(365 * 86_400)),
        ]
      )
    }
  }

  private static func migrate(_ db: Database) throws {
    try db.execute(sql: Schema.sqlite)
    let backfillColumns = try Row.fetchAll(db, sql: "PRAGMA table_info(appview_backfill_jobs)")
      .map { row -> String in row["name"] }
    if !backfillColumns.contains("failure_reason") {
      try db.execute(sql: "ALTER TABLE appview_backfill_jobs ADD COLUMN failure_reason TEXT")
    }
  }

  private func json<T: Encodable>(_ value: T) throws -> String {
    let data = try encoder.encode(value)
    guard let string = String(data: data, encoding: .utf8) else {
      throw OperationsStoreError.jsonEncoding
    }
    return string
  }

  private static func decode<T: Decodable>(_ type: T.Type, _ string: String) -> T? {
    guard let data = string.data(using: .utf8) else { return nil }
    return try? JSONDecoder().decode(type, from: data)
  }

  private static func iso(_ date: Date) -> String {
    let formatter = ISO8601DateFormatter()
    formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    return formatter.string(from: date)
  }

  private static func date(_ value: String?) -> Date? {
    guard let value else { return nil }
    let formatter = ISO8601DateFormatter()
    formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    return formatter.date(from: value) ?? ISO8601DateFormatter().date(from: value)
  }

  private static func streamState(_ row: Row) -> IngestionStreamState? {
    guard
      let state = IngestionConnectionState(rawValue: row["connection_state"]),
      let heartbeatAt = date(row["heartbeat_at"])
    else { return nil }
    return IngestionStreamState(
      source: row["source"],
      connectionState: state,
      connectedAt: date(row["connected_at"]),
      lastDisconnectAt: date(row["last_disconnect_at"]),
      lastDisconnectReason: row["last_disconnect_reason"],
      lastReceivedCursor: row["last_received_cursor"],
      lastReceivedEventAt: date(row["last_received_event_at"]),
      lastReceivedAt: date(row["last_received_at"]),
      lastCommittedCursor: row["last_committed_cursor"],
      lastCommittedEventAt: date(row["last_committed_event_at"]),
      lastCommittedAt: date(row["last_committed_at"]),
      queueDepth: row["queue_depth"],
      heartbeatAt: heartbeatAt
    )
  }

  private static func gap(_ row: Row) -> IngestionGap? {
    guard
      let status = IngestionGapStatus(rawValue: row["status"]),
      let detectedAt = date(row["detected_at"]),
      let updatedAt = date(row["updated_at"])
    else { return nil }
    let collections: String = row["collections"]
    return IngestionGap(
      id: row["id"], source: row["source"], startCursor: row["start_cursor"],
      endCursor: row["end_cursor"], startTime: date(row["start_time"]),
      endTime: date(row["end_time"]),
      reason: row["reason"], status: status, collections: decode([String].self, collections) ?? [],
      detectedAt: detectedAt, updatedAt: updatedAt, backfillJobId: row["backfill_job_id"],
      discoveredCount: row["discovered_count"], processedCount: row["processed_count"],
      failedCount: row["failed_count"], reconciledCount: row["reconciled_count"]
    )
  }

  private static func backfill(_ row: Row) -> BackfillJob? {
    guard
      let sourceMode = BackfillSourceMode(rawValue: row["source_mode"]),
      let status = BackfillJobStatus(rawValue: row["status"]),
      let createdAt = date(row["created_at"]),
      let updatedAt = date(row["updated_at"])
    else { return nil }
    let collections: String = row["collections"]
    let authorDids: String = row["author_dids"]
    return BackfillJob(
      id: row["id"], gapId: row["gap_id"], sourceMode: sourceMode, status: status,
      startCursor: row["start_cursor"], endCursor: row["end_cursor"],
      checkpointCursor: row["checkpoint_cursor"],
      collections: decode([String].self, collections) ?? [],
      authorDids: decode([String].self, authorDids) ?? [],
      batchSize: row["batch_size"], rateLimit: row["rate_limit"],
      maxConcurrency: row["max_concurrency"],
      estimatedCount: row["estimated_count"], processedCount: row["processed_count"],
      failedCount: row["failed_count"], reconciledCount: row["reconciled_count"],
      requestedByDid: row["requested_by_did"], auditNote: row["audit_note"],
      failureReason: row["failure_reason"], leaseOwner: row["lease_owner"],
      leaseExpiresAt: date(row["lease_expires_at"]), createdAt: createdAt, updatedAt: updatedAt,
      completedAt: date(row["completed_at"])
    )
  }

  private static func alert(_ row: Row) -> OperationsAlert? {
    guard
      let status = OperationsAlertStatus(rawValue: row["status"]),
      let openedAt = date(row["opened_at"]),
      let updatedAt = date(row["updated_at"])
    else { return nil }
    let evidence: String = row["evidence"]
    return OperationsAlert(
      id: row["id"], rule: row["rule"], severity: row["severity"], status: status,
      summary: row["summary"], evidence: decode([String: String].self, evidence) ?? [:],
      runbookSlug: row["runbook_slug"], openedAt: openedAt, updatedAt: updatedAt,
      acknowledgedByDid: row["acknowledged_by_did"], resolvedByDid: row["resolved_by_did"],
      deliveryAttempts: row["delivery_attempts"], lastDeliveryError: row["last_delivery_error"]
    )
  }

  private static func span(_ row: Row) -> TraceSpan? {
    guard let startedAt = date(row["started_at"]), let expiresAt = date(row["expires_at"]) else {
      return nil
    }
    let attributes: String = row["attributes"]
    return TraceSpan(
      id: row["id"], traceId: row["trace_id"], parentSpanId: row["parent_span_id"],
      service: row["service"], name: row["name"], startedAt: startedAt,
      durationMs: row["duration_ms"], status: row["status"],
      attributes: decode([String: String].self, attributes) ?? [:], expiresAt: expiresAt
    )
  }

  private static func event(_ row: Row) -> OperationsEvent? {
    guard let occurredAt = date(row["occurred_at"]) else { return nil }
    let attributes: String = row["attributes"]
    return OperationsEvent(
      id: row["id"],
      service: row["service"],
      environment: row["environment"],
      instanceId: row["instance_id"],
      name: row["event_name"],
      occurredAt: occurredAt,
      requestId: row["request_id"],
      traceId: row["trace_id"],
      attributes: decode([String: String].self, attributes) ?? [:]
    )
  }
}

private enum Schema {
  static let sqlite = """
    CREATE TABLE IF NOT EXISTS operations_service_state (
      service TEXT NOT NULL, environment TEXT NOT NULL, instance_id TEXT NOT NULL,
      liveness TEXT NOT NULL, readiness TEXT NOT NULL, freshness TEXT NOT NULL, completeness TEXT NOT NULL,
      dependency_state TEXT NOT NULL DEFAULT '{}', version TEXT, started_at TEXT NOT NULL, heartbeat_at TEXT NOT NULL,
      PRIMARY KEY (service, environment, instance_id)
    );
    CREATE TABLE IF NOT EXISTS operations_trace_spans (
      id TEXT PRIMARY KEY, trace_id TEXT NOT NULL, parent_span_id TEXT, service TEXT NOT NULL,
      name TEXT NOT NULL, started_at TEXT NOT NULL, duration_ms REAL NOT NULL, status TEXT NOT NULL,
      attributes TEXT NOT NULL DEFAULT '{}', expires_at TEXT NOT NULL
    );
    CREATE INDEX IF NOT EXISTS idx_operations_trace_spans_trace ON operations_trace_spans (trace_id, started_at);
    CREATE TABLE IF NOT EXISTS operations_metric_rollups (
      bucket_start TEXT NOT NULL, metric_name TEXT NOT NULL, dimensions_hash TEXT NOT NULL,
      dimensions TEXT NOT NULL DEFAULT '{}', sample_count INTEGER NOT NULL DEFAULT 0,
      value_sum REAL NOT NULL DEFAULT 0, value_min REAL, value_max REAL,
      histogram_buckets TEXT NOT NULL DEFAULT '{}', expires_at TEXT NOT NULL,
      PRIMARY KEY (bucket_start, metric_name, dimensions_hash)
    );
    CREATE TABLE IF NOT EXISTS operations_events (
      id TEXT PRIMARY KEY, service TEXT NOT NULL, environment TEXT NOT NULL, instance_id TEXT NOT NULL,
      event_name TEXT NOT NULL, occurred_at TEXT NOT NULL, request_id TEXT, trace_id TEXT,
      attributes TEXT NOT NULL DEFAULT '{}', expires_at TEXT NOT NULL
    );
    CREATE TABLE IF NOT EXISTS operations_audit_events (
      id TEXT PRIMARY KEY, operator_did TEXT NOT NULL, action TEXT NOT NULL, target_type TEXT NOT NULL,
      target_id TEXT, note TEXT, occurred_at TEXT NOT NULL, expires_at TEXT NOT NULL
    );
    CREATE TABLE IF NOT EXISTS appview_ingestion_stream_state (
      source TEXT PRIMARY KEY, connection_state TEXT NOT NULL DEFAULT 'unknown', connected_at TEXT,
      last_disconnect_at TEXT, last_disconnect_reason TEXT, last_received_cursor INTEGER,
      last_received_event_at TEXT, last_received_at TEXT, last_committed_cursor INTEGER,
      last_committed_event_at TEXT, last_committed_at TEXT, queue_depth INTEGER NOT NULL DEFAULT 0,
      heartbeat_at TEXT NOT NULL
    );
    CREATE TABLE IF NOT EXISTS appview_ingestion_gaps (
      id TEXT PRIMARY KEY, source TEXT NOT NULL, start_cursor INTEGER, end_cursor INTEGER,
      start_time TEXT, end_time TEXT, reason TEXT NOT NULL, status TEXT NOT NULL,
      collections TEXT NOT NULL DEFAULT '[]', detected_at TEXT NOT NULL, updated_at TEXT NOT NULL,
      backfill_job_id TEXT, discovered_count INTEGER NOT NULL DEFAULT 0,
      processed_count INTEGER NOT NULL DEFAULT 0, failed_count INTEGER NOT NULL DEFAULT 0,
      reconciled_count INTEGER NOT NULL DEFAULT 0
    );
    CREATE TABLE IF NOT EXISTS appview_backfill_jobs (
      id TEXT PRIMARY KEY, gap_id TEXT, source_mode TEXT NOT NULL, status TEXT NOT NULL,
      start_cursor INTEGER, end_cursor INTEGER, checkpoint_cursor INTEGER,
      collections TEXT NOT NULL DEFAULT '[]', author_dids TEXT NOT NULL DEFAULT '[]',
      batch_size INTEGER NOT NULL, rate_limit INTEGER NOT NULL, max_concurrency INTEGER NOT NULL,
      estimated_count INTEGER NOT NULL DEFAULT 0, processed_count INTEGER NOT NULL DEFAULT 0,
      failed_count INTEGER NOT NULL DEFAULT 0, reconciled_count INTEGER NOT NULL DEFAULT 0,
      requested_by_did TEXT NOT NULL, audit_note TEXT NOT NULL, failure_reason TEXT,
      lease_owner TEXT, lease_expires_at TEXT,
      created_at TEXT NOT NULL, updated_at TEXT NOT NULL, completed_at TEXT
    );
    CREATE TABLE IF NOT EXISTS appview_recovery_failures (
      id TEXT PRIMARY KEY, job_id TEXT, source TEXT NOT NULL, record_identifier_hash TEXT NOT NULL,
      collection TEXT, operation TEXT, cursor INTEGER, error_type TEXT NOT NULL,
      retry_count INTEGER NOT NULL DEFAULT 0, first_failed_at TEXT NOT NULL, last_failed_at TEXT NOT NULL,
      resolved_at TEXT, expires_at TEXT NOT NULL
    );
    CREATE TABLE IF NOT EXISTS operations_alerts (
      id TEXT PRIMARY KEY, rule TEXT NOT NULL, severity TEXT NOT NULL, status TEXT NOT NULL,
      summary TEXT NOT NULL, evidence TEXT NOT NULL DEFAULT '{}', runbook_slug TEXT NOT NULL,
      opened_at TEXT NOT NULL, updated_at TEXT NOT NULL, acknowledged_by_did TEXT,
      resolved_by_did TEXT, delivery_attempts INTEGER NOT NULL DEFAULT 0, last_delivery_error TEXT
    );
    """
}
