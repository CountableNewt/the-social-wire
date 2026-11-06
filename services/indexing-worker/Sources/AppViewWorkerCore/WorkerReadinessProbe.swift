import Foundation
import OperationsCore
import ThinAppViewCore

struct WorkerReadinessProbe: Sendable {
  let databaseProbe: @Sendable () async throws -> Void
  let serviceStateProbe: (@Sendable () async throws -> OperationsServiceState?)?
  let now: @Sendable () -> Date
  let mode: ThinAppViewWorkerMode

  init(
    databaseProbe: @escaping @Sendable () async throws -> Void,
    serviceStateProbe: (@Sendable () async throws -> OperationsServiceState?)? = nil,
    now: @escaping @Sendable () -> Date = Date.init,
    mode: ThinAppViewWorkerMode = .ingestion
  ) {
    self.databaseProbe = databaseProbe
    self.serviceStateProbe = serviceStateProbe
    self.now = now
    self.mode = mode
  }

  func run(includingDiagnostics: Bool = false) async throws {
    try await databaseProbe()
    guard let serviceStateProbe else {
      throw WorkerReadinessError.missingIngestionHeartbeat
    }
    guard let state = try await serviceStateProbe() else {
      throw WorkerReadinessError.missingIngestionHeartbeat
    }

    let evaluatedAt = now()
    let heartbeatAge = evaluatedAt.timeIntervalSince(state.heartbeatAt)
    guard heartbeatAge >= -5, heartbeatAge <= 15 else {
      throw failure(
        .staleIngestionHeartbeat,
        state: state,
        at: evaluatedAt,
        includingDiagnostics: includingDiagnostics
      )
    }
    guard state.liveness == .healthy, state.readiness == .healthy else {
      throw failure(
        .ingestionTransportUnhealthy,
        state: state,
        at: evaluatedAt,
        includingDiagnostics: includingDiagnostics
      )
    }

    if mode == .maintenance {
      // Maintenance reports scheduler progress only. It makes no claim about
      // the coverage or freshness of a retired global ingestion pipeline.
      guard state.dependencyState["coordinator_authority"] == "active",
        state.dependencyState["coordinator_role"] == "indexing.appview-coordinator",
        state.dependencyState["worker_mode"] == "maintenance",
        state.dependencyState["worker_role"] == "coordinator",
        state.dependencyState["ingestion_source"] == "disabled",
        state.dependencyState["maintenance_retention"] == "ready",
        ["ready", "disabled"].contains(state.dependencyState["maintenance_rss"] ?? ""),
        state.dependencyState["jetstream_replay"] == "disabled_by_worker_mode",
        state.dependencyState["pds_reconciliation"] == "disabled_by_worker_mode"
      else { throw WorkerReadinessError.maintenanceEvidenceUnavailable }
      return
    }

    // Projection-repair evidence describes the authoritative Tap and durable V2 paths. The
    // legacy Jetstream path used during v1_authoritative/v2_shadow has no repair queue, so its
    // freshness and completeness are intentionally unknown even while its transport is healthy.
    // Keep fail-closed behavior for missing or unrecognized source metadata.
    let projectionQualityRequired =
      switch state.dependencyState["ingestion_source"] {
      case "jetstream": false
      default: true
      }
    if projectionQualityRequired, state.freshness != .healthy {
      throw failure(
        .ingestionFreshnessUnhealthy,
        state: state,
        at: evaluatedAt,
        includingDiagnostics: includingDiagnostics
      )
    }
    if projectionQualityRequired, state.completeness != .healthy {
      throw failure(
        .ingestionCompletenessUnhealthy,
        state: state,
        at: evaluatedAt,
        includingDiagnostics: includingDiagnostics
      )
    }
  }

  private func failure(
    _ reason: WorkerReadinessError,
    state: OperationsServiceState,
    at now: Date,
    includingDiagnostics: Bool
  ) -> any Error {
    guard includingDiagnostics,
      let diagnostics = WorkerReadinessDiagnostics.v2(from: state, at: now)
    else { return reason }
    return WorkerReadinessFailure(reason: reason, diagnostics: diagnostics)
  }
}

enum WorkerReadinessError: Error, Equatable {
  case maintenanceEvidenceUnavailable
  case missingIngestionHeartbeat
  case staleIngestionHeartbeat
  case ingestionTransportUnhealthy
  case ingestionFreshnessUnhealthy
  case ingestionCompletenessUnhealthy
}
