import Foundation

public enum AppViewUnreadCounterSupport {
  public static func generation(for date: Date = Date()) -> Int64 {
    Int64((date.timeIntervalSince1970 * 1000).rounded())
  }

  public static func scopeKeys(
    publicationAtUri: String?,
    publicationScopeAtUris: [String],
    publicationSiteUrls: [String]
  ) -> [String] {
    AppViewProjectionCacheScopeKeys.publicationSiteKeys(
      publicationAtUri: publicationAtUri,
      publicationScopeAtUris: publicationScopeAtUris,
      publicationSiteUrls: publicationSiteUrls
    )
  }

  public static func publicationScope(
    viewerDid: String,
    publicationId: String,
    authorDid: String,
    publicationAtUri: String?,
    publicationScopeAtUris: [String],
    publicationSiteUrls: [String],
    sectionKeys: [String],
    updatedAt: Date = Date()
  ) -> AppViewPublicationScope {
    AppViewPublicationScope(
      viewerDid: viewerDid,
      publicationId: publicationId,
      authorDid: authorDid,
      publicationAtUri: publicationAtUri,
      publicationScopeAtUris: publicationScopeAtUris,
      publicationSiteUrls: publicationSiteUrls,
      scopeKeys: scopeKeys(
        publicationAtUri: publicationAtUri,
        publicationScopeAtUris: publicationScopeAtUris,
        publicationSiteUrls: publicationSiteUrls
      ),
      sectionKeys: Array(Set(sectionKeys)).sorted(),
      updatedAt: updatedAt
    )
  }

  public static func contentMatchesScope(
    authorDid: String,
    publicationSite: String?,
    scope: AppViewPublicationScope
  ) -> Bool {
    guard scope.authorDid == authorDid else { return false }
    guard !scope.scopeKeys.isEmpty else { return true }
    return ThinAppViewQuerySupport.publicationSiteMatches(
      siteField: publicationSite,
      publicationAtUri: scope.publicationAtUri,
      publicationScopeAtUris: scope.publicationScopeAtUris,
      publicationSiteUrls: scope.publicationSiteUrls
    )
  }
}
