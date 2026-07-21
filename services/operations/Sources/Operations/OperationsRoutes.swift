import Foundation
import GatewayCore
import Hummingbird
import OperationsCore

struct OperationsRoutes {
  let store: any OperationsStore
  let config: OperationsConfiguration

  func register(on group: RouterGroup<GatewayRequestContext>) {
    group.get("/v1/operations/overview") { _, _ async throws -> OperationsOverview in
      try await store.overview()
    }
    group.get("/v1/operations/services") { _, _ async throws -> OperationsServiceListResponse in
      OperationsServiceListResponse(services: try await store.listServiceStates())
    }
    group.get("/v1/operations/ingestion") { _, _ async throws -> IngestionResponse in
      IngestionResponse(
        state: try await store.fetchStreamState(source: "jetstream"),
        gaps: try await store.listGaps(limit: 100))
    }
    group.get("/v1/operations/appview") { _, _ async throws -> AppViewOperationsResponse in
      AppViewOperationsResponse(
        services: try await store.listServiceStates().filter {
          $0.service == "appview" || $0.service == "gateway"
        },
        traces: try await store.listTraceSpans(limit: 100, traceId: nil)
      )
    }
    group.get("/v1/operations/gaps") { _, _ async throws -> GapListResponse in
      GapListResponse(gaps: try await store.listGaps(limit: 250))
    }
    group.get("/v1/operations/gaps/:id/investigation") {
      _, context async throws -> GapInvestigation in
      guard let id = context.parameters.get("id") else { throw HTTPError(.badRequest) }
      guard let investigation = try await store.investigateGap(id: id) else {
        throw HTTPError(.notFound)
      }
      return investigation
    }
    group.patch("/v1/operations/gaps/:id") { request, context async throws -> IngestionGap in
      guard let operatorDid = context.authContext?.did else { throw HTTPError(.unauthorized) }
      guard let id = context.parameters.get("id") else { throw HTTPError(.badRequest) }
      let body = try await request.decode(as: GapUpdateRequest.self, context: context)
      guard IngestionGapStatus.allCases.contains(body.status) else { throw HTTPError(.badRequest) }
      try validate(
        OperatorMutationRequest(
          auditNote: body.auditNote, environmentConfirmation: body.environmentConfirmation))
      try await store.updateGap(id: id, status: body.status, operatorDid: operatorDid, at: Date())
      try await store.recordAudit(
        operatorDid: operatorDid, action: "gap.\(body.status.rawValue).note", targetType: "gap",
        targetId: id, note: body.auditNote, at: Date())
      guard let gap = try await store.listGaps(limit: 250).first(where: { $0.id == id }) else {
        throw HTTPError(.notFound)
      }
      return gap
    }
    group.post("/v1/operations/backfills/dry-run") {
      request, context async throws -> BackfillDryRunResponse in
      let body = try await request.decode(as: BackfillDryRunRequest.self, context: context)
      try Self.validate(body)
      return try await store.estimateBackfill(body)
    }
    group.get("/v1/operations/backfills") { _, _ async throws -> BackfillListResponse in
      BackfillListResponse(backfills: try await store.listBackfills(limit: 250))
    }
    group.post("/v1/operations/backfills") { request, context async throws -> BackfillJob in
      guard config.recoveryEnabled else {
        throw HTTPError(.serviceUnavailable, message: "Recovery mutations are disabled")
      }
      guard let operatorDid = context.authContext?.did else { throw HTTPError(.unauthorized) }
      let body = try await request.decode(as: CreateBackfillRequest.self, context: context)
      try Self.validate(body.dryRun)
      if config.environment.lowercased() == "production",
        body.environmentConfirmation != "PRODUCTION"
      {
        throw HTTPError(.badRequest, message: "Production confirmation is required")
      }
      let freshEstimate = try await store.estimateBackfill(body.dryRun)
      guard freshEstimate.conflicts.isEmpty else {
        throw HTTPError(.conflict, message: freshEstimate.conflicts.joined(separator: " "))
      }
      guard freshEstimate.estimatedCount == body.expectedEstimate else {
        throw HTTPError(.conflict, message: "Dry-run estimate changed; review it again")
      }
      return try await store.createBackfill(body, operatorDid: operatorDid, at: Date())
    }
    group.get("/v1/operations/backfills/:id") { _, context async throws -> BackfillJob in
      guard let id = context.parameters.get("id") else { throw HTTPError(.badRequest) }
      guard let job = try await store.fetchBackfill(id: id) else {
        throw HTTPError(.notFound)
      }
      return job
    }
    registerBackfillAction("pause", status: .paused, on: group)
    registerBackfillAction("resume", status: .queued, on: group)
    registerBackfillAction("cancel", status: .cancelled, on: group)

    group.get("/v1/operations/alerts") { _, _ async throws -> AlertListResponse in
      AlertListResponse(alerts: try await store.listAlerts(limit: 250))
    }
    registerAlertAction("acknowledge", status: .acknowledged, on: group)
    registerAlertAction("resolve", status: .resolved, on: group)

    group.get("/v1/operations/traces") { request, _ async throws -> TraceListResponse in
      let traceId = request.uri.queryParameters.get("traceId")
      return TraceListResponse(spans: try await store.listTraceSpans(limit: 500, traceId: traceId))
    }
    group.get("/v1/operations/traces/:traceId") { _, context async throws -> TraceListResponse in
      TraceListResponse(
        spans: try await store.listTraceSpans(
          limit: 500, traceId: context.parameters.get("traceId"))
      )
    }
  }

  private func registerBackfillAction(
    _ action: String,
    status: BackfillJobStatus,
    on group: RouterGroup<GatewayRequestContext>
  ) {
    group.post("/v1/operations/backfills/:id/\(action)") {
      request, context async throws -> BackfillJob in
      guard config.recoveryEnabled else {
        throw HTTPError(.serviceUnavailable, message: "Recovery mutations are disabled")
      }
      guard let operatorDid = context.authContext?.did else { throw HTTPError(.unauthorized) }
      let mutation = try await request.decode(as: OperatorMutationRequest.self, context: context)
      try validate(mutation)
      guard let id = context.parameters.get("id") else { throw HTTPError(.badRequest) }
      try await store.updateBackfillStatus(
        id: id,
        status: status,
        operatorDid: operatorDid,
        failureReason: nil,
        at: Date()
      )
      try await store.recordAudit(
        operatorDid: operatorDid, action: "backfill.\(action).note", targetType: "backfill",
        targetId: id, note: mutation.auditNote, at: Date())
      guard let job = try await store.fetchBackfill(id: id) else { throw HTTPError(.notFound) }
      return job
    }
  }

  private func registerAlertAction(
    _ action: String,
    status: OperationsAlertStatus,
    on group: RouterGroup<GatewayRequestContext>
  ) {
    group.post("/v1/operations/alerts/:id/\(action)") {
      request, context async throws -> OperationsAlert in
      guard let operatorDid = context.authContext?.did else { throw HTTPError(.unauthorized) }
      let mutation = try await request.decode(as: OperatorMutationRequest.self, context: context)
      try validate(mutation)
      guard let id = context.parameters.get("id") else { throw HTTPError(.badRequest) }
      try await store.updateAlertStatus(
        id: id, status: status, operatorDid: operatorDid, at: Date())
      try await store.recordAudit(
        operatorDid: operatorDid, action: "alert.\(action).note", targetType: "alert", targetId: id,
        note: mutation.auditNote, at: Date())
      guard let alert = try await store.listAlerts(limit: 250).first(where: { $0.id == id }) else {
        throw HTTPError(.notFound)
      }
      return alert
    }
  }

  private static func validate(_ request: BackfillDryRunRequest) throws {
    guard (1...10_000).contains(request.batchSize),
      (1...5_000).contains(request.rateLimit),
      (1...16).contains(request.maxConcurrency),
      !request.collections.isEmpty,
      request.collections.count <= 16
    else { throw HTTPError(.badRequest, message: "Backfill bounds are invalid") }
    if request.sourceMode == .jetstreamReplay {
      guard let start = request.startCursor, let end = request.endCursor, start < end else {
        throw HTTPError(
          .badRequest, message: "Jetstream replay requires an increasing cursor range")
      }
    } else if request.authorDids.isEmpty {
      throw HTTPError(.badRequest, message: "PDS reconciliation requires at least one author DID")
    }
  }

  private func validate(_ request: OperatorMutationRequest) throws {
    guard request.auditNote.trimmingCharacters(in: .whitespacesAndNewlines).count >= 8 else {
      throw HTTPError(.badRequest, message: "An operator audit note is required")
    }
    if config.environment.lowercased() == "production",
      request.environmentConfirmation != "PRODUCTION"
    {
      throw HTTPError(.badRequest, message: "Production confirmation is required")
    }
  }
}

struct GapUpdateRequest: Codable, Sendable {
  let status: IngestionGapStatus
  let auditNote: String
  let environmentConfirmation: String?
}
struct OperatorMutationRequest: Codable, Sendable {
  let auditNote: String
  let environmentConfirmation: String?
}
struct OperationsServiceListResponse: Codable, Sendable { let services: [OperationsServiceState] }
struct IngestionResponse: Codable, Sendable {
  let state: IngestionStreamState?
  let gaps: [IngestionGap]
}
struct AppViewOperationsResponse: Codable, Sendable {
  let services: [OperationsServiceState]
  let traces: [TraceSpan]
}
struct GapListResponse: Codable, Sendable { let gaps: [IngestionGap] }
struct BackfillListResponse: Codable, Sendable { let backfills: [BackfillJob] }
struct AlertListResponse: Codable, Sendable { let alerts: [OperationsAlert] }
struct TraceListResponse: Codable, Sendable { let spans: [TraceSpan] }

extension OperationsOverview: @retroactive ResponseEncodable {}
extension OperationsServiceState: @retroactive ResponseEncodable {}
extension IngestionStreamState: @retroactive ResponseEncodable {}
extension IngestionGap: @retroactive ResponseEncodable {}
extension GapInvestigation: @retroactive ResponseEncodable {}
extension BackfillDryRunResponse: @retroactive ResponseEncodable {}
extension BackfillJob: @retroactive ResponseEncodable {}
extension OperationsAlert: @retroactive ResponseEncodable {}
extension TraceSpan: @retroactive ResponseEncodable {}
extension OperationsServiceListResponse: ResponseEncodable {}
extension IngestionResponse: ResponseEncodable {}
extension AppViewOperationsResponse: ResponseEncodable {}
extension GapListResponse: ResponseEncodable {}
extension BackfillListResponse: ResponseEncodable {}
extension AlertListResponse: ResponseEncodable {}
extension TraceListResponse: ResponseEncodable {}
