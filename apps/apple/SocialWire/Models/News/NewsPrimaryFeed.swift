import Foundation

enum NewsPrimaryFeed: String, CaseIterable, Codable, Identifiable, Hashable, Sendable {
    case subscribed
    case following

    var id: Self { self }

    var title: String {
        switch self {
        case .subscribed: "Subscribed"
        case .following: "Following"
        }
    }

    var systemImage: String {
        switch self {
        case .subscribed: "tray.full"
        case .following: "person.2"
        }
    }

    var newsTab: NewsTab { .library }

    var readerListSource: ReaderListSource? {
        switch self {
        case .subscribed: .subscribed
        case .following: .following
        }
    }

    static let defaultFeeds: [Self] = [.subscribed, .following]
}
