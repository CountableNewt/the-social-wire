import Foundation

struct AnalyticsItem: Codable, Sendable, Equatable {
  var uri: String
  var kind: String
  var title: String
  var url: String
  var publishedAt: String
  var engagement: AnalyticsEngagement? = nil
  var tracking: AnalyticsTracking = .init()
  var refreshedAt: Date

  enum CodingKeys: String, CodingKey { case uri, kind, title, url, publishedAt, engagement, tracking, refreshedAt }
  func encode(to encoder: Encoder) throws {
    var container = encoder.container(keyedBy: CodingKeys.self)
    try container.encode(uri, forKey: .uri)
    try container.encode(kind, forKey: .kind)
    try container.encode(title, forKey: .title)
    try container.encode(url, forKey: .url)
    try container.encode(publishedAt, forKey: .publishedAt)
    try container.encode(engagement, forKey: .engagement)
    try container.encode(tracking, forKey: .tracking)
    try container.encode(refreshedAt, forKey: .refreshedAt)
  }
}
