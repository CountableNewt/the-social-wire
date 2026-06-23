import Foundation

struct EntryDetail: Identifiable, Codable, Equatable, Sendable {
    var entryId: String
    var title: String
    var publishedAt: String
    var contentHtml: String
    var originalUrl: String?
    var embedUrl: String?
    var bskyPostUri: String?
    var bskyPostCid: String?

    var id: String { entryId }
    var canonicalURL: URL? {
        guard let raw = embedUrl ?? originalUrl else { return nil }
        return URL(string: PublicURLNormalizer.normalizeHttpURLToHTTPS(raw))
    }
}
