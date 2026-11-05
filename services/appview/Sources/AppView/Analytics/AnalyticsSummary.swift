import Foundation

struct AnalyticsSummary: Codable, Sendable, Equatable {
  var items: [AnalyticsItem] = []
  var totals: AnalyticsTotals = .init()
  var series: [AnalyticsDay] = []
  var coverage: AnalyticsCoverage = .init()
  var cursor: String? = nil
}
