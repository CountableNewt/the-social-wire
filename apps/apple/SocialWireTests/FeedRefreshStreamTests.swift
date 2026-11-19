import Foundation
import Testing
@testable import SocialWire

@Suite("Feed refresh stream")
@MainActor
struct FeedRefreshStreamTests {
    private func entry(_ id: String, _ publishedAt: String) -> EntryListItem {
        EntryListItem(entryId: id, title: id, summary: nil, publishedAt: publishedAt, thumbnailUrl: nil, thumbnailFallbackUrl: nil)
    }

    @Test func decodesEventsInOrderAndStopsAtDone() async throws {
        let lines = [
            #"{"kind":"started","started":{"total":2}}"#,
            #"{"kind":"future","future":{}}"#,
            #"{"kind":"publication","publication":{"publicationId":"a","status":"updated","indexed":3,"completed":1,"total":2}}"#,
            #"{"kind":"done","done":{"total":2,"indexed":3,"unavailable":0}}"#,
            #"{"kind":"started","started":{"total":9}}"#,
        ]
        var events: [FeedRefreshStreamEvent] = []
        try await FeedRefreshStreamEvent.consume(AsyncStream { continuation in
            for line in lines { continuation.yield(line) }
            continuation.finish()
        }) { events.append($0) }
        #expect(events == [
            .started(total: 2),
            .publication(.init(publicationId: "a", status: .updated, indexed: 3, completed: 1, total: 2)),
            .done(total: 2, indexed: 3, unavailable: 0),
        ])
    }

    @Test func aStreamEndingBeforeDoneFails() async {
        await #expect(throws: SocialWireError.self) {
            try await FeedRefreshStreamEvent.consume(AsyncStream { continuation in
                continuation.yield(#"{"kind":"started","started":{"total":1}}"#)
                continuation.finish()
            }) { _ in }
        }
    }

    @Test func progressLabelsUseTitleCaseCounts() {
        #expect(FeedRefreshProgress().label == "Checking Publications…")
        #expect(FeedRefreshProgress(completed: 2, total: 5).label == "Checking 2 of 5 Publications…")
        #expect(FeedRefreshProgress(completed: 0, total: 1).label == "Checking 0 of 1 Publication…")
        #expect(FeedRefreshProgress(completed: 5, total: 5).label == "Updating Articles…")
    }

    @Test func unreadRefreshKeepsRowsOlderThanTheFreshSnapshot() {
        let existing = [
            entry("read-elsewhere", "2026-01-05T00:00:00Z"),
            entry("kept", "2026-01-04T00:00:00Z"),
            entry("older", "2026-01-02T00:00:00.000Z"),
        ]
        let merged = ReaderUnreadPageMerge.merge(
            existing: existing, existingCursor: "after-older",
            fresh: [entry("new", "2026-01-06T00:00:00Z"), entry("kept", "2026-01-04T00:00:00Z")], freshCursor: "fresh"
        )
        #expect(merged.entries.map(\.entryId) == ["new", "kept", "older"])
        #expect(merged.cursor == "after-older")
    }

    @Test func aCompleteUnreadPageReplacesTheList() {
        let merged = ReaderUnreadPageMerge.merge(
            existing: [entry("old", "2026-01-01T00:00:00Z")], existingCursor: "c",
            fresh: [entry("new", "2026-01-06T00:00:00Z")], freshCursor: nil
        )
        #expect(merged.entries.map(\.entryId) == ["new"])
        #expect(merged.cursor == nil)
    }
}
