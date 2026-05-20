import Foundation

/// Top-level list bucket on the compact lists pane (Read Later / Subscribed / Following).
enum ReaderListSource: String, CaseIterable, Identifiable, Hashable {
    case readLater = "Read Later"
    case subscribed = "Subscribed"
    case following = "Following"

    var id: String { rawValue }

    var systemImage: String {
        switch self {
        case .readLater: "bookmark"
        case .subscribed: "tray.full"
        case .following: "person.2"
        }
    }
}
