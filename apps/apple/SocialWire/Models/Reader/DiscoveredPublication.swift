import Foundation

struct DiscoveredPublication: Identifiable, Codable, Equatable, Sendable {
    var publicationId: String
    var subscriptionPublicationId: String?
    var authorDid: String
    var authorHandle: String
    var title: String
    var iconUrl: String?
    var avatarUrl: String?
    var discoveredAt: String

    var id: String { publicationId }
    var displayImageURL: URL? { URL(string: iconUrl ?? avatarUrl ?? "") }
}
