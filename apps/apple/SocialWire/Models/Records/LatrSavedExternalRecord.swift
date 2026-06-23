import Foundation

struct LatrSavedExternalRecord: Codable, Equatable, Sendable {
    let type: String
    var url: String
    var normalizedUrl: String
    var fingerprint: String
    var createdAt: String
    var title: String?
    var excerpt: String?
    var site: String?
    var image: String?
    var language: String?
    var publishedAt: String?
    var author: String?

    enum CodingKeys: String, CodingKey {
        case type = "$type"
        case url
        case normalizedUrl
        case fingerprint
        case createdAt
        case title
        case excerpt
        case site
        case image
        case language
        case publishedAt
        case author
    }
}
