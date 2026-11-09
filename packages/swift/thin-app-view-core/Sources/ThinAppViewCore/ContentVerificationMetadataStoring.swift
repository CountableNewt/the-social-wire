public protocol ContentVerificationMetadataStoring: Sendable {
  func contentVerificationMetadata(uri: String) async throws -> ContentVerificationMetadata?
  func upsertContentItem(_ item: IndexedContentItem, expectedCID: String?) async throws -> Bool
  /// False means no matching row remained; never deletes a concurrently updated CID.
  func deleteContentItem(uri: String, expectedCID: String) async throws -> Bool
}
