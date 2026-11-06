import Foundation

struct AnalyticsProvider: AnalyticsContentProviding {
  let reader: any AnalyticsPublicReading

  func validate(_ uri: String) async throws {
    let subject = try AnalyticsSubject(uri)
    guard let record = try await reader.record(uri: uri), record.uri == uri,
      record.value.values["$type"] as? String == subject.collection else { throw AnalyticsError.providerUnavailable }
  }

  func inventory(author: String, at: Date) async throws -> (items: [AnalyticsItem], complete: Bool) {
    var items: [AnalyticsItem] = []
    var complete = true
    var publications: [String: AnalyticsPublicRecord] = [:]
    var knownPublications: Set<String> = []
    for collection in ["app.bsky.feed.post", "site.standard.document"] {
      var records: [AnalyticsPublicRecord] = []
      var cursor: String?
      var seenCursors: Set<String> = []
      var identities: Set<String> = []
      for _ in 0..<10 {
        try Task.checkCancellation()
        let page = try await reader.records(author: author, collection: collection, cursor: cursor)
        guard page.records.count <= 100 else { throw AnalyticsError.providerUnavailable }
        for record in page.records {
          guard identities.insert(record.uri).inserted else { throw AnalyticsError.providerUnavailable }
          records.append(record)
        }
        cursor = page.cursor.flatMap { $0.isEmpty ? nil : $0 }
        guard let next = cursor else { break }
        guard seenCursors.insert(next).inserted else { throw AnalyticsError.providerUnavailable }
      }
      if cursor != nil { complete = false }
      for record in records {
        let subject = try AnalyticsSubject(record.uri)
        let value = record.value.values
        guard subject.did == author, subject.collection == collection,
          value["$type"] as? String == collection else { throw AnalyticsError.providerUnavailable }
        var item = AnalyticsItem(uri: record.uri, kind: "post", title: value["text"] as? String ?? "",
          url: "https://bsky.app/profile/\(author)/post/\(subject.rkey)",
          publishedAt: value["createdAt"] as? String ?? "", refreshedAt: at)
        if collection == "site.standard.document" {
          item.kind = "article"; item.title = value["title"] as? String ?? ""
          item.publishedAt = value["publishedAt"] as? String ?? ""; item.url = ""
          if let site = value["site"] as? String, let publication = try? AnalyticsSubject(site, publication: true), publication.did == author {
            if !knownPublications.contains(site), knownPublications.count < 25 {
              knownPublications.insert(site)
              publications[site] = try? await reader.record(uri: site)
            }
            if let record = publications[site], record.uri == site,
              record.value.values["$type"] as? String == "site.standard.publication",
              let base = record.value.values["url"] as? String, var url = URLComponents(string: base),
              url.scheme == "https", url.host?.isEmpty == false, url.user == nil, url.password == nil,
              let path = value["path"] as? String, path.hasPrefix("/"), !path.hasPrefix("//") {
              var basePath = url.path
              while basePath.hasSuffix("/") { basePath.removeLast() }
              url.path = basePath + path
              url.query = nil; url.fragment = nil
              item.url = url.url?.absoluteString ?? ""
            }
          }
        }
        items.append(item)
      }
    }
    for start in stride(from: 0, to: items.count, by: 25) {
      let uris = items[start..<min(start + 25, items.count)].filter { $0.kind == "post" }.map(\.uri)
      if uris.isEmpty { continue }
      let response = try await reader.engagement(uris: uris)
      for post in response.posts ?? [] {
        guard post.author?.did == author else { throw AnalyticsError.providerUnavailable }
        guard let likes = post.likeCount, let replies = post.replyCount,
          let reposts = post.repostCount, let quotes = post.quoteCount else { continue }
        guard min(likes, replies, reposts, quotes) >= 0 else { throw AnalyticsError.providerUnavailable }
        for index in items.indices where items[index].uri == post.uri {
          items[index].engagement = .init(likes: likes, replies: replies, reposts: reposts, quotes: quotes)
        }
      }
    }
    return (items, complete)
  }
}
