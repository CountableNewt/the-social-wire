import Foundation

enum ReaderFilter: String, CaseIterable, Identifiable {
    case all = "All"
    case unread = "Unread"

    var id: String { rawValue }
}
