import Foundation

struct PostsResponse: Codable, Sendable {
    let posts: [ProfileViewResponse]
}
