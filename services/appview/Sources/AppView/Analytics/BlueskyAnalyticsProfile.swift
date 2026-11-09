struct BlueskyAnalyticsProfile: Codable, Sendable {
  var followers: Int?
  var following: Int?
  var posts: Int?

  enum CodingKeys: String, CodingKey { case followers, following, posts }
  func encode(to encoder: Encoder) throws {
    var container = encoder.container(keyedBy: CodingKeys.self)
    try container.encode(followers, forKey: .followers)
    try container.encode(following, forKey: .following)
    try container.encode(posts, forKey: .posts)
  }
}
