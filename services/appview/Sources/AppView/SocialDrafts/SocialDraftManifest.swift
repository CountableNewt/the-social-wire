struct SocialDraftManifest: Codable, Sendable, Equatable {
  let draftId: String
  let revision: Int64
  let deleted: Bool
  var attachments: [SocialDraftAssetBinding]
}
