import Foundation

struct FollowsResponse: Codable, Sendable {
    let follows: [FollowProfile]
    let cursor: String?
}
