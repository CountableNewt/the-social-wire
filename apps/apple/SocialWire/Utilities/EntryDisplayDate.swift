import Foundation

enum EntryDisplayDate {
    static func listRowPublishedAt(_ raw: String) -> String {
        guard let date = DateFormatters.date(from: raw) else { return raw }
        return date.formatted(date: .abbreviated, time: .omitted)
    }

    static func savedLinkDate(_ raw: String) -> String {
        guard let date = DateFormatters.date(from: raw) else { return raw }
        return date.formatted(date: .abbreviated, time: .shortened)
    }

    static func savedLinkRowSubtitle(
        site: String?,
        previewHost: String?,
        author: String?,
        publishedAt: String?,
        savedAt: String
    ) -> String {
        var parts: [String] = []
        if let site, !site.isEmpty {
            parts.append(site)
        } else if let previewHost, !previewHost.isEmpty {
            parts.append(previewHost)
        }
        if let author, !author.isEmpty {
            parts.append(author)
        }
        if let publishedAt, !publishedAt.isEmpty {
            parts.append(savedLinkDate(publishedAt))
        }
        parts.append(savedLinkDate(savedAt))
        return parts.joined(separator: " · ")
    }
}
