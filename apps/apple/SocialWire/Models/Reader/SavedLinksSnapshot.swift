import Foundation

/// A bounded display snapshot; the server remains authoritative on every refresh.
struct SavedLinksSnapshot: Codable, Sendable {
    let active: [MergedLatrSave]
    let archived: [MergedLatrSave]
    let cachedAt: Date

    init(active: [MergedLatrSave], archived: [MergedLatrSave], now: Date = Date()) {
        self.active = Array(active.prefix(120))
        self.archived = Array(archived.prefix(120))
        cachedAt = now
    }

    func isUsable(now: Date = Date()) -> Bool {
        now.timeIntervalSince(cachedAt) < 7 * 24 * 60 * 60
    }

    static func storageKey(viewerDID: String) -> String {
        "saved-links-snapshot.v1|\(viewerDID)"
    }
}
