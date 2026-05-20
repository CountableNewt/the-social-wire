import Foundation

struct LatrSavedItemRecord: Codable, Equatable, Sendable {
    let type: String
    var subjectUri: String
    var savedAt: String
    var state: String?
    var tags: [String]?
    var note: String?
    var lastOpenedAt: String?

    enum CodingKeys: String, CodingKey {
        case type = "$type"
        case subjectUri
        case savedAt
        case state
        case tags
        case note
        case lastOpenedAt
    }
}
