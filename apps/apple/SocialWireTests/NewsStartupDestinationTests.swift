import Foundation
import Testing
@testable import SocialWire

@Suite("Startup destination")
struct NewsStartupDestinationTests {
    @Test("preferences and last destinations stay account scoped")
    func accountIsolation() throws {
        let suite = "startup-tests-\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }
        #expect(NewsStartupDestination.preference(viewerDID: "one", defaults: defaults) == .lastUsed)
        defaults.set("readLater", forKey: NewsStartupDestination.key(viewerDID: "one"))
        NewsStartupDestination.following.remember(viewerDID: "one", defaults: defaults)
        #expect(NewsStartupDestination.preference(viewerDID: "one", defaults: defaults) == .readLater)
        #expect(NewsStartupDestination.remembered(viewerDID: "one", defaults: defaults) == .following)
        #expect(NewsStartupDestination.preference(viewerDID: "two", defaults: defaults) == .lastUsed)
        #expect(NewsStartupDestination.remembered(viewerDID: "two", defaults: defaults) == nil)
        defaults.set("retired-feed", forKey: NewsStartupDestination.key(viewerDID: "one"))
        #expect(NewsStartupDestination.preference(viewerDID: "one", defaults: defaults) == .lastUsed)
    }

    @Test("only last-used reader feeds restore a publication or folder")
    func restoresReaderSelection() {
        #expect(NewsStartupDestination.lastUsed.restoresReaderSelection(remembered: nil))
        #expect(NewsStartupDestination.lastUsed.restoresReaderSelection(remembered: .following))
        #expect(!NewsStartupDestination.subscribed.restoresReaderSelection(remembered: .following))
        for destination in [NewsStartupDestination.readLater, .archive, .podcasts, .lists] {
            #expect(!NewsStartupDestination.lastUsed.restoresReaderSelection(remembered: destination))
        }
    }

    @Test("explicit startup wins over last used and hidden feeds fall back")
    func resolution() {
        #expect(NewsStartupDestination.subscribed.resolved(availableFeeds: [.subscribed, .following], remembered: .readLater) == .subscribed)
        #expect(NewsStartupDestination.following.resolved(availableFeeds: [.subscribed], remembered: nil) == .subscribed)
        #expect(NewsStartupDestination.subscribed.resolved(availableFeeds: [], remembered: nil) == .readLater)
        #expect(NewsStartupDestination.lastUsed.resolved(availableFeeds: [.subscribed], remembered: .readLater) == .readLater)
        #expect(NewsStartupDestination.lastUsed.resolved(availableFeeds: [], remembered: .lists) == .lists)
        #expect(NewsStartupDestination.lists.resolved(availableFeeds: [.subscribed], remembered: .following) == .lists)
        #expect(NewsStartupDestination.lastUsed.resolved(availableFeeds: [.subscribed], remembered: .archive) == .archive)
        #expect(NewsStartupDestination.lastUsed.resolved(availableFeeds: [.podcasts], remembered: .podcasts) == .podcasts)
        #expect(NewsStartupDestination.podcasts.resolved(availableFeeds: [.subscribed], remembered: nil) == .subscribed)
    }
}
