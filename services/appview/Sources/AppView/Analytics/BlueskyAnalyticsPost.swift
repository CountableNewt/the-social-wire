struct BlueskyAnalyticsPost: Codable, Sendable {
  var uri: String
  var title: String
  var url: String
  var publishedAt: String
  var engagement: AnalyticsEngagement?

  enum CodingKeys: String, CodingKey { case uri, title, url, publishedAt, engagement }
  func encode(to encoder: Encoder) throws {
    var container = encoder.container(keyedBy: CodingKeys.self)
    try container.encode(uri, forKey: .uri)
    try container.encode(title, forKey: .title)
    try container.encode(url, forKey: .url)
    try container.encode(publishedAt, forKey: .publishedAt)
    try container.encode(engagement, forKey: .engagement)
  }
}
