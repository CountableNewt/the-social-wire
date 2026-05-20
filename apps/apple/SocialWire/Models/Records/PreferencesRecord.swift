import Foundation

struct PreferencesRecord: Codable, Equatable, Sendable {
    let type: String
    var readLaterService: String?
    var readLaterConnections: [String: ReadLaterConnectionPreferenceRecord]?
    var createdAt: String
    var updatedAt: String

    enum CodingKeys: String, CodingKey {
        case type = "$type"
        case readLaterService
        case readLaterConnections
        case createdAt
        case updatedAt
    }
}
