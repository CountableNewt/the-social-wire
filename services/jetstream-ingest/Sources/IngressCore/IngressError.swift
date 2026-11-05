import Foundation

public enum IngressError: Error, Sendable, CustomStringConvertible {
  case invalidConfiguration(String)
  case invalidEvent(String)
  case malformedArchive(String)
  case provider(String)
  case cursorTooOld
  case dictionaryRotated
  case leaseUnavailable
  case sourceDiscontinuity
  case poisonCapacity
  case incidentBudget
  case dailyBudget
  case schemaTimeout

  public var description: String {
    switch self {
    case .invalidConfiguration(let value), .invalidEvent(let value), .malformedArchive(let value),
      .provider(let value):
      return value
    case .dictionaryRotated: return "Live compression dictionary rotated"
    case .cursorTooOld: return "Live cursor requires archive replay"
    case .leaseUnavailable: return "Ingestion lease is unavailable"
    case .sourceDiscontinuity:
      return "Source identity or cursor discontinuity requires operator review"
    case .poisonCapacity: return "Poison evidence capacity exhausted"
    case .incidentBudget: return "Replay incident byte budget exhausted"
    case .dailyBudget: return "Replay daily byte budget exhausted"
    case .schemaTimeout: return "Schema readiness canceled or timed out"
    }
  }
}
