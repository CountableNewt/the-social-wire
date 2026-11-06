import Foundation

struct AnalyticsTotals: Codable, Sendable, Equatable {
  var engagement: AnalyticsEngagement? = nil
  var tracking: AnalyticsTracking = .init()

  enum CodingKeys: String, CodingKey { case engagement, tracking }
  func encode(to encoder: Encoder) throws {
    var container = encoder.container(keyedBy: CodingKeys.self)
    try container.encode(engagement, forKey: .engagement)
    try container.encode(tracking, forKey: .tracking)
  }
}
