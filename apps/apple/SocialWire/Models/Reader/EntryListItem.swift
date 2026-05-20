import Foundation

struct EntryListItem: Identifiable, Codable, Equatable, Sendable {
    var entryId: String
    var title: String
    var summary: String?
    var publishedAt: String
    var thumbnailUrl: String?
    var thumbnailFallbackUrl: String?

    var id: String { entryId }
}
