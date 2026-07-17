import Foundation
import Logging
import OperationsCore

struct AlertEvaluator {
  let store: any OperationsStore
  let config: OperationsConfiguration
  let logger: Logger
  let webhook: OperationsWebhookDelivery?

  func runForever() async {
    while !Task.isCancelled {
      do {
        try await evaluate(at: Date())
      } catch {
        logger.error("Operations alert evaluation failed", metadata: ["error_type": .string("evaluation")])
      }
      try? await Task.sleep(for: .seconds(30))
    }
  }

  func evaluate(at now: Date) async throws {
    guard let state = try await store.fetchStreamState(source: "jetstream") else { return }
    var opened: [OperationsAlert] = []
    if state.connectionState != .connected,
       let disconnectedAt = state.lastDisconnectAt,
       now.timeIntervalSince(disconnectedAt) >= config.disconnectAlertSeconds
    {
      opened.append(
        try await store.openAlert(
          rule: "jetstream_disconnected",
          severity: "critical",
          summary: "Jetstream has remained disconnected beyond the configured threshold.",
          evidence: ["connection_state": state.connectionState.rawValue],
          runbookSlug: "jetstream-disconnect-reconnect",
          at: now
        )
      )
    }
    if state.connectionState == .connected,
       let receivedAt = state.lastReceivedAt,
       now.timeIntervalSince(receivedAt) >= config.idleAlertSeconds
    {
      opened.append(
        try await store.openAlert(
          rule: "jetstream_connected_idle",
          severity: "critical",
          summary: "Jetstream is connected but has not received an event within the configured threshold.",
          evidence: ["last_received_cursor": state.lastReceivedCursor.map(String.init) ?? "none"],
          runbookSlug: "live-process-stalled-ingestion",
          at: now
        )
      )
    }
    if let committedAt = state.lastCommittedAt,
       now.timeIntervalSince(committedAt) >= config.commitStaleSeconds
    {
      opened.append(
        try await store.openAlert(
          rule: "jetstream_committed_cursor_stale",
          severity: "critical",
          summary: "The last committed Jetstream cursor is stale.",
          evidence: ["last_committed_cursor": state.lastCommittedCursor.map(String.init) ?? "none"],
          runbookSlug: "live-process-stalled-ingestion",
          at: now
        )
      )
    }
    if let received = state.lastReceivedCursor,
       let committed = state.lastCommittedCursor,
       received - committed >= config.backlogAlertMicroseconds
    {
      opened.append(
        try await store.openAlert(
          rule: "jetstream_commit_backlog",
          severity: "warning",
          summary: "The receive-to-commit Jetstream backlog is above threshold.",
          evidence: ["cursor_delta_microseconds": String(received - committed)],
          runbookSlug: "live-process-stalled-ingestion",
          at: now
        )
      )
    }
    let confirmedGaps = try await store.listGaps(limit: 250).filter { $0.status == .confirmed }
    if !confirmedGaps.isEmpty {
      opened.append(
        try await store.openAlert(
          rule: "confirmed_ingestion_gap",
          severity: "critical",
          summary: "A confirmed ingestion gap requires recovery.",
          evidence: ["gap_count": String(confirmedGaps.count)],
          runbookSlug: "confirming-and-scoping-a-gap",
          at: now
        )
      )
    }
    let backfills = try await store.listBackfills(limit: 250)
    let stalled = backfills.filter {
      $0.status == .running && now.timeIntervalSince($0.updatedAt) >= config.backfillStallSeconds
    }
    if !stalled.isEmpty {
      opened.append(
        try await store.openAlert(
          rule: "backfill_without_progress",
          severity: "critical",
          summary: "A running backfill has not reported progress within the configured threshold.",
          evidence: ["backfill_count": String(stalled.count)],
          runbookSlug: "running-and-validating-backfills",
          at: now
        )
      )
    }
    let terminalFailures = backfills.filter { $0.status == .failed }
    if !terminalFailures.isEmpty {
      opened.append(
        try await store.openAlert(
          rule: "terminal_backfill_failure",
          severity: "critical",
          summary: "A backfill ended in a terminal failure.",
          evidence: ["backfill_count": String(terminalFailures.count)],
          runbookSlug: "running-and-validating-backfills",
          at: now
        )
      )
    }
    guard config.alertDeliveryEnabled, let webhook else { return }
    for alert in opened where alert.deliveryAttempts == 0 {
      do {
        try await webhook.deliver(alert)
        try await store.recordAlertDelivery(id: alert.id, error: nil, at: now)
      } catch {
        try await store.recordAlertDelivery(id: alert.id, error: "webhook_delivery_failed", at: now)
      }
    }
  }
}
