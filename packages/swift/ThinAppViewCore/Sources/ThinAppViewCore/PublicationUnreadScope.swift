import Foundation

public struct PublicationUnreadScope: Sendable, Hashable {
  public let publicationId: String
  public let authorDid: String
  public let publicationAtUri: String?
  public let publicationScopeAtUris: [String]
  public let publicationSiteUrls: [String]

  public init(
    publicationId: String,
    authorDid: String,
    publicationAtUri: String?,
    publicationScopeAtUris: [String],
    publicationSiteUrls: [String]
  ) {
    self.publicationId = publicationId
    self.authorDid = authorDid
    self.publicationAtUri = publicationAtUri
    self.publicationScopeAtUris = publicationScopeAtUris
    self.publicationSiteUrls = publicationSiteUrls
  }
}
