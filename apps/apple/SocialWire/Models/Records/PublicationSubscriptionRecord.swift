import Foundation

struct PublicationSubscriptionRecord: Codable, Equatable, Sendable {
    let type: String
    var publication: String

    enum CodingKeys: String, CodingKey {
        case type = "$type"
        case publication
    }
}
