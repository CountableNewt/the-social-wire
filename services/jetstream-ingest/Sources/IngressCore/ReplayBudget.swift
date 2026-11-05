import Foundation

public actor ReplayBudget {
  let incidentLimit: Int64
  let dailyLimit: Int64
  public private(set) var incident: Int64
  var buckets: [(Date, Int64)]
  public init(
    incidentLimit: Int64, dailyLimit: Int64, incident: Int64 = 0, buckets: [(Date, Int64)] = []
  ) {
    self.incidentLimit = incidentLimit
    self.dailyLimit = dailyLimit
    self.incident = incident
    self.buckets = buckets
  }
  public func add(_ bytes: Int64, now: Date = Date()) throws {
    guard bytes > 0 else { return }
    buckets.removeAll { $0.0 <= now.addingTimeInterval(-86400) }
    let (total, overflow) = incident.addingReportingOverflow(bytes)
    guard !overflow else { throw IngressError.incidentBudget }
    incident = total
    let minute = Date(timeIntervalSince1970: floor(now.timeIntervalSince1970 / 60) * 60)
    if buckets.last?.0 == minute {
      buckets[buckets.count - 1].1 += bytes
    } else {
      buckets.append((minute, bytes))
    }
    if incident > incidentLimit { throw IngressError.incidentBudget }
    if buckets.reduce(Int64(0), { $0 + $1.1 }) > dailyLimit { throw IngressError.dailyBudget }
  }
  public func check(now: Date = Date()) throws {
    if incident >= incidentLimit { throw IngressError.incidentBudget }
    buckets.removeAll { $0.0 <= now.addingTimeInterval(-86400) }
    if buckets.reduce(Int64(0), { $0 + $1.1 }) > dailyLimit { throw IngressError.dailyBudget }
  }
  public func dailyWait(now: Date = Date()) -> Double {
    buckets.removeAll { $0.0 <= now.addingTimeInterval(-86400) }
    guard buckets.reduce(Int64(0), { $0 + $1.1 }) > dailyLimit, let oldest = buckets.first else {
      return 0
    }
    return max(60, oldest.0.addingTimeInterval(86400).timeIntervalSince(now))
  }
  public func resetIncident() { incident = 0 }
}
