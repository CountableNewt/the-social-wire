import Foundation

struct OAuthProtectedResourceMetadata: Decodable, Sendable {
    let authorizationServers: [String]

    enum CodingKeys: String, CodingKey {
        case authorizationServers = "authorization_servers"
    }
}
