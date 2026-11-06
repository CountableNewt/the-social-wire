struct ArticleDraftPayload: Codable, Sendable, Equatable {
  let id: String
  let title: String
  let markdown: String
  let excerpt: String
  let path: String
  let tags: [String]
  let publicationUri: String
  let createdAt: String
  let updatedAt: String
  let assets: [ArticleDraftAssetDescriptor]
  let coverAssetId: String?
  let publishedUri: String?
  let scheduleRecordKey: String?
  let scheduleUri: String?
  let publishingRecordKey: String?
  let publicationClaimId: String?

  var isLocked: Bool { publishedUri != nil || scheduleRecordKey != nil || publishingRecordKey != nil }

  func hasSameContent(as other: Self) -> Bool {
    id == other.id && title == other.title && markdown == other.markdown && excerpt == other.excerpt
      && path == other.path && tags == other.tags && publicationUri == other.publicationUri
      && createdAt == other.createdAt && assets == other.assets && coverAssetId == other.coverAssetId
  }
}
