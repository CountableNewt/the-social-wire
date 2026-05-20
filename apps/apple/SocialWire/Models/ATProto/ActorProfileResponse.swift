import Foundation

struct ActorProfileResponse: Codable, Sendable {
    let did: String
    let handle: String
    let displayName: String?
    let avatar: String?

    enum CodingKeys: String, CodingKey {
        case did
        case handle
        case displayName
        case avatar
    }
}
