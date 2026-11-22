import Foundation
import Testing
@testable import SocialWire

@Suite("Article social record contracts")
struct ArticleSocialContractTests {
    @Test("Recommendations accept only normalized site.standard.document AT-URIs")
    func standardSiteDocumentURI() {
        #expect(StandardSiteRecommendationContract.documentURI(
            from: "at://did:plc:author/site.standard.document/article"
        ) == "at://did:plc:author/site.standard.document/article")
        #expect(StandardSiteRecommendationContract.documentURI(
            from: "at%3A%2F%2Fdid%3Aplc%3Aauthor%2Fsite.standard.document%2Farticle"
        ) == "at://did:plc:author/site.standard.document/article")
        #expect(StandardSiteRecommendationContract.documentURI(
            from: "at://did:plc:author/site.standard.entry/article"
        ) == nil)
        #expect(StandardSiteRecommendationContract.documentURI(
            from: "at://did:plc:author/app.bsky.feed.post/article"
        ) == nil)
        #expect(StandardSiteRecommendationContract.documentURI(
            from: "rss:https://example.com"
        ) == nil)
    }

    @Test("Recommendation record and PDS collection constants match web")
    func recommendationRecordEncoding() throws {
        let record = StandardSiteRecommendRecord(
            type: PDSRecordService.standardSiteRecommend,
            document: "at://did:plc:author/site.standard.document/article",
            createdAt: "2026-08-30T12:00:00Z"
        )
        let object = try #require(
            JSONSerialization.jsonObject(with: JSONEncoder().encode(record)) as? [String: String]
        )

        #expect(PDSRecordService.standardSiteRecommend == "site.standard.graph.recommend")
        #expect(object["$type"] == "site.standard.graph.recommend")
        #expect(object["document"] == "at://did:plc:author/site.standard.document/article")
        #expect(object["createdAt"] == "2026-08-30T12:00:00Z")
    }

    @Test("Cached entry details created before social actions remain decodable")
    func legacyEntryDetailDecoding() throws {
        let detail = try JSONDecoder().decode(
            EntryDetail.self,
            from: Data("""
            {
              "entryId": "story-1",
              "title": "Story",
              "publishedAt": "",
              "contentHtml": "<p>Body</p>"
            }
            """.utf8)
        )

        #expect(detail.entryId == "story-1")
        #expect(detail.standardSiteDocumentURI == nil)
    }
}
