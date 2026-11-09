import Foundation

struct BlueskyAnalyticsSummary: Codable, Sendable {
  var sampledAt: Date
  var days: Int
  var profile = BlueskyAnalyticsProfile()
  var activity: BlueskyAnalyticsActivity?
  var topPosts: [BlueskyAnalyticsPost] = []
  var shares: [BlueskyAnalyticsShares] = []
  var coverage = BlueskyAnalyticsCoverage()

  enum CodingKeys: String, CodingKey { case sampledAt, days, profile, activity, topPosts, shares, coverage }
  func encode(to encoder: Encoder) throws {
    var container = encoder.container(keyedBy: CodingKeys.self)
    try container.encode(sampledAt, forKey: .sampledAt)
    try container.encode(days, forKey: .days)
    try container.encode(profile, forKey: .profile)
    try container.encode(activity, forKey: .activity)
    try container.encode(topPosts, forKey: .topPosts)
    try container.encode(shares, forKey: .shares)
    try container.encode(coverage, forKey: .coverage)
  }
}
