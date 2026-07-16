import Foundation

public struct OperationsConfiguration: Sendable {
  public let enabled: Bool
  public let recoveryEnabled: Bool
  public let alertDeliveryEnabled: Bool
  public let environment: String
  public let instanceId: String
  public let operatorDids: Set<String>
  public let webhookURL: String?
  public let webhookSecret: String?
  public let replayRewindMicroseconds: Int64
  public let disconnectAlertSeconds: TimeInterval
  public let idleAlertSeconds: TimeInterval
  public let commitStaleSeconds: TimeInterval
  public let backlogAlertMicroseconds: Int64
  public let backfillStallSeconds: TimeInterval

  public static func fromEnvironment(_ environment: [String: String]) -> OperationsConfiguration {
    let operatorDids = Set(
      (environment["OPERATIONS_OPERATOR_DIDS"] ?? "")
        .split(separator: ",")
        .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
        .filter { !$0.isEmpty }
    )
    return OperationsConfiguration(
      enabled: truthy(environment["OPERATIONS_TELEMETRY_ENABLED"], defaultValue: true),
      recoveryEnabled: truthy(environment["OPERATIONS_RECOVERY_ENABLED"]),
      alertDeliveryEnabled: truthy(environment["OPERATIONS_ALERT_DELIVERY_ENABLED"]),
      environment: environment["APP_ENV"] ?? "local",
      instanceId: environment["FLY_MACHINE_ID"] ?? ProcessInfo.processInfo.hostName,
      operatorDids: operatorDids,
      webhookURL: nonEmpty(environment["OPERATIONS_ALERT_WEBHOOK_URL"]),
      webhookSecret: nonEmpty(environment["OPERATIONS_ALERT_WEBHOOK_SECRET"]),
      replayRewindMicroseconds: int64(environment["OPERATIONS_REPLAY_REWIND_MICROSECONDS"], 5_000_000),
      disconnectAlertSeconds: seconds(environment["OPERATIONS_DISCONNECT_ALERT_SECONDS"], 120),
      idleAlertSeconds: seconds(environment["OPERATIONS_IDLE_ALERT_SECONDS"], 300),
      commitStaleSeconds: seconds(environment["OPERATIONS_COMMIT_STALE_SECONDS"], 300),
      backlogAlertMicroseconds: int64(environment["OPERATIONS_BACKLOG_ALERT_MICROSECONDS"], 60_000_000),
      backfillStallSeconds: seconds(environment["OPERATIONS_BACKFILL_STALL_SECONDS"], 600)
    )
  }

  private static func truthy(_ value: String?, defaultValue: Bool = false) -> Bool {
    guard let value else { return defaultValue }
    return ["1", "true", "yes", "on"].contains(value.lowercased())
  }

  private static func nonEmpty(_ value: String?) -> String? {
    guard let value = value?.trimmingCharacters(in: .whitespacesAndNewlines), !value.isEmpty else {
      return nil
    }
    return value
  }

  private static func seconds(_ value: String?, _ fallback: TimeInterval) -> TimeInterval {
    guard let value, let parsed = TimeInterval(value), parsed > 0 else { return fallback }
    return parsed
  }

  private static func int64(_ value: String?, _ fallback: Int64) -> Int64 {
    guard let value, let parsed = Int64(value), parsed > 0 else { return fallback }
    return parsed
  }
}
