import Foundation

enum PublicationSiteFavicon {
    static func url(for siteOrFeedURL: String) -> String? {
        let trimmed = siteOrFeedURL.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }
        let candidate = trimmed.contains("://") ? trimmed : "https://\(trimmed)"
        guard let host = URL(string: PublicURLNormalizer.normalizeHttpURLToHTTPS(candidate))?.host else {
            return nil
        }
        return "https://\(host)/favicon.ico"
    }
}
