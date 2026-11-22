import Foundation

/// Top-level list bucket on the reader's Lists pane.
enum ReaderListSource: String, CaseIterable, Codable, Identifiable, Hashable {
    case readLater = "Read Later"
    case archive = "Archive"
    case subscribed = "Subscribed"
    case following = "Following"

    var id: String { rawValue }

    var preferenceKey: String {
        switch self {
        case .readLater: "readLater"
        case .archive: "archive"
        case .subscribed: "subscribed"
        case .following: "following"
        }
    }

    init?(preferenceKey: String) {
        switch preferenceKey {
        case "readLater": self = .readLater
        case "archive": self = .archive
        case "subscribed": self = .subscribed
        case "following": self = .following
        default: return nil
        }
    }

    var systemImage: String {
        switch self {
        case .readLater: "bookmark"
        case .archive: "archivebox"
        case .subscribed: "tray.full"
        case .following: "person.2"
        }
    }

    var supportsReadState: Bool {
        switch self {
        case .readLater, .archive: false
        case .subscribed, .following: true
        }
    }

    static let preferenceCases: [ReaderListSource] = [
        .readLater, .archive, .subscribed, .following,
    ]

    var supportsMarkAllReadContextMenu: Bool {
        switch self {
        case .subscribed, .following: true
        case .readLater, .archive: false
        }
    }
}
