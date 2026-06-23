import Foundation

struct EntryReadStateRecord: Codable, Equatable, Sendable {
    let type: String
    var subjectUri: String
    var readAt: String
    var updatedAt: String?

    enum CodingKeys: String, CodingKey {
        case type = "$type"
        case subjectUri
        case readAt
        case updatedAt
    }
}
