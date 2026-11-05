struct SocialDraftReserveInput: Codable, Sendable {
  let assetId: String
  let name: String
  let contentType: String
  let byteLength: Int64
  let sha256: String
  let kind: String
}
