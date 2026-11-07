struct ArticleDraftPutInput: Codable, Sendable {
  let expectedRevision: Int64
  let mutationId: String
  let draft: ArticleDraftPayload
  let attachments: [ArticleDraftAttachmentReference]
}
