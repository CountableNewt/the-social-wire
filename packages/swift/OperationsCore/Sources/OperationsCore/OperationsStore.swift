import Foundation

public protocol OperationsStore: Actor {
  func ping() async throws
  func upsertServiceState(_ state: OperationsServiceState) async throws
  func listServiceStates() async throws -> [OperationsServiceState]

  func fetchStreamState(source: String) async throws -> IngestionStreamState?
  func markStreamConnected(source: String, at: Date) async throws
  func markStreamDisconnected(source: String, reason: String, at: Date) async throws
  func markStreamReceived(source: String, cursor: Int64, eventAt: Date?, receivedAt: Date, queueDepth: Int) async throws
  func markStreamCommitted(source: String, cursor: Int64, eventAt: Date?, committedAt: Date, queueDepth: Int) async throws
  func recordRecoveryFailure(jobId: String?, identityHash: String, collection: String, operation: String, cursor: Int64?, errorCategory: String, at: Date) async throws

  func createGap(source: String, startCursor: Int64?, endCursor: Int64?, reason: String, collections: [String], detectedAt: Date) async throws -> IngestionGap
  func listGaps(limit: Int) async throws -> [IngestionGap]
  func updateGap(id: String, status: IngestionGapStatus, operatorDid: String, at: Date) async throws

  func estimateBackfill(_ request: BackfillDryRunRequest) async throws -> BackfillDryRunResponse
  func createBackfill(_ request: CreateBackfillRequest, operatorDid: String, at: Date) async throws -> BackfillJob
  func listBackfills(limit: Int) async throws -> [BackfillJob]
  func fetchBackfill(id: String) async throws -> BackfillJob?
  func updateBackfillStatus(id: String, status: BackfillJobStatus, operatorDid: String, at: Date) async throws
  func claimNextBackfill(workerId: String, leaseUntil: Date, at: Date) async throws -> BackfillJob?
  func checkpointBackfill(id: String, cursor: Int64?, processed: Int, failed: Int, reconciled: Int, leaseUntil: Date, at: Date) async throws

  func listAlerts(limit: Int) async throws -> [OperationsAlert]
  func openAlert(rule: String, severity: String, summary: String, evidence: [String: String], runbookSlug: String, at: Date) async throws -> OperationsAlert
  func updateAlertStatus(id: String, status: OperationsAlertStatus, operatorDid: String, at: Date) async throws
  func recordAlertDelivery(id: String, error: String?, at: Date) async throws
  func listTraceSpans(limit: Int, traceId: String?) async throws -> [TraceSpan]
  func recordTraceSpan(_ span: TraceSpan) async throws
  func recordMetric(_ sample: OperationsMetricSample) async throws
  func recordEvent(_ event: OperationsEvent) async throws
  func recordAudit(operatorDid: String, action: String, targetType: String, targetId: String?, note: String?, at: Date) async throws
}

public extension OperationsStore {
  func overview(at: Date = Date()) async throws -> OperationsOverview {
    async let services = listServiceStates()
    async let stream = fetchStreamState(source: "jetstream")
    async let gaps = listGaps(limit: 20)
    async let backfills = listBackfills(limit: 20)
    async let alerts = listAlerts(limit: 20)
    async let traces = listTraceSpans(limit: 20, traceId: nil)
    return try await OperationsOverview(
      services: services,
      ingestion: stream,
      gaps: gaps,
      backfills: backfills,
      alerts: alerts,
      recentTraces: traces,
      refreshedAt: at
    )
  }
}
