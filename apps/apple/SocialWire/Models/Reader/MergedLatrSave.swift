import Foundation

enum MergedLatrSave: Identifiable, Codable, Equatable, Hashable, Sendable {
    case external(MergedLatrExternalSave)
    case native(MergedLatrNativeSave)

    var id: String {
        switch self {
        case .external(let save): "external:\(save.normalizedUrl)"
        case .native(let save): "native:\(save.itemUri)"
        }
    }

    var title: String {
        switch self {
        case .external(let save): save.title ?? URL(string: save.url)?.host ?? save.url
        case .native(let save): save.title ?? save.subjectUri
        }
    }

    var url: URL? {
        switch self {
        case .external(let save): URL(string: save.url)
        case .native(let save): save.url.flatMap(URL.init(string:))
        }
    }

    var savedAt: String {
        switch self {
        case .external(let save): save.savedAt
        case .native(let save): save.savedAt
        }
    }
}
