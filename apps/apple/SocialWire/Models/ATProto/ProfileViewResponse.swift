import Foundation

struct ProfileViewResponse: Codable, Sendable {
    struct Viewer: Codable, Sendable {
        let like: String?
        let repost: String?
    }

    let uri: String
    let cid: String?
    let viewer: Viewer?
}
