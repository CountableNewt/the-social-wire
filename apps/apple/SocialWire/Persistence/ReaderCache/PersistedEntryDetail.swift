import Foundation
import SwiftData

@Model
final class PersistedEntryDetail {
    @Attribute(.unique) var entryId: String
    var detailPayload: Data
    var cachedAt: Date

    init(entryId: String, detailPayload: Data, cachedAt: Date = Date()) {
        self.entryId = entryId
        self.detailPayload = detailPayload
        self.cachedAt = cachedAt
    }
}
