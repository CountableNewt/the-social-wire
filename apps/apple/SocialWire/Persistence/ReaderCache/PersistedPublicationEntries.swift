import Foundation
import SwiftData

/// Cached entry-list slice for stale-while-revalidate (bounded eviction at coordinator level).
@Model
final class PersistedPublicationEntries {
    @Attribute(.unique) var publicationId: String
    var entriesPayload: Data
    var cachedAt: Date

    init(publicationId: String, entriesPayload: Data, cachedAt: Date = Date()) {
        self.publicationId = publicationId
        self.entriesPayload = entriesPayload
        self.cachedAt = cachedAt
    }
}
