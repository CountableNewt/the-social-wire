import Foundation

/// Mirrors web `mergeUnreadFeedRefresh`: a fresh unread page is authoritative only for the
/// span it covers, so rows older than its oldest entry stay and scrolling is not reset.
enum ReaderUnreadPageMerge {
    static func merge(
        existing: [EntryListItem],
        existingCursor: String?,
        fresh: [EntryListItem],
        freshCursor: String?
    ) -> (entries: [EntryListItem], cursor: String?) {
        // Without a cursor the fresh page is the complete unread set.
        guard freshCursor != nil, !existing.isEmpty,
              let oldest = fresh.compactMap({ date($0.publishedAt) }).min()
        else { return (fresh, freshCursor) }
        var seen = Set(fresh.map(\.entryId))
        let carried = existing.filter { item in
            // A newer row missing from the fresh page was read elsewhere.
            guard let published = date(item.publishedAt), published < oldest else { return false }
            return seen.insert(item.entryId).inserted
        }
        return (fresh + carried, carried.isEmpty ? freshCursor : existingCursor)
    }

    private static func date(_ value: String) -> Date? {
        if let date = try? Date(value, strategy: .iso8601) { return date }
        return try? Date(value, strategy: Date.ISO8601FormatStyle(includingFractionalSeconds: true))
    }
}
