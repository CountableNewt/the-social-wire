import Foundation

actor AnalyticsLimits {
  private var entries: [String: (until: Date, count: Int)] = [:]
  func allow(key: String, at: Date, maximum: Int) -> Bool {
    entries = entries.filter { at < $0.value.until }
    var value = entries[key] ?? (at.addingTimeInterval(60), 0)
    guard entries[key] != nil || entries.count < 10000, value.count < maximum else { return false }
    value.count += 1
    entries[key] = value
    return true
  }
}
