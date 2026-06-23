import Foundation

struct PublicationPrefsRecord: Codable, Equatable, Sendable {
    let type: String
    var publicationId: String
    var folderId: String?
    var sortOrder: Int?
    var hidden: Bool?
    var createdAt: String

    enum CodingKeys: String, CodingKey {
        case type = "$type"
        case publicationId
        case folderId
        case sortOrder
        case hidden
        case createdAt
    }
}
