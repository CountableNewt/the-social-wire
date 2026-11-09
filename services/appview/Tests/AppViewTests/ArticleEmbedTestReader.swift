import Foundation
import GatewayCore
@testable import AppView

actor ArticleEmbedTestReader: ArticleEmbedReading {
  static let article = "at://did:plc:author/site.standard.document/post"
  static let publication = "at://did:plc:author/site.standard.publication/pub"
  var calls = 0
  var failOnce = false
  var stalls = false
  var status = 200
  var body: Data
  var records: [String: AnalyticsPublicRecord]

  init(base: String = "https://publisher.social", path: String = "/post", failOnce: Bool = false) {
    self.failOnce = failOnce
    body = Data("<head><meta name=\"at:canonical\" content=\"\(Self.article)\"></head>".utf8)
    records = [
      Self.article: .init(uri: Self.article, value: .init(values: ["$type": "site.standard.document", "site": Self.publication, "path": path, "title": "Article"])),
      Self.publication: .init(uri: Self.publication, value: .init(values: ["$type": "site.standard.publication", "url": base, "name": "Publication"])),
    ]
  }
  func page(url: String) async throws -> (status: Int, body: Data) {
    calls += 1
    if stalls { try await Task.sleep(for: .seconds(60)) }
    if failOnce { failOnce = false; throw ArticleEmbedError.unavailable }
    return (status, body)
  }
  func record(uri: String) async throws -> AnalyticsPublicRecord? { records[uri] }
  func setLabels(_ values: [String]) {
    guard let existing = records[Self.article] else { return }
    var record = existing.value.values
    record["labels"] = ["$type": "com.atproto.label.defs#selfLabels", "values": values.map { ["val": $0] }]
    records[Self.article] = .init(uri: existing.uri, value: .init(values: record))
  }
  func stallPage() { stalls = true }
  func mismatchIdentity(_ uri: String) {
    guard let existing = records[uri] else { return }
    records[uri] = .init(uri: "at://did:plc:attacker/site.standard.document/wrong", value: existing.value)
  }
}
