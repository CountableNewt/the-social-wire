import Foundation

public enum OperationsHealthState: String, Codable, Sendable {
  case healthy
  case degraded
  case unhealthy
  case unknown
}

public enum IngestionConnectionState: String, Codable, Sendable {
  case connected
  case disconnected
  case reconnecting
  case unknown
}

public enum IngestionGapStatus: String, Codable, Sendable, CaseIterable {
  case suspected
  case confirmed
  case backfillQueued = "backfill_queued"
  case backfilling
  case resolved
  case ignored
}

public enum BackfillSourceMode: String, Codable, Sendable {
  case jetstreamReplay = "jetstream_replay"
  case pdsReconciliation = "pds_reconciliation"
}

public enum BackfillJobStatus: String, Codable, Sendable, CaseIterable {
  case queued
  case running
  case paused
  case completed
  case failed
  case cancelled
}

public enum OperationsAlertStatus: String, Codable, Sendable {
  case open
  case acknowledged
  case resolved
}

public struct OperationsServiceState: Codable, Sendable {
  public let service: String
  public let environment: String
  public let instanceId: String
  public let liveness: OperationsHealthState
  public let readiness: OperationsHealthState
  public let freshness: OperationsHealthState
  public let completeness: OperationsHealthState
  public let dependencyState: [String: String]
  public let version: String?
  public let startedAt: Date
  public let heartbeatAt: Date

  public init(
    service: String,
    environment: String,
    instanceId: String,
    liveness: OperationsHealthState,
    readiness: OperationsHealthState,
    freshness: OperationsHealthState,
    completeness: OperationsHealthState,
    dependencyState: [String: String] = [:],
    version: String? = nil,
    startedAt: Date,
    heartbeatAt: Date
  ) {
    self.service = service
    self.environment = environment
    self.instanceId = instanceId
    self.liveness = liveness
    self.readiness = readiness
    self.freshness = freshness
    self.completeness = completeness
    self.dependencyState = dependencyState
    self.version = version
    self.startedAt = startedAt
    self.heartbeatAt = heartbeatAt
  }
}

public struct IngestionStreamState: Codable, Sendable {
  public let source: String
  public let connectionState: IngestionConnectionState
  public let connectedAt: Date?
  public let lastDisconnectAt: Date?
  public let lastDisconnectReason: String?
  public let lastReceivedCursor: Int64?
  public let lastReceivedEventAt: Date?
  public let lastReceivedAt: Date?
  public let lastCommittedCursor: Int64?
  public let lastCommittedEventAt: Date?
  public let lastCommittedAt: Date?
  public let queueDepth: Int
  public let heartbeatAt: Date

  public init(
    source: String,
    connectionState: IngestionConnectionState,
    connectedAt: Date? = nil,
    lastDisconnectAt: Date? = nil,
    lastDisconnectReason: String? = nil,
    lastReceivedCursor: Int64? = nil,
    lastReceivedEventAt: Date? = nil,
    lastReceivedAt: Date? = nil,
    lastCommittedCursor: Int64? = nil,
    lastCommittedEventAt: Date? = nil,
    lastCommittedAt: Date? = nil,
    queueDepth: Int = 0,
    heartbeatAt: Date
  ) {
    self.source = source
    self.connectionState = connectionState
    self.connectedAt = connectedAt
    self.lastDisconnectAt = lastDisconnectAt
    self.lastDisconnectReason = lastDisconnectReason
    self.lastReceivedCursor = lastReceivedCursor
    self.lastReceivedEventAt = lastReceivedEventAt
    self.lastReceivedAt = lastReceivedAt
    self.lastCommittedCursor = lastCommittedCursor
    self.lastCommittedEventAt = lastCommittedEventAt
    self.lastCommittedAt = lastCommittedAt
    self.queueDepth = queueDepth
    self.heartbeatAt = heartbeatAt
  }
}

public struct IngestionGap: Codable, Sendable, Identifiable {
  public let id: String
  public let source: String
  public let startCursor: Int64?
  public let endCursor: Int64?
  public let startTime: Date?
  public let endTime: Date?
  public let reason: String
  public let status: IngestionGapStatus
  public let collections: [String]
  public let detectedAt: Date
  public let updatedAt: Date
  public let backfillJobId: String?
  public let discoveredCount: Int
  public let processedCount: Int
  public let failedCount: Int
  public let reconciledCount: Int
}

public struct BackfillJob: Codable, Sendable, Identifiable {
  public let id: String
  public let gapId: String?
  public let sourceMode: BackfillSourceMode
  public let status: BackfillJobStatus
  public let startCursor: Int64?
  public let endCursor: Int64?
  public let checkpointCursor: Int64?
  public let collections: [String]
  public let authorDids: [String]
  public let batchSize: Int
  public let rateLimit: Int
  public let maxConcurrency: Int
  public let estimatedCount: Int
  public let processedCount: Int
  public let failedCount: Int
  public let reconciledCount: Int
  public let requestedByDid: String
  public let auditNote: String
  public let leaseOwner: String?
  public let leaseExpiresAt: Date?
  public let createdAt: Date
  public let updatedAt: Date
  public let completedAt: Date?
}

public struct BackfillDryRunRequest: Codable, Sendable {
  public let gapId: String?
  public let sourceMode: BackfillSourceMode
  public let startCursor: Int64?
  public let endCursor: Int64?
  public let collections: [String]
  public let authorDids: [String]
  public let batchSize: Int
  public let rateLimit: Int
  public let maxConcurrency: Int

  public init(
    gapId: String? = nil,
    sourceMode: BackfillSourceMode,
    startCursor: Int64? = nil,
    endCursor: Int64? = nil,
    collections: [String],
    authorDids: [String] = [],
    batchSize: Int,
    rateLimit: Int,
    maxConcurrency: Int
  ) {
    self.gapId = gapId
    self.sourceMode = sourceMode
    self.startCursor = startCursor
    self.endCursor = endCursor
    self.collections = collections
    self.authorDids = authorDids
    self.batchSize = batchSize
    self.rateLimit = rateLimit
    self.maxConcurrency = maxConcurrency
  }
}

public struct BackfillDryRunResponse: Codable, Sendable {
  public let estimatedCount: Int
  public let estimatedDurationSeconds: Int
  public let snapshotEndCursor: Int64?
  public let conflicts: [String]
  public let unresolvedDeletesWarning: Bool
}

public struct CreateBackfillRequest: Codable, Sendable {
  public let dryRun: BackfillDryRunRequest
  public let expectedEstimate: Int
  public let auditNote: String
}

public struct OperationsAlert: Codable, Sendable, Identifiable {
  public let id: String
  public let rule: String
  public let severity: String
  public let status: OperationsAlertStatus
  public let summary: String
  public let evidence: [String: String]
  public let runbookSlug: String
  public let openedAt: Date
  public let updatedAt: Date
  public let acknowledgedByDid: String?
  public let resolvedByDid: String?
  public let deliveryAttempts: Int
  public let lastDeliveryError: String?
}

public struct TraceSpan: Codable, Sendable, Identifiable {
  public let id: String
  public let traceId: String
  public let parentSpanId: String?
  public let service: String
  public let name: String
  public let startedAt: Date
  public let durationMs: Double
  public let status: String
  public let attributes: [String: String]
  public let expiresAt: Date
}

public struct OperationsOverview: Codable, Sendable {
  public let services: [OperationsServiceState]
  public let ingestion: IngestionStreamState?
  public let gaps: [IngestionGap]
  public let backfills: [BackfillJob]
  public let alerts: [OperationsAlert]
  public let recentTraces: [TraceSpan]
  public let refreshedAt: Date
}
