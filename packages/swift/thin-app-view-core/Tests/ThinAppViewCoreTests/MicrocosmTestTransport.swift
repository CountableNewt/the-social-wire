import AsyncHTTPClient
import Foundation
import NIOCore
import Testing
@testable import ThinAppViewCore

actor MicrocosmTestTransport: PDSHTTPTransport {
  enum Mode: Sendable { case good, many, wrongDID, wrongURI, wrongSite, tamperedCID, repeatedCursor, unavailable, stalled }
  let mode: Mode
  let author = "did:plc:aaaaaaaaaaaaaaaaaaaaaaaa"
  let publication = "at://did:plc:aaaaaaaaaaaaaaaaaaaaaaaa/site.standard.publication/main"
  private(set) var backlinkCalls = 0
  private(set) var hydrationCalls = 0
  init(_ mode: Mode = .good) { self.mode = mode }

  func execute(_ request: HTTPClientRequest, timeout: TimeAmount) async throws -> HTTPClientResponse {
    #expect(request.headers.first(name: "Authorization") == nil)
    #expect(request.headers.first(name: "DPoP") == nil)
    #expect(request.headers.first(name: "User-Agent")?.contains("TheSocialWire") == true)
    let url = try #require(URLComponents(string: request.url))
    let query = Dictionary((url.queryItems ?? []).map { ($0.name, $0.value ?? "") }, uniquingKeysWith: { _, new in new })
    if url.host == "constellation.microcosm.blue" {
      backlinkCalls += 1
      #expect(url.path == "/xrpc/blue.microcosm.links.getBacklinks")
      #expect(query["did"] == author && query["limit"] == "25" && query["reverse"] == "false")
      if mode == .unavailable { return .init(status: .serviceUnavailable) }
      if mode == .stalled { try await Task.sleep(for: .seconds(60)) }
      if query["source"] == "site.standard.entry:site" { return try response(["records": [], "cursor": NSNull()]) }
      let many = mode == .many
      let offset = query["cursor"] == nil ? 0 : 25
      let records = (0..<(many ? 25 : 1)).map { index in
        // Constellation's default/false order is newest first; true reverses it.
        // Model 999 chronological records so the 50-record cap exposes old-first loss.
        let chronologicalIndex = many && query["reverse"] != "true" ? 998 - offset - index : offset + index
        return ["did": mode == .wrongDID ? "did:plc:cccccccccccccccccccccccc" : author,
          "collection": "site.standard.document", "rkey": String(chronologicalIndex)]
      }
      let cursor: Any = many ? "next\(offset)" : mode == .repeatedCursor ? "repeated" : NSNull()
      return try response(["records": records, "cursor": cursor, "total": many ? 999 : 1])
    }
    #expect(url.host == "slingshot.microcosm.blue" && url.path == "/xrpc/com.atproto.repo.getRecord")
    hydrationCalls += 1
    let rkey = query["rkey"] ?? ""
    var value: [String: String] = ["$type": "site.standard.document", "title": "Article \(rkey)",
      "site": mode == .wrongSite ? "at://did:plc:aaaaaaaaaaaaaaaaaaaaaaaa/site.standard.publication/other" : publication,
      "publishedAt": "2026-11-05T12:00:00Z", "url": "https://publisher.example/\(rkey)"]
    let cid = RepositoryCID.hash(RepositoryDAGCBOR.encode(.map(value.mapValues { .string($0) }))).string
    if mode == .tamperedCID { value["title"] = "Changed after hashing" }
    return try response(["uri": mode == .wrongURI ? "at://did:plc:cccccccccccccccccccccccc/site.standard.document/\(rkey)" : "at://\(author)/site.standard.document/\(rkey)", "cid": cid, "value": value])
  }

  private func response(_ value: [String: Any]) throws -> HTTPClientResponse {
    .init(status: .ok, body: .bytes(ByteBuffer(bytes: try JSONSerialization.data(withJSONObject: value))))
  }
}
