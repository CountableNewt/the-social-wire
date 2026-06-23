import Foundation

enum LatrSaveListState: String, Sendable {
    case active
    case archived
    case all
}

struct LatrSaveMetadata: Equatable, Sendable {
    var title: String?
    var excerpt: String?
    var image: String?
    var site: String?
    var author: String?
    var publishedAt: String?
    var language: String?
    var linkedWebUrl: String?
}
