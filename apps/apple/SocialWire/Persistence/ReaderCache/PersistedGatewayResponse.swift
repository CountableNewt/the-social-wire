import Foundation
import SwiftData

@Model
final class PersistedGatewayResponse {
    @Attribute(.unique) var cacheKey: String
    var etagValue: String?
    var body: Data?
    var cachedAt: Date

    init(cacheKey: String, etagValue: String?, body: Data?, cachedAt: Date = Date()) {
        self.cacheKey = cacheKey
        self.etagValue = etagValue
        self.body = body
        self.cachedAt = cachedAt
    }
}
