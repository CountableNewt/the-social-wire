import Foundation

/// Compact-width horizontal pager.
/// Subscribed/Following: lists → publications → articles → reader.
/// Read Later/Archive: lists → saved links → reader.
enum ReaderPane: Int, Hashable, CaseIterable {
    case lists = 0
    case publications = 1
    case articles = 2
    case reader = 3

    /// Contiguous `TabView` page index. Three-pane layouts must not skip index 2.
    func compactTabTag(usesArticlesPane: Bool) -> Int {
        if usesArticlesPane { return rawValue }
        switch self {
        case .lists: return 0
        case .publications, .articles: return 1
        case .reader: return 2
        }
    }

    static func fromCompactTabTag(_ tag: Int, usesArticlesPane: Bool) -> ReaderPane {
        if usesArticlesPane {
            return ReaderPane(rawValue: tag) ?? .lists
        }
        switch tag {
        case 0: return .lists
        case 1: return .publications
        default: return .reader
        }
    }
}
