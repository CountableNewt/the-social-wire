import Foundation
import GatewayCore
import Testing

@Suite("Bootstrap stream NDJSON")
struct BootstrapStreamNDJSONTests {
  @Test("encodeLine appends newline and round-trips selectedPublication")
  func encodeLineAppendsNewline() throws {
    let event = AppViewBootstrapStreamEvent.selectedPublication(publicationId: "pub-a")
    let data = try AppViewBootstrapStreamNDJSON.encodeLine(event)
    #expect(data.last == 0x0A)

    let line = String(decoding: data.dropLast(), as: UTF8.self)
    let decoded = try JSONDecoder().decode(AppViewBootstrapStreamEvent.self, from: Data(line.utf8))
    #expect(decoded.kind == .selectedPublication)
    #expect(decoded.selectedPublication?.publicationId == "pub-a")
  }
}
