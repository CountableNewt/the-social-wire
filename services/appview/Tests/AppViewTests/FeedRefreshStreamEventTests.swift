import Foundation
import Testing
@testable import AppView

@Suite struct FeedRefreshStreamEventTests {
  private func decode(_ event: FeedRefreshStreamEvent) throws -> [String: Any] {
    let line = try event.encodedLine()
    #expect(line.last == 0x0A)
    return try #require(JSONSerialization.jsonObject(with: line.dropLast()) as? [String: Any])
  }

  @Test func eventsAreOneKindKeyedJSONObjectPerLine() throws {
    let started = try decode(.started(.init(total: 3)))
    #expect(started["kind"] as? String == "started")
    #expect((started["started"] as? [String: Any])?["total"] as? Int == 3)

    let publication = try decode(.publication(.init(publicationId: "at://did:plc:a/site.standard.publication/p",
      status: .updated, indexed: 2, completed: 1, total: 3)))
    let payload = try #require(publication["publication"] as? [String: Any])
    #expect(publication["kind"] as? String == "publication")
    #expect(payload["status"] as? String == "updated")
    #expect(payload["indexed"] as? Int == 2)
    #expect(payload["completed"] as? Int == 1)

    let done = try decode(.done(.init(total: 3, indexed: 2, unavailable: 1)))
    #expect(done["kind"] as? String == "done")
    #expect((done["done"] as? [String: Any])?["unavailable"] as? Int == 1)
  }
}
