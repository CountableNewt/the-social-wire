import Foundation

struct AnalyticsTracking: Codable, Sendable, Equatable {
  var impressions: Int = 0
  var views: Int = 0
  var outboundClicks: Int = 0
  var ctr: Double? = nil

  enum CodingKeys: String, CodingKey { case impressions, views, outboundClicks, ctr }
  func encode(to encoder: Encoder) throws {
    var container = encoder.container(keyedBy: CodingKeys.self)
    try container.encode(impressions, forKey: .impressions)
    try container.encode(views, forKey: .views)
    try container.encode(outboundClicks, forKey: .outboundClicks)
    try container.encode(ctr, forKey: .ctr)
  }
}

extension AnalyticsTracking {
  mutating func calculateCTR() { ctr = impressions > 0 ? min(100, 100 * Double(outboundClicks) / Double(impressions)) : nil }
}
