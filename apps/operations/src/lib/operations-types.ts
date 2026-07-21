export type Health = "healthy" | "degraded" | "unhealthy" | "unknown"
export type ServiceState = {
  service: string
  environment: string
  instanceId: string
  liveness: Health
  readiness: Health
  freshness: Health
  completeness: Health
  dependencyState: Record<string, string>
  version?: string
  startedAt: string
  heartbeatAt: string
}
export type StreamState = {
  source: string
  connectionState: "connected" | "disconnected" | "reconnecting" | "unknown"
  connectedAt?: string
  lastDisconnectAt?: string
  lastDisconnectReason?: string
  lastReceivedCursor?: number
  lastReceivedEventAt?: string
  lastReceivedAt?: string
  lastCommittedCursor?: number
  lastCommittedEventAt?: string
  lastCommittedAt?: string
  queueDepth: number
  heartbeatAt: string
}
export type Gap = {
  id: string
  source: string
  startCursor?: number
  endCursor?: number
  startTime?: string
  endTime?: string
  reason: string
  status: "suspected" | "confirmed" | "backfill_queued" | "backfilling" | "resolved" | "ignored"
  collections: string[]
  detectedAt: string
  updatedAt: string
  backfillJobId?: string
  discoveredCount: number
  processedCount: number
  failedCount: number
  reconciledCount: number
}
export type GapCauseAssessment = {
  title: string
  confidence: "high" | "medium" | "low" | "insufficient"
  summary: string
  evidenceIds: string[]
  limitations: string[]
}
export type GapInvestigationEvidence = {
  id: string
  kind: "gap" | "stream" | "indexing" | "service" | "alert" | "trace"
  occurredAt: string
  service: string
  title: string
  detail: string
  attributes: Record<string, string>
  traceId?: string
}
export type GapInvestigation = {
  gap: Gap
  windowStart: string
  windowEnd: string
  assessment: GapCauseAssessment
  evidence: GapInvestigationEvidence[]
  recommendedActions: string[]
}
export type Backfill = {
  id: string
  gapId?: string
  sourceMode: "jetstream_replay" | "pds_reconciliation"
  status: "queued" | "running" | "paused" | "completed" | "failed" | "cancelled"
  startCursor?: number
  endCursor?: number
  checkpointCursor?: number
  collections: string[]
  authorDids: string[]
  batchSize: number
  rateLimit: number
  maxConcurrency: number
  estimatedCount: number
  processedCount: number
  failedCount: number
  reconciledCount: number
  requestedByDid: string
  auditNote: string
  leaseOwner?: string
  leaseExpiresAt?: string
  createdAt: string
  updatedAt: string
  completedAt?: string
}
export type Alert = {
  id: string
  rule: string
  severity: string
  status: "open" | "acknowledged" | "resolved"
  summary: string
  evidence: Record<string, string>
  runbookSlug: string
  openedAt: string
  updatedAt: string
  acknowledgedByDid?: string
  resolvedByDid?: string
  deliveryAttempts: number
  lastDeliveryError?: string
}
export type Span = {
  id: string
  traceId: string
  parentSpanId?: string
  service: string
  name: string
  startedAt: string
  durationMs: number
  status: string
  attributes: Record<string, string>
  expiresAt: string
}
export type DatabaseTableRecordCount = { schema: string; table: string; estimatedRecords: number }
export type DatabaseObservabilitySnapshot = {
  databaseSizeBytes: number
  activeConnections: number
  maxConnections: number
  transactionsTotal: number
  estimatedRecords: number
  cacheHitRatio: number
  statsResetAt?: string
  topTables: DatabaseTableRecordCount[]
}
export type Overview = {
  services: ServiceState[]
  ingestion?: StreamState
  gaps: Gap[]
  backfills: Backfill[]
  alerts: Alert[]
  recentTraces: Span[]
  database?: DatabaseObservabilitySnapshot
  refreshedAt: string
}
export type EnvironmentName = "development" | "production"
export type BackfillDryRun = {
  gapId?: string
  sourceMode: "jetstream_replay" | "pds_reconciliation"
  startCursor?: number
  endCursor?: number
  collections: string[]
  authorDids: string[]
  batchSize: number
  rateLimit: number
  maxConcurrency: number
}
export type DryRunResult = {
  estimatedCount: number
  estimatedDurationSeconds: number
  snapshotEndCursor?: number
  conflicts: string[]
  unresolvedDeletesWarning: boolean
}
