import Foundation

enum ArticleDraftValidation {
  static let maximumPayloadBytes = 512 << 10
  static func identifier(_ value: String) -> Bool {
    value.utf8.count == 36 && SocialDraftValidation.assetID(value)
  }
  static func date(_ value: String) -> Bool {
    let formatter = ISO8601DateFormatter()
    formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    if formatter.date(from: value) != nil { return true }
    formatter.formatOptions = [.withInternetDateTime]
    return formatter.date(from: value) != nil
  }
  static func revision(_ value: Int64) -> Bool { value >= 0 && value < Int64.max }
  static func payload(_ value: ArticleDraftPayload) -> Bool {
    guard ArticleDraftValidation.identifier(value.id), date(value.createdAt), date(value.updatedAt),
      value.tags.count <= 100, value.assets.count <= 200,
      Set(value.assets.map(\.id)).count == value.assets.count,
      value.tags.allSatisfy({ $0.utf8.count <= 256 }),
      [value.title, value.markdown, value.excerpt, value.path, value.publicationUri].allSatisfy({ !$0.contains("\0") }),
      value.assets.allSatisfy({ ArticleDraftValidation.identifier($0.id) && $0.width > 0 && $0.height > 0
        && $0.width <= 100_000 && $0.height <= 100_000 && !$0.alt.contains("\0")
        && !$0.name.isEmpty && $0.name.utf8.count <= 1024 && !$0.name.contains("\0")
        && ["image/jpeg", "image/png", "image/gif", "image/webp"].contains($0.mimeType) }),
      value.publicationClaimId.map(ArticleDraftValidation.identifier) ?? true,
      value.publishingRecordKey == nil || value.publicationClaimId != nil,
      value.coverAssetId.map({ id in value.assets.contains { $0.id == id } }) ?? true,
      [value.publishedUri, value.scheduleRecordKey, value.scheduleUri, value.publishingRecordKey].allSatisfy({ $0.map { !$0.isEmpty && !$0.contains("\0") && $0.utf8.count <= 4096 } ?? true })
    else { return false }
    return true
  }
  static func references(_ refs: [ArticleDraftAttachmentReference], draft: ArticleDraftPayload) -> Bool {
    refs.count == draft.assets.count && Set(refs.map(\.localAssetId)).count == refs.count
      && Set(refs.map(\.localAssetId)) == Set(draft.assets.map(\.id))
      && refs.allSatisfy { ArticleDraftValidation.identifier($0.localAssetId) && ArticleDraftValidation.identifier($0.assetId) }
  }
}
