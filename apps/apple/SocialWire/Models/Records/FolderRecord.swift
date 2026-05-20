import Foundation

struct FolderRecord: Codable, Equatable, Sendable {
    let type: String
    var name: String
    var sortOrder: Int?
    var icon: String?
    var iconImage: String?
    var createdAt: String

    enum CodingKeys: String, CodingKey {
        case type = "$type"
        case name
        case sortOrder
        case icon
        case iconImage
        case createdAt
    }
}
