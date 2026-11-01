import Foundation
import Testing
@testable import SocialWire

@MainActor
struct SavedSidebarSourceTests {
    @Test
    func sembleSourcesGroupPublishersAndFilterWithoutAnArchive() throws {
        let model = SocialWireAppModel()
        model.preferencesFromGateway = try JSONDecoder().decode(PreferencesRecord.self, from: Data(#"""
        {"$type":"app.thesocialwire.preferences","readLaterService":"semble","createdAt":"2026-10-27T00:00:00Z","updatedAt":"2026-10-27T00:00:00Z"}
        """#.utf8))
        model.sembleItems = try JSONDecoder().decode([SembleCollectionItem].self, from: Data(#"""
        [
          {"id":"one","cardUri":"at://did:plc:test/network.cosmik.card/one","cardType":"URL","url":"https://www.example.com/one","siteName":"Example","contributor":{"did":"did:plc:test"}},
          {"id":"two","cardUri":"at://did:plc:test/network.cosmik.card/two","cardType":"URL","url":"https://example.com/two","siteName":"Example","contributor":{"did":"did:plc:test"}},
          {"id":"three","cardUri":"at://did:plc:test/network.cosmik.card/three","cardType":"URL","url":"https://other.example/three","siteName":"Other","contributor":{"did":"did:plc:test"}}
        ]
        """#.utf8))
        // Sidebar sources stay tied to Read Later, even while a News feed is active.
        model.readerListSource = .subscribed
        let sources = model.savedFeedSources(for: .readLater)
        #expect(sources.count == 2)
        let example = try #require(sources.first { $0.id == "site:example.com" })
        #expect(example.count == 2)
        #expect(model.savedFeedSources(for: .archive).isEmpty)

        model.selectReaderListSource(.readLater)
        model.selectSavedFeedSource(example)
        #expect(model.filteredSembleItems.map(\.id) == ["one", "two"])
        model.clearSavedFeedSource()
        #expect(model.filteredSembleItems.count == 3)
    }
}
