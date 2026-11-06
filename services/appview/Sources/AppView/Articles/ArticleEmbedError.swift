enum ArticleEmbedError: Error, Equatable {
  case invalidURL
  case notFound
  case unavailable
}
