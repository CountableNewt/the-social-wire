import Foundation

struct MergedLatrNativeSave: Codable, Equatable, Hashable, Sendable {
    var savedAt: String
    var itemRkey: String
    var itemUri: String
    var subjectUri: String
    var state: String?
    var title: String?
    var excerpt: String?
    var url: String?
    var image: String?
}
