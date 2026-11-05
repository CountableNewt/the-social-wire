struct SocialDraftDeleteManifestInput: Codable, Sendable {
  let draftId: String
  let expectedRevision: Int64
}
