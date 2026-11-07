struct ArticleDraftDocument: Codable, Sendable, Equatable {
  let draftId: String
  let revision: Int64
  let deleted: Bool
  let mutationId: String
  let updatedAt: String
  let draft: ArticleDraftPayload?
  let attachments: [ArticleDraftAssetBinding]
}
