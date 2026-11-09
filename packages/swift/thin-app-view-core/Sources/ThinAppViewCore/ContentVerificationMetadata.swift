public struct ContentVerificationMetadata: Sendable, Equatable {
  public let cid: String
  public let publicationSite: String?

  public init(cid: String, publicationSite: String?) {
    self.cid = cid
    self.publicationSite = publicationSite
  }
}
