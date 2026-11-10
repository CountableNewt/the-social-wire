import Foundation
import Logging
import Testing

@testable import ThinAppViewCore

@Suite("Content label metadata")
struct ContentLabelsTests {
  @Test("Only exact supported self labels are retained in stable order")
  func extraction() {
    let labels: [String: Any] = ["$type": "com.atproto.label.defs#selfLabels", "values": [
      ["val": "sexual"], ["val": "unknown"], ["val": "ai-generated"], ["val": "sexual"],
    ]]
    #expect(RenderFieldExtractor.extractRenderFields(from: ["$type": "site.standard.document", "labels": labels]).contentLabels == ["ai-generated", "sexual"])
    for type in ["site.standard.publication", "blog.pckt.document", "site.standard.entry", "com.standard.document"] {
      #expect(ContentLabels.fromRecord(["$type": type, "labels": labels]) == nil)
    }
    #expect(ContentLabels.fromRecord(["labels": labels]) == nil)
    for record: [String: Any] in [
      [:], ["labels": ["values": [["val": "sexual"]]]],
      ["labels": ["$type": "other", "values": [["val": "sexual"]]]],
      ["labels": ["$type": "com.atproto.label.defs#selfLabels", "values": "sexual"]],
      ["labels": ["$type": "com.atproto.label.defs#selfLabels", "values": [["val": "AI-generated"], ["val": "nsfw"], ["val": "!no-unauth"], ["val": " sexual "]]]],
      ["tags": ["ai-generated", "sexual"], "nsfw": true, "aiGenerated": true],
    ] {
      #expect(RenderFieldExtractor.extractRenderFields(from: record.merging(["$type": "site.standard.document"]) { original, _ in original }).contentLabels == nil)
    }
  }

  @Test("Old render JSON and entry copies preserve backwards compatibility")
  func codingAndCopies() throws {
    let json = Data(#"{"title":"Old","publishedAt":"2026-01-01T00:00:00Z"}"#.utf8)
    #expect(try JSONDecoder().decode(ContentRenderFields.self, from: json).contentLabels == nil)
    let render = ContentRenderFields(title: "New", publishedAt: "2026-01-01T00:00:00Z", contentLabels: ["sexual"])
    #expect(try JSONDecoder().decode(ContentRenderFields.self, from: JSONEncoder().encode(render)).contentLabels == ["sexual"])
    let entry = AppViewEntryListItem(entryId: "at://did:plc:a/site.standard.document/one", title: "New", publishedAt: Date(), contentLabels: ["sexual"])
    #expect(entry.withPublicationId("publication").withReadState(true).contentLabels == ["sexual"])
    #expect(entry.withReadState(true).isRead)
    #expect(ContentLabels.decodeJSON(#"["sexual","ai-generated","sexual","unknown"]"#) == ["ai-generated", "sexual"])
    #expect(ContentLabels.decodeJSON(#"{"val":"sexual"}"#) == nil)
  }

  @Test("Indexing persists labels without changing feed order, membership or read state")
  func persistedFeeds() async throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent("content-labels-\(UUID()).sqlite").path
    defer {
      for suffix in ["", "-wal", "-shm"] { try? FileManager.default.removeItem(atPath: path + suffix) }
    }
    let logger = Logger(label: "content-labels-test")
    let store = try SQLiteThinAppViewStore(path: path, logger: logger)
    let indexer = ThinAppViewIndexer(store: store, config: .fromEnvironment(["ENABLE_THIN_APPVIEW": "true"]), logger: logger)
    let author = "did:plc:author", viewer = "did:plc:viewer", site = "https://pckt.blog"
    let now = Date()
    let scope = AppViewUnreadCounterSupport.publicationScope(viewerDid: viewer, publicationId: "publication", authorDid: author,
      publicationAtUri: nil, publicationScopeAtUris: [], publicationSiteUrls: [site], sectionKeys: [], updatedAt: now)
    try await store.replaceViewerFeedProjection(viewerDid: viewer, scopes: [scope],
      feeds: [.init(viewerDid: viewer, kind: .subscribed, feedId: "", updatedAt: now)],
      memberships: [.init(viewerDid: viewer, kind: .subscribed, feedId: "", publicationId: scope.publicationId)])
    for (key, date, values) in [("older", "2026-01-01T00:00:00Z", ["sexual", "ai-generated"]), ("newer", "2026-01-02T00:00:00Z", [])] {
      let record: [String: Any] = ["$type": "site.standard.document", "title": key, "publishedAt": date, "site": site,
        "labels": ["$type": "com.atproto.label.defs#selfLabels", "values": values.map { ["val": $0] }]]
      try await indexer.handleCommit(repoDid: author, collection: "site.standard.document", rkey: key, cid: "fixture-\(key)",
        recordJSON: JSONSerialization.data(withJSONObject: record), operation: "create", pdsBase: "https://pds.example")
    }
    let olderURI = "at://\(author)/site.standard.document/older"
    let render = try await store.fetchContentRender(uri: olderURI)
    #expect(render?.contentLabels == ["ai-generated", "sexual"])
    #expect(try await store.fetchContentItem(uri: olderURI)?.contentLabels == ["ai-generated", "sexual"])
    let ordinary = try await store.listEntries(viewerDid: viewer, authorDid: author, publicationAtUri: nil,
      publicationScopeAtUris: [], publicationSiteUrls: [site], filter: .all, cursor: nil, limit: 10, readBoundary: nil)
    #expect(ordinary.entries.map(\.title) == ["newer", "older"])
    #expect(ordinary.entries.map(\.contentLabels) == [nil, ["ai-generated", "sexual"]])
    let reader: any ThinAppViewStore = store
    let page = try #require(try await reader.listFeedEntries(viewerDid: viewer, selector: .init(kind: .subscribed), filter: .all, cursor: nil, limit: 10))
    #expect(page.response.entries.map(\.title) == ["newer", "older"])
    #expect(page.response.entries.map(\.contentLabels) == [nil, ["ai-generated", "sexual"]])
    #expect(page.response.entries.allSatisfy { !$0.isRead })
    let unread = try #require(try await reader.listFeedEntries(viewerDid: viewer, selector: .init(kind: .subscribed), filter: .unread, cursor: nil, limit: 10))
    #expect(unread.response.entries.map(\.entryId) == page.response.entries.map(\.entryId))
    let read = try #require(try await reader.listFeedEntries(viewerDid: viewer, selector: .init(kind: .subscribed), filter: .read, cursor: nil, limit: 10))
    #expect(read.response.entries.isEmpty)
    try await store.upsertReadMark(viewerDid: viewer, subjectUri: olderURI, createdAt: now)
    let afterRead = try #require(try await reader.listFeedEntries(viewerDid: viewer, selector: .init(kind: .subscribed), filter: .all, cursor: nil, limit: 10))
    #expect(afterRead.response.entries.map(\.title) == ["newer", "older"])
    #expect(afterRead.response.entries.map(\.isRead) == [false, true])
    #expect(afterRead.response.entries.last?.contentLabels == ["ai-generated", "sexual"])
  }
}
