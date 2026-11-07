import Foundation

enum NewsStartupDestination: String, CaseIterable, Identifiable {
    case lastUsed, subscribed, following, readLater, archive, podcasts, lists

    var id: String { rawValue }

    var title: LocalizedStringResource {
        switch self {
        case .lastUsed: "Last Used"
        case .subscribed: "Subscribed"
        case .following: "Following"
        case .readLater: "Read Later"
        case .archive: "Archive"
        case .podcasts: "Podcasts"
        case .lists: "Lists"
        }
    }

    static func key(viewerDID: String) -> String {
        "the-social-wire.startup-destination.v1.\(viewerDID)"
    }

    static func preference(viewerDID: String, defaults: UserDefaults = .standard) -> Self {
        defaults.string(forKey: key(viewerDID: viewerDID)).flatMap(Self.init(rawValue:)) ?? .lastUsed
    }

    static func remembered(viewerDID: String, defaults: UserDefaults = .standard) -> Self? {
        defaults.string(forKey: "the-social-wire.last-destination.v1.\(viewerDID)")
            .flatMap(Self.init(rawValue:))
    }

    func remember(viewerDID: String, defaults: UserDefaults = .standard) {
        guard self != .lastUsed else { return }
        defaults.set(rawValue, forKey: "the-social-wire.last-destination.v1.\(viewerDID)")
    }

    func restoresReaderSelection(remembered: Self?) -> Bool {
        self == .lastUsed && (remembered == nil || remembered == .subscribed || remembered == .following)
    }

    func resolved(availableFeeds: [NewsPrimaryFeed], remembered: Self?) -> Self {
        let candidate = self == .lastUsed ? remembered ?? .subscribed : self
        switch candidate {
        case .readLater, .archive, .lists: return candidate
        case .podcasts where availableFeeds.contains(.podcasts): return .podcasts
        case .subscribed where availableFeeds.contains(.subscribed): return .subscribed
        case .following where availableFeeds.contains(.following): return .following
        default:
            if availableFeeds.contains(.subscribed) { return .subscribed }
            if availableFeeds.contains(.following) { return .following }
            return .readLater
        }
    }
}
