import Foundation

struct AnalyticsDay: Codable, Sendable, Equatable {
  var day: String
  var likes: Int? = nil
  var replies: Int? = nil
  var reposts: Int? = nil
  var quotes: Int? = nil
  var impressions: Int? = nil
  var views: Int? = nil
  var outboundClicks: Int? = nil
  var ctr: Double? = nil

  enum CodingKeys: String, CodingKey { case day, likes, replies, reposts, quotes, impressions, views, outboundClicks, ctr }
  func encode(to encoder: Encoder) throws {
    var container = encoder.container(keyedBy: CodingKeys.self)
    try container.encode(day, forKey: .day)
    try container.encode(likes, forKey: .likes)
    try container.encode(replies, forKey: .replies)
    try container.encode(reposts, forKey: .reposts)
    try container.encode(quotes, forKey: .quotes)
    try container.encode(impressions, forKey: .impressions)
    try container.encode(views, forKey: .views)
    try container.encode(outboundClicks, forKey: .outboundClicks)
    try container.encode(ctr, forKey: .ctr)
  }
}
