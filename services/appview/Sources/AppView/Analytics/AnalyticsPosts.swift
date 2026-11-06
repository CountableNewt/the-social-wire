struct AnalyticsPosts: Decodable, Sendable {
  let posts: [Post]?
  struct Post: Decodable, Sendable {
    let uri: String?
    let author: Author?
    let likeCount: Int?
    let replyCount: Int?
    let repostCount: Int?
    let quoteCount: Int?
  }
  struct Author: Decodable, Sendable { let did: String? }
}
