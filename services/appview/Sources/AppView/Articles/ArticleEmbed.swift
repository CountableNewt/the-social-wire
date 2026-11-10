import Foundation

struct ArticleEmbed: Codable, Sendable, Equatable {
  let documentUri: String
  let publicationUri: String
  let publicationName: String
  let publicationUrl: String
  let articleUrl: String
  let title: String
  var description: String?
  let authorDid: String
  var contentLabels: [String]? = nil
}
