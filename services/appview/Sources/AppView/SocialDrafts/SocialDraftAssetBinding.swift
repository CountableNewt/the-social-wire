struct SocialDraftAssetBinding: Codable, Sendable, Equatable {
  let assetId: String
  let role: String
  let ref: String
  let asset: SocialDraftAsset
}
