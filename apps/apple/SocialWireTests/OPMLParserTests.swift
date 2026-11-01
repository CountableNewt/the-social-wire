import Foundation
import Testing
@testable import SocialWire

@Suite("OPML import")
struct OPMLParserTests {
    @Test("parses nested outlines, normalizes URLs, and deduplicates feeds")
    func parsesNestedOutlines() throws {
        let document = """
        <?xml version="1.0" encoding="UTF-8"?>
        <opml version="2.0"><body>
          <outline text="Technology">
            <outline text="Example" xmlUrl="http://EXAMPLE.com/feed.xml#latest" htmlUrl="http://example.com" />
            <outline title="Duplicate" xmlUrl="https://example.com/feed.xml" />
          </outline>
        </body></opml>
        """

        let feeds = try OPMLParser.parse(Data(document.utf8))

        #expect(feeds.count == 1)
        #expect(feeds[0].title == "Example")
        #expect(feeds[0].feedURL == "https://example.com/feed.xml")
        #expect(feeds[0].siteURL == "https://example.com")
    }

    @Test("rejects files over two megabytes")
    func rejectsLargeFiles() {
        let data = Data(repeating: 0, count: OPMLParser.maximumBytes + 1)
        #expect(throws: OPMLParserError.fileTooLarge) {
            try OPMLParser.parse(data)
        }
    }

    @Test("rejects documents without feed outlines")
    func rejectsEmptyDocuments() {
        let document = "<opml version=\"2.0\"><body><outline text=\"Folder\" /></body></opml>"
        #expect(throws: OPMLParserError.noFeeds) {
            try OPMLParser.parse(Data(document.utf8))
        }
    }
    @Test("Completed OPML feeds are removed from selection while failures remain retryable")
    @MainActor
    func importSelectionAfterPartialSuccess() throws {
        let model = OPMLImportModel()
        model.load(data: Data(#"<opml><body><outline text="Good" xmlUrl="https://example.com/good"/><outline text="Bad" xmlUrl="https://example.com/bad"/></body></opml>"#.utf8), existingFeedURLs: [])
        let failed = try #require(model.feeds.first { $0.title == "Bad" })
        model.beginImport(total: 2)
        model.noteProgress(2)
        model.finishImport(failures: [OPMLImportFailure(feed: failed, message: "Retry")], successfulFeedURLs: ["https://example.com/good"])
        #expect(model.selectedFeeds.map(\.title) == ["Bad"])
        #expect(model.totalCount == 2)
        #expect(model.failures.count == 1)
    }

}
