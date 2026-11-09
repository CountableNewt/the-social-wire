import Crypto
import Foundation

/// RSS / Skyreader identity helpers aligned with web `rssFeedCore.ts`.
public enum RssFeedLexicons {
  public static let rssAuthorDid = "did:web:skyreader.rss"
  public static let skyreaderFeedSubscription = "app.skyreader.feed.subscription"
  public static let skyreaderFeedEntry = "app.skyreader.feed.entry"
  public static let publicationPrefix = "rss:"
  public static let entryPrefix = "rssentry:"
}

public enum RssFeedIdentity {
  public static func normalizeFeedUrl(_ raw: String) -> String? {
    let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !trimmed.isEmpty else { return nil }
    var href = trimmed
    if !href.lowercased().hasPrefix("http://"), !href.lowercased().hasPrefix("https://") {
      href = "https://\(href)"
    }
    guard var components = URLComponents(string: href),
          let scheme = components.scheme?.lowercased(),
          scheme == "http" || scheme == "https",
          let host = components.host?.trimmingCharacters(in: .whitespacesAndNewlines),
          !host.isEmpty
    else { return nil }
    if scheme == "http" { components.scheme = "https" }
    components.fragment = nil
    components.user = nil
    components.password = nil
    guard let url = components.url else { return nil }
    var out = url.absoluteString
    if out.hasSuffix("/") { out.removeLast() }
    return out
  }

  public static func isFetchableFeedUrl(_ raw: String) -> Bool {
    guard let normalized = normalizeFeedUrl(raw) else { return false }
    guard let host = URL(string: normalized)?.host?.lowercased() else { return false }
    return !isBlockedFetchHostname(host)
  }

  public static func rssPublicationId(from normalizedFeedUrl: String) -> String {
    "\(RssFeedLexicons.publicationPrefix)\(utf8ToBase64Url(normalizedFeedUrl))"
  }

  public static func normalizedFeedUrl(fromRssPublicationId pubId: String) -> String? {
    guard pubId.hasPrefix(RssFeedLexicons.publicationPrefix) else { return nil }
    let payload = String(pubId.dropFirst(RssFeedLexicons.publicationPrefix.count))
    return base64UrlToUtf8(payload)
  }

  /// Prefer normalized article links over opaque GUIDs so re-polls do not mint duplicate rows.
  public static func stableItemKey(from item: ParsedRssItem) -> String {
    if let postKey = postIdentityStableKey(from: item.link)
      ?? postIdentityStableKey(from: item.guid)
    {
      return postKey
    }
    if let linkKey = stableLinkKey(from: item.link) {
      return linkKey
    }
    if let guidKey = stableGuidKey(from: item.guid) {
      return guidKey
    }
    let title = item.title.trimmingCharacters(in: .whitespacesAndNewlines)
    let date = item.publishedAtISO.trimmingCharacters(in: .whitespacesAndNewlines)
    return "fallback:\(title)\n\(date)"
  }

  /// Identity keys used to collapse legacy Atom `?p=` rows with path-based link rows.
  public static func dedupeIdentityKeys(
    forEntryId entryId: String,
    renderJSON: String?,
    summary: String?
  ) -> Set<String> {
    var keys = Set<String>()
    if let canonical = canonicalLink(forEntryId: entryId, renderJSON: renderJSON, summary: summary) {
      keys.insert("url:\(canonical)")
    }
    if entryId.hasPrefix(RssFeedLexicons.entryPrefix),
       let decoded = decodeEntryId(entryId)
    {
      if decoded.stableItemKey.hasPrefix("post:") {
        keys.insert(decoded.stableItemKey)
      }
      if let raw = rawURL(fromStableItemKey: decoded.stableItemKey),
         let postKey = postIdentityStableKey(from: raw)
      {
        keys.insert(postKey)
      }
    }
    if let renderJSON,
       let data = renderJSON.data(using: .utf8),
       let render = try? JSONDecoder().decode(ContentRenderFields.self, from: data),
       let articleUrl = render.articleUrl?.trimmingCharacters(in: .whitespacesAndNewlines),
       !articleUrl.isEmpty
    {
      if let canonical = canonicalArticleUrl(articleUrl) {
        keys.insert("url:\(canonical)")
      }
      if let postKey = postIdentityStableKey(from: articleUrl) {
        keys.insert(postKey)
      }
    }
    if let summary = summary?.trimmingCharacters(in: .whitespacesAndNewlines),
       summary.lowercased().hasPrefix("http")
    {
      if let postKey = postIdentityStableKey(from: summary) {
        keys.insert(postKey)
      }
    }
    return keys
  }

  public static func registersAsDuplicateIdentity(
    keys: Set<String>,
    seen: inout Set<String>
  ) -> Bool {
    guard !keys.isEmpty else { return false }
    if keys.contains(where: { seen.contains($0) }) { return true }
    seen.formUnion(keys)
    return false
  }

  public static func decodeEntryId(_ entryId: String) -> (feedUrl: String, stableItemKey: String)? {
    guard entryId.hasPrefix(RssFeedLexicons.entryPrefix) else { return nil }
    let payload = String(entryId.dropFirst(RssFeedLexicons.entryPrefix.count))
    guard let inner = base64UrlToUtf8(payload) else { return nil }
    if let data = inner.data(using: .utf8),
       let object = try? JSONSerialization.jsonObject(with: data) as? [String: String],
       let feedUrl = object["f"],
       let stableItemKey = object["k"]
    {
      return (feedUrl, stableItemKey)
    }
    guard let pipe = inner.firstIndex(of: "|") else { return nil }
    let feedUrl = String(inner[..<pipe])
    let stableItemKey = String(inner[inner.index(after: pipe)...])
    guard !feedUrl.isEmpty, !stableItemKey.isEmpty else { return nil }
    return (feedUrl, stableItemKey)
  }

  /// Normalizes article URLs for dedupe (HTTPS, no query/fragment, no trailing slash).
  public static func canonicalArticleUrl(_ raw: String) -> String? {
    RenderFieldExtractor.normalizePublicationSiteUrl(raw) ?? normalizeFeedUrl(raw)
  }

  public static func canonicalLinkFromStableItemKey(_ stableItemKey: String) -> String? {
    if stableItemKey.hasPrefix("post:") {
      return nil
    }
    if stableItemKey.hasPrefix("link:") {
      let raw = String(stableItemKey.dropFirst("link:".count))
      return canonicalArticleUrl(raw)
    }
    if stableItemKey.hasPrefix("guid:") {
      let raw = String(stableItemKey.dropFirst("guid:".count))
      guard raw.lowercased().hasPrefix("http") else { return nil }
      return canonicalArticleUrl(raw)
    }
    return nil
  }

  public static func canonicalLink(
    forEntryId entryId: String,
    renderJSON: String?,
    summary: String?
  ) -> String? {
    if let renderJSON,
       let data = renderJSON.data(using: .utf8),
       let render = try? JSONDecoder().decode(ContentRenderFields.self, from: data),
       let articleUrl = render.articleUrl?.trimmingCharacters(in: .whitespacesAndNewlines),
       !articleUrl.isEmpty,
       let canonical = canonicalArticleUrl(articleUrl)
    {
      return canonical
    }

    if entryId.hasPrefix(RssFeedLexicons.entryPrefix),
       let decoded = decodeEntryId(entryId),
       let link = canonicalLinkFromStableItemKey(decoded.stableItemKey)
    {
      return link
    }

    if let summary = summary?.trimmingCharacters(in: .whitespacesAndNewlines),
       summary.lowercased().hasPrefix("http"),
       let normalized = canonicalArticleUrl(summary)
    {
      return normalized
    }
    return nil
  }

  public static func canonicalLinkForEntryListItem(_ item: AppViewEntryListItem) -> String? {
    canonicalLink(forEntryId: item.entryId, renderJSON: nil, summary: item.summary)
  }

  /// Browser-openable permalink for an indexed entry (RSS article link, render `articleUrl`, etc.).
  public static func originalArticleURL(
    forEntryId entryId: String,
    render: ContentRenderFields?,
    summary: String?
  ) -> String? {
    if let render,
       let articleUrl = render.articleUrl?.trimmingCharacters(in: .whitespacesAndNewlines),
       !articleUrl.isEmpty,
       let canonical = canonicalArticleUrl(articleUrl)
    {
      return canonical
    }
    let renderJSON: String? = render.flatMap { fields in
      guard let data = try? JSONEncoder().encode(fields) else { return nil }
      return String(data: data, encoding: .utf8)
    }
    return canonicalLink(forEntryId: entryId, renderJSON: renderJSON, summary: summary)
  }

  /// Prefers link-stable RSS entry ids over legacy guid-stable rows.
  public static func isPreferredRssEntryURI(_ candidate: String, over incumbent: String) -> Bool {
    let candidateScore = stableKeyPreferenceScore(for: candidate)
    let incumbentScore = stableKeyPreferenceScore(for: incumbent)
    if candidateScore != incumbentScore { return candidateScore > incumbentScore }
    return false
  }

  public static func dedupeEntryListItems(_ items: [AppViewEntryListItem]) -> [AppViewEntryListItem] {
    var seenEntryIds = Set<String>()
    var seenIdentityKeys = Set<String>()
    var seenTitlePublished = Set<String>()
    var deduped: [AppViewEntryListItem] = []
    deduped.reserveCapacity(items.count)

    for item in items {
      guard seenEntryIds.insert(item.entryId).inserted else { continue }

      let identityKeys = dedupeIdentityKeys(forEntryId: item.entryId, renderJSON: nil, summary: item.summary)
      if registersAsDuplicateIdentity(keys: identityKeys, seen: &seenIdentityKeys) {
        if let existingIdx = deduped.firstIndex(where: {
          !dedupeIdentityKeys(forEntryId: $0.entryId, renderJSON: nil, summary: $0.summary)
            .isDisjoint(with: identityKeys)
        }),
           (deduped[existingIdx].thumbnailUrl ?? "").isEmpty,
           !(item.thumbnailUrl ?? "").isEmpty
        {
          deduped[existingIdx] = item
        }
        continue
      }

      if identityKeys.isEmpty {
        let titleKey =
          "\(item.title.trimmingCharacters(in: .whitespacesAndNewlines).lowercased())|\(Int(item.publishedAt.timeIntervalSince1970))"
        guard seenTitlePublished.insert(titleKey).inserted else { continue }
      }

      deduped.append(item)
    }
    return deduped
  }

  private static func stableKeyPreferenceScore(for entryId: String) -> Int {
    guard let decoded = decodeEntryId(entryId) else { return 0 }
    if decoded.stableItemKey.hasPrefix("link:") || decoded.stableItemKey.hasPrefix("post:") { return 2 }
    if decoded.stableItemKey.hasPrefix("guid:") { return 1 }
    return 0
  }

  private static func rawURL(fromStableItemKey stableItemKey: String) -> String? {
    if stableItemKey.hasPrefix("link:") {
      return String(stableItemKey.dropFirst("link:".count))
    }
    if stableItemKey.hasPrefix("guid:") {
      return String(stableItemKey.dropFirst("guid:".count))
    }
    return nil
  }

  /// WordPress / Atom ids like `https://www.theverge.com/?p=936829` ↔ path `/936829/`.
  private static func postIdentityStableKey(from raw: String?) -> String? {
    guard let raw = raw?.trimmingCharacters(in: .whitespacesAndNewlines), !raw.isEmpty else { return nil }
    guard let components = URLComponents(string: raw),
          let host = components.host?.trimmingCharacters(in: .whitespacesAndNewlines).lowercased(),
          !host.isEmpty
    else { return nil }

    if let postId = components.queryItems?.first(where: { $0.name == "p" })?.value,
       !postId.isEmpty,
       postId.allSatisfy(\.isNumber)
    {
      return "post:\(host):\(postId)"
    }

    for segment in components.path.split(separator: "/") {
      let part = String(segment)
      guard part.count >= 5, part.allSatisfy(\.isNumber) else { continue }
      return "post:\(host):\(part)"
    }
    return nil
  }

  private static func stableLinkKey(from raw: String?) -> String? {
    guard let trimmed = raw?.trimmingCharacters(in: .whitespacesAndNewlines), !trimmed.isEmpty else {
      return nil
    }
    if let normalized = normalizeFeedUrl(trimmed) {
      return "link:\(normalized)"
    }
    return "link:\(trimmed)"
  }

  private static func stableGuidKey(from raw: String?) -> String? {
    guard let trimmed = raw?.trimmingCharacters(in: .whitespacesAndNewlines), !trimmed.isEmpty else {
      return nil
    }
    if let normalized = normalizeFeedUrl(trimmed) {
      return "guid:\(normalized)"
    }
    return "guid:\(trimmed)"
  }

  public static func rssEntryId(normalizedFeedUrl: String, stableItemKey: String) -> String {
    let inner: [String: String] = ["f": normalizedFeedUrl, "k": stableItemKey]
    guard
      let data = try? JSONSerialization.data(withJSONObject: inner),
      let json = String(data: data, encoding: .utf8)
    else {
      return "\(RssFeedLexicons.entryPrefix)\(utf8ToBase64Url(normalizedFeedUrl + "|" + stableItemKey))"
    }
    return "\(RssFeedLexicons.entryPrefix)\(utf8ToBase64Url(json))"
  }

  public static func deterministicCid(for entryUri: String) -> String {
    let digest = SHA256.hash(data: Data(entryUri.utf8))
    let hex = digest.prefix(16).map { String(format: "%02x", $0) }.joined()
    return "rss:\(hex)"
  }

  private static func utf8ToBase64Url(_ text: String) -> String {
    let data = Data(text.utf8)
    return data.base64EncodedString()
      .replacingOccurrences(of: "+", with: "-")
      .replacingOccurrences(of: "/", with: "_")
      .replacingOccurrences(of: "=", with: "")
  }

  private static func base64UrlToUtf8(_ b64url: String) -> String? {
    var s = b64url.replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
    let pad = s.count % 4
    if pad != 0 { s += String(repeating: "=", count: 4 - pad) }
    guard let data = Data(base64Encoded: s) else { return nil }
    return String(data: data, encoding: .utf8)
  }

  private static func isBlockedFetchHostname(_ hostname: String) -> Bool {
    let h = hostname.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
    if h.isEmpty { return true }
    if h == "localhost" || h.hasSuffix(".localhost") || h.hasSuffix(".local") { return true }
    if h == "[::1]" || h == "::1" { return true }

    let parts = h.split(separator: ".", omittingEmptySubsequences: false).map(String.init)
    if parts.count == 4, let a = Int(parts[0]), let b = Int(parts[1]) {
      if a == 127 || a == 0 || a == 10 { return true }
      if a == 192 && b == 168 { return true }
      if a == 172 && (16 ... 31).contains(b) { return true }
      if a == 169 && b == 254 { return true }
    }
    return false
  }
}
