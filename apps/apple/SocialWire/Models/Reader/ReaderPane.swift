import Foundation

/// Compact-width horizontal pager: lists → publications → articles → reader (left to right).
enum ReaderPane: Int, Hashable, CaseIterable {
    case lists = 0
    case publications = 1
    case articles = 2
    case reader = 3
}
