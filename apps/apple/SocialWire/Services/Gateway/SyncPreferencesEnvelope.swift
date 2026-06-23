import Foundation

/// JSON bundle returned by **`GET /v1/sync/preferences`** (`PreferenceSyncService.finalizePreferences`).
struct SyncPreferencesEnvelope: Codable, Sendable {
    let etag: String?
    let revision: String?
    let cid: String?
    let cachedAt: String?
    let record: PreferencesRecord?
}
