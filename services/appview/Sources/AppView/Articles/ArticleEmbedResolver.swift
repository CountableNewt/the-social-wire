import Foundation
import SwiftSoup
import ThinAppViewCore

/// Successes alone are cached. A page's declared PDS record must map back to its exact URL.
actor ArticleEmbedResolver {
  let reader: any ArticleEmbedReading
  private var cached: [String: (value: ArticleEmbed, until: Date)] = [:]
  private var active = 0
  init(reader: any ArticleEmbedReading) { self.reader = reader }

  func resolve(_ raw: String) async throws -> ArticleEmbed {
    let key = try Self.url(raw)
    if let hit = cached[key], hit.until > Date() { return hit.value }
    // Bounded admission waits within the same twelve-second caller deadline.
    while active >= 16 { try await Task.sleep(for: .milliseconds(10)) }
    try Task.checkCancellation()
    active += 1
    defer { active -= 1 }
    let page = try await reader.page(url: key)
    guard page.status != 404 else { throw ArticleEmbedError.notFound }
    guard page.status == 200, page.body.count <= 1 << 20 else { throw ArticleEmbedError.unavailable }
    guard let uri = try Self.headURI(String(decoding: page.body, as: UTF8.self)),
      let document = Self.subject(uri), ["site.standard.document", "site.standard.entry"].contains(document.collection),
      let record = try await reader.record(uri: uri), record.uri == uri,
      record.value.values["$type"] as? String == document.collection,
      let publicationURI = record.value.values["site"] as? String,
      let publication = Self.subject(publicationURI), publication.collection == "site.standard.publication",
      let pub = try await reader.record(uri: publicationURI), pub.uri == publicationURI,
      pub.value.values["$type"] as? String == publication.collection,
      let rawBase = pub.value.values["url"] as? String,
      let base = try? Self.url(rawBase), let path = record.value.values["path"] as? String,
      path.hasPrefix("/"), !path.hasPrefix("//"),
      let canonical = try? Self.url(base.trimmingTrailingSlashes + path), canonical == key,
      URLComponents(string: canonical)?.host == URLComponents(string: base)?.host,
      let title = record.value.values["title"] as? String, !title.isEmpty,
      let name = pub.value.values["name"] as? String, !name.isEmpty
    else { throw ArticleEmbedError.notFound }
    let description = record.value.values["description"] as? String
    let value = ArticleEmbed(documentUri: uri, publicationUri: publicationURI, publicationName: name,
      publicationUrl: base, articleUrl: key, title: title,
      description: description?.isEmpty == false ? description : nil, authorDid: document.did,
      contentLabels: ContentLabels.fromRecord(record.value.values))
    try Task.checkCancellation()
    if cached.count >= 256 { cached.removeAll(keepingCapacity: true) }
    cached[key] = (value, Date().addingTimeInterval(300))
    return value
  }

  static func url(_ raw: String) throws -> String {
    guard raw.utf8.count <= 4096, var url = URLComponents(string: raw), url.scheme == "https",
      let host = url.host, !host.isEmpty, url.user == nil, url.password == nil else { throw ArticleEmbedError.invalidURL }
    url.fragment = nil
    guard let value = url.string else { throw ArticleEmbedError.invalidURL }
    return value
  }

  static func subject(_ raw: String) -> (did: String, collection: String, rkey: String)? {
    guard raw.hasPrefix("at://"), !raw.contains(where: { "?#\\ \t\r\n".contains($0) }) else { return nil }
    let parts = raw.dropFirst(5).split(separator: "/", omittingEmptySubsequences: false).map(String.init)
    guard parts.count == 3, parts[0].hasPrefix("did:"), parts[0] != "did:", !parts[2].isEmpty,
      ["site.standard.document", "site.standard.entry", "site.standard.publication"].contains(parts[1]) else { return nil }
    return (parts[0], parts[1], parts[2])
  }

  static func headURI(_ body: String) throws -> String? {
    // XML mode preserves explicit head/body boundaries instead of synthesizing a head.
    // Script/style content is never considered metadata, even when it contains markup.
    let document = try Parser.xmlParser().settings(.htmlDefault).parseInput(body, "")
    var pending: [(Node, Bool)] = [(document, false)]
    var foundHead = false
    while let (node, inHead) = pending.popLast() {
      let tag = node.nodeName().lowercased()
      if tag == "body" { return nil }
      if ["script", "style", "title", "textarea"].contains(tag) { continue }
      if tag == "head" {
        if foundHead { return nil }
        foundHead = true
      }
      let head = inHead || tag == "head"
      if head, let element = node as? Element {
        var candidate = ""
        if tag == "meta", [try element.attr("name").lowercased(), try element.attr("property").lowercased()].contains("at:canonical") {
          candidate = try element.attr("content")
        } else if tag == "link" {
          let rels = try element.attr("rel").split(whereSeparator: { $0.isWhitespace }).map(String.init)
          if rels.contains("site.standard.document") || rels.contains("site.standard.entry") { candidate = try element.attr("href") }
        }
        if let subject = subject(candidate), ["site.standard.document", "site.standard.entry"].contains(subject.collection) { return candidate }
      }
      for child in node.getChildNodes().reversed() { pending.append((child, head)) }
    }
    return nil
  }
}

private extension String {
  var trimmingTrailingSlashes: String {
    var result = self
    while result.hasSuffix("/") { result.removeLast() }
    return result
  }
}
