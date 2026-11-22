import Foundation

enum NewsTabSlot: Hashable {
    case subscribed
    case following
    case standardList(String)
    case readLater
    case archive

    init(_ feed: NewsPrimaryFeed) {
        switch feed {
        case .subscribed: self = .subscribed
        case .following: self = .following
        }
    }

    var primaryFeed: NewsPrimaryFeed? {
        switch self {
        case .subscribed: .subscribed
        case .following: .following
        case .standardList, .readLater, .archive: nil
        }
    }

    var savedListSource: ReaderListSource? {
        switch self {
        case .readLater: .readLater
        case .archive: .archive
        case .subscribed, .following, .standardList: nil
        }
    }
}
