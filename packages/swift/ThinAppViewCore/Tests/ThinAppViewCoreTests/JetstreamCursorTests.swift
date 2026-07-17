import Foundation
import Testing
@testable import ThinAppViewCore

@Suite("Jetstream cursor")
struct JetstreamCursorTests {
  @Test func parsesOnlyMicrosecondCursorRepresentations() {
    #expect(JetstreamCursor.parse("1720000000000000") == 1_720_000_000_000_000)
    #expect(JetstreamCursor.parse(NSNumber(value: 42)) == 42)
    #expect(JetstreamCursor.parse(nil) == nil)
  }

  @Test func rewindsCommittedCursorFiveSeconds() {
    #expect(
      JetstreamCursor.resumeCursor(
        committed: 20_000_000,
        seededReceived: 19_000_000,
        rewindMicroseconds: 5_000_000
      ) == 15_000_000
    )
  }

  @Test func firstUpgradeRewindsSeedThirtySeconds() {
    #expect(
      JetstreamCursor.resumeCursor(
        committed: nil,
        seededReceived: 40_000_000,
        rewindMicroseconds: 5_000_000
      ) == 10_000_000
    )
  }

  @Test func replacesCursorWithoutDroppingCollectionFilters() throws {
    let url = try JetstreamCursor.url(
      "wss://jetstream.example/subscribe?wantedCollections=site.standard.document&cursor=1",
      cursor: 25
    )
    #expect(url.contains("wantedCollections=site.standard.document"))
    #expect(url.contains("cursor=25"))
    #expect(!url.contains("cursor=1&"))
  }
}

private actor OrderedValues {
  var values: [String] = []
  func append(_ value: String) { values.append(value) }
  func snapshot() -> [String] { values }
}

@Suite("Bounded sequential message pump")
struct BoundedSequentialMessagePumpTests {
  @Test func processesMessagesInReceiveOrder() async throws {
    let ordered = OrderedValues()
    let pump = BoundedSequentialMessagePump(capacity: 4, handleMessage: { value in
      if value == "first" { try await Task.sleep(for: .milliseconds(25)) }
      await ordered.append(value)
    }, onFailure: { _ in })
    #expect(pump.enqueue("first"))
    #expect(pump.enqueue("second"))
    try await Task.sleep(for: .milliseconds(100))
    #expect(await ordered.snapshot() == ["first", "second"])
  }

  @Test func rejectsMessagesWhenCapacityIsSaturated() async throws {
    let pump = BoundedSequentialMessagePump(capacity: 1, handleMessage: { _ in
      try await Task.sleep(for: .milliseconds(100))
    }, onFailure: { _ in })
    #expect(pump.enqueue("first"))
    #expect(!pump.enqueue("overflow"))
  }
}
