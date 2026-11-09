import Foundation
import GatewayCore
import Testing
import ThinAppViewCore

@testable import AppView

@Suite("Bootstrap content label metadata")
struct BootstrapContentLabelsTests {
  @Test("Live and cached first-page encoding retains labels without changing legacy fields", arguments: [false, true])
  func preservesLiveAndCachedLabels(cached: Bool) async throws {
    let publishedAt = Date(timeIntervalSince1970: 1_800_000_000)
    let entry = AppViewEntryListItem(entryId: "at://did:plc:author/site.standard.document/one", title: "Labelled",
      publishedAt: publishedAt, contentLabels: ["ai-generated", "sexual"])
    let page = AppViewEntryListResponse(entries: [entry, .init(entryId: "old", title: "Unlabelled", publishedAt: publishedAt)], cursor: "next")
    let enrollment = BootstrapEnrollment.start {}
    while !(await enrollment.isFinished) { await Task.yield() }
    let selection = try await BootstrapEntriesPageSelection.load(enrollment: enrollment,
      livePage: { cached ? nil : page },
      cachedPage: { cached ? .init(value: page, cachedAt: publishedAt, expiresAt: publishedAt.addingTimeInterval(300)) : nil })
    let event = BootstrapEntriesPageEvent(publicationId: "publication", page: selection.page,
      source: selection.source, cachedAt: selection.cachedAt, expiresAt: selection.expiresAt)
    let encoded = try event.encodedLine()
    #expect(encoded.last == 0x0A)
    let object = try #require(JSONSerialization.jsonObject(with: encoded) as? [String: Any])
    let payload = try #require(object["entriesPage"] as? [String: Any])
    let entries = try #require(payload["entries"] as? [[String: Any]])
    #expect(entries.map { $0["title"] as? String } == ["Labelled", "Unlabelled"])
    #expect(entries[0]["contentLabels"] as? [String] == ["ai-generated", "sexual"])
    #expect(entries[1]["contentLabels"] == nil)
    let decoder = JSONDecoder()
    decoder.dateDecodingStrategy = .iso8601
    let legacy = try decoder.decode(AppViewBootstrapStreamEvent.self, from: encoded)
    #expect(legacy.entriesPage?.entries.first?.entryId == entry.entryId)
    #expect(legacy.entriesPage?.source == (cached ? .projectionCache : .liveProjection))
    #expect(legacy.entriesPage?.cachedAt == (cached ? publishedAt : nil))
    #expect(legacy.entriesPage?.cursor == "next")
  }
}
