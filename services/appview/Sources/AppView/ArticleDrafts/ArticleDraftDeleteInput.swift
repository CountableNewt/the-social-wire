struct ArticleDraftDeleteInput: Codable, Sendable {
  let draftId: String
  let expectedRevision: Int64
  let mutationId: String
}
