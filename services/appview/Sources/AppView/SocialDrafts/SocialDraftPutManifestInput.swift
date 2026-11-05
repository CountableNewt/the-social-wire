struct SocialDraftPutManifestInput: Codable, Sendable {
  let draftId: String
  let expectedRevision: Int64
  let attachments: [SocialDraftManifestReference]
}
