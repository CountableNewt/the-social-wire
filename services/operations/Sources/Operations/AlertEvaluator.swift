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
    if let committedAt = state.lastCommittedAt,
       now.timeIntervalSince(committedAt) >= config.commitStaleSeconds
    {
      opened.append(
        try await store.openAlert(
          rule: "jetstream_committed_cursor_stale",
          severity: "critical",
          summary: "The last committed Jetstream cursor is stale.",
          evidence: ["last_committed_cursor": state.lastCommittedCursor.map(String.init) ?? "none"],
          runbookSlug: "stalled-ingestion",
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
          runbookSlug: "stalled-ingestion",
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
          runbookSlug: "confirm-scope-gap",
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
