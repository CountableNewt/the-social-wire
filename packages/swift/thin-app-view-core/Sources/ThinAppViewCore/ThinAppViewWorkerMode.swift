/// Maintenance owns only RSS polling and retention; it never consumes global ingestion or recovery jobs.
public enum ThinAppViewWorkerMode: String, Sendable, Equatable {
  case ingestion
  case maintenance

  public static func fromEnvironment(_ environment: [String: String]) throws -> Self {
    let raw = environment["THIN_APPVIEW_WORKER_MODE"] ?? "ingestion"
    guard let mode = Self(rawValue: raw) else { throw ConfigurationError.invalidMode }
    return mode
  }

  public func validate(role: ThinAppViewWorkerRole) throws {
    if self == .maintenance, role != .coordinator {
      throw ConfigurationError.maintenanceRequiresCoordinator
    }
  }

  public enum ConfigurationError: Error, Sendable, Equatable {
    case invalidMode
    case maintenanceRequiresCoordinator
  }
}
