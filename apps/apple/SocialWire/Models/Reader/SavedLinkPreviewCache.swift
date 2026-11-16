import Foundation

struct SavedLinkPreviewCache: Codable, Sendable {
    private struct Entry: Codable, Sendable {
        let metadata: LatrSaveMetadata
        let cachedAt: Date
    }

    private var entries: [String: Entry] = [:]
    static let maximumEntries = 256
    static let lifetime: TimeInterval = 7 * 24 * 60 * 60

    static func storageKey(viewerDID: String) -> String {
        "saved-link-previews.v1|\(viewerDID)"
    }

    func metadata(for id: String, now: Date = Date()) -> LatrSaveMetadata? {
        guard let entry = entries[id],
              now.timeIntervalSince(entry.cachedAt) < Self.lifetime else { return nil }
        return entry.metadata
    }

    mutating func store(_ metadata: LatrSaveMetadata, for id: String, now: Date = Date()) {
        entries = entries.filter { now.timeIntervalSince($0.value.cachedAt) < Self.lifetime }
        entries[id] = Entry(metadata: metadata, cachedAt: now)
        for key in entries.sorted(by: { $0.value.cachedAt > $1.value.cachedAt })
            .dropFirst(Self.maximumEntries).map(\.key) {
            entries.removeValue(forKey: key)
        }
    }
}
