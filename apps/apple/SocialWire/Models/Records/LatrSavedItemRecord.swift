import Foundation

struct LatrSavedItemRecord: Codable, Equatable, Sendable {
    let type: String
    var subjectUri: String
    var savedAt: String
    var state: String?
    var tags: [String]?
    var note: String?
    var lastOpenedAt: String?
    var linkedWebUrl: String?
    var previewTitle: String?
    var previewExcerpt: String?
    var previewSite: String?
    var previewImage: String?
    var previewAuthor: String?

    enum CodingKeys: String, CodingKey {
        case type = "$type"
        case subjectUri
        case savedAt
        case state
        case tags
        case note
        case lastOpenedAt
        case linkedWebUrl
        case previewTitle
        case previewExcerpt
        case previewSite
        case previewImage
        case previewAuthor
    }
}
