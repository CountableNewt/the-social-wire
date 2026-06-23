import Foundation

struct ReadLaterConnectionPreferenceRecord: Codable, Equatable, Sendable {
    var connectedAt: String?
    var accountLabel: String?

    enum CodingKeys: String, CodingKey {
        case connectedAt
        case accountLabel
    }
}
