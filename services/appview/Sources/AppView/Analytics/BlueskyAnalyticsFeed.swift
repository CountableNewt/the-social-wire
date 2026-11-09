struct BlueskyAnalyticsFeed: Decodable, Sendable {
  var feed: [Row]
  var cursor: String?

  struct Row: Decodable, Sendable {
    var post: Post
    var reason: Reason?
  }
  struct Reason: Decodable, Sendable {
    var type: String
    enum CodingKeys: String, CodingKey { case type = "$type" }
  }
  struct Post: Decodable, Sendable {
    var uri: String
    var author: Author
    var record: Record
    var likeCount: Int?
    var replyCount: Int?
    var repostCount: Int?
    var quoteCount: Int?
  }
  struct Author: Decodable, Sendable { var did: String }
  struct Record: Decodable, Sendable {
    var text: String
    var createdAt: String
    var reply: Reply?
  }
  struct Reply: Decodable, Sendable {}
}
