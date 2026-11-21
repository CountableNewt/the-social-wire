import Foundation

enum NewsRootSection: String, CaseIterable, Codable, Identifiable, Hashable {
    var id: String { rawValue }

    var title: LocalizedStringResource {
        switch self {
        case .readLater: "Read Later"
        case .feeds: "News"
        case .lists: "Lists"
        }
    }

    case readLater
    case feeds
    case lists

    var systemImage: String {
        switch self {
        case .readLater: "bookmark"
        case .feeds: "newspaper"
        case .lists: "list.bullet"
        }
    }

    var fallbackSlot: NewsTabSlot {
        switch self {
        case .readLater: .readLater
        case .feeds: .subscribed
        case .lists: .standardList("")
        }
    }

    var emptyTitle: LocalizedStringResource {
        switch self {
        case .readLater: "Nothing Saved"
        case .feeds: "No Feeds"
        case .lists: "No Lists"
        }
    }

    var emptyDescription: LocalizedStringResource {
        switch self {
        case .readLater: "Saved articles will appear here."
        case .feeds: "Available feeds will appear here."
        case .lists: "Your reader lists will appear here."
        }
    }
}
