import Foundation

/// Public publication references, never viewer-owned state or arbitrary collections.
public struct PublicContentDiscoveryScope: Hashable, Sendable {
  public let authorDid: String
  public let publicationKeys: [String]

  public init(authorDid: String, publicationKeys: [String]) throws {
    guard authorDid.utf8.count <= 2048, ThinAppViewEnrollBackfill.isBackfillEligibleAuthorDid(authorDid),
      publicationKeys.count <= 8 else { throw PublicContentDiscoveryError.invalidScope }
    var keys: Set<String> = []
    for raw in publicationKeys {
      guard raw.utf8.count <= 2048 else { throw PublicContentDiscoveryError.invalidScope }
      if let parsed = RenderFieldExtractor.parseAtUri(raw) {
        guard parsed.did == authorDid, parsed.collection == "site.standard.publication",
          !raw.contains(where: { "?#%\\ \r\n\t".contains($0) }) else { throw PublicContentDiscoveryError.invalidScope }
        keys.insert(raw)
      } else {
        guard let url = URLComponents(string: raw), url.scheme == "https", url.host?.isEmpty == false,
          url.user == nil, url.password == nil, url.fragment == nil, url.query == nil,
          let key = RenderFieldExtractor.normalizePublicationSiteUrl(raw) else { throw PublicContentDiscoveryError.invalidScope }
        keys.insert(key)
      }
    }
    self.authorDid = authorDid
    self.publicationKeys = keys.sorted()
  }

  func contains(site: String?) -> Bool {
    guard let site else { return false }
    if publicationKeys.contains(site) { return true }
    return RenderFieldExtractor.normalizePublicationSiteUrl(site).map(publicationKeys.contains) ?? false
  }
}
