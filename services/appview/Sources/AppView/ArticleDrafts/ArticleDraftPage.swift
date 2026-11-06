struct ArticleDraftPage: Codable, Sendable {
  let drafts: [ArticleDraftDocument]
  let cursor: String?
}
