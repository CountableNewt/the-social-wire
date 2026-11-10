struct BlueskyAnalyticsShares: Codable, Sendable {
  var articleUri: String
  var articleTitle: String
  var articleUrl: String
  var posts: [BlueskyAnalyticsPost]
}
