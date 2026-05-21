@testable import GatewayCore
import HTTPTypes
import Hummingbird
import NIOCore
import Testing

@Suite("DPoPHtu")
struct DPoPHtuTests {
  @Test("canonical prefers X-Forwarded-Proto over internal http scheme")
  func forwardedProtoWinsOverInternalScheme() {
    var headerFields = HTTPFields()
    headerFields[HTTPField.Name("X-Forwarded-Proto")!] = "https"
    let request = Request(
      head: .init(
        method: .get,
        scheme: "http",
        authority: "api.testing.thesocialwire.app",
        path: "/v1/publications/sidebar",
        headerFields: headerFields
      ),
      body: .init(buffer: ByteBuffer())
    )

    let canonical = DPoPHtu.canonical(for: request)
    #expect(canonical == "https://api.testing.thesocialwire.app/v1/publications/sidebar")
  }
}
