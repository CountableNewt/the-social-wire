import Foundation

struct SkyreaderFeedSubscriptionRecord: Codable, Equatable, Sendable {
    let type: String
    var createdAt: String
    var updatedAt: String?
    var feedUrl: String?
    var title: String?
    var siteUrl: String?
    var source: String?
    var sourceType: String?
    var customTitle: String?
    var customIconUrl: String?

    enum CodingKeys: String, CodingKey {
        case type = "$type"
        case createdAt
        case updatedAt
        case feedUrl
        case title
        case siteUrl
        case source
        case sourceType
        case customTitle
        case customIconUrl
    }
}
