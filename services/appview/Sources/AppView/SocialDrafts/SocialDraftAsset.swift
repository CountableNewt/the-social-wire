struct SocialDraftAsset: Codable, Sendable, Equatable {
  let assetId: String
  let name: String
  let contentType: String
  let sha256: String
  let kind: String
  var state: String
  let byteLength: Int64
}
