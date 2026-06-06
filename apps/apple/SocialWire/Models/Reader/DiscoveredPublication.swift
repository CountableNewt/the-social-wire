import Foundation

struct DiscoveredPublication: Identifiable, Codable, Equatable, Sendable {
    var publicationId: String
    var subscriptionPublicationId: String?
    var authorDid: String
    var authorHandle: String
    var title: String
    var iconUrl: String?
    var avatarUrl: String?
    var publicationSiteUrls: [String] = []
    var discoveredAt: String

    var id: String { publicationId }

    /// Publication icon first, then author avatar, then site favicon fallbacks.
    var displayImageURLs: [URL] {
        var urls: [URL] = []
        var seen = Set<String>()

        func appendCandidate(_ raw: String?) {
            guard let raw = raw?.trimmingCharacters(in: .whitespacesAndNewlines),
                  !raw.isEmpty,
                  !PublicURLNormalizer.isBridgySyncGetBlobURL(raw)
            else { return }
            let normalized = PublicURLNormalizer.normalizeHttpURLToHTTPS(raw)
            guard let url = URL(string: normalized), seen.insert(url.absoluteString).inserted else { return }
            urls.append(url)
        }

        appendCandidate(iconUrl)
        appendCandidate(avatarUrl)
        for site in publicationSiteUrls {
            appendCandidate(PublicationSiteFavicon.url(for: site))
        }
        return urls
    }

    /// Primary display image URL for simple single-URL consumers.
    var displayImageURL: URL? {
        displayImageURLs.first
    }
}
