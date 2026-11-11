struct PublicationAnalyticsRow: Codable, Sendable {
  let uri: String
  let name: String
  let url: String
  let documents: Int
  let subscribers: Int?
  let recommendations: Int?
  let recommendationCoverageComplete: Bool

  enum CodingKeys: String, CodingKey {
    case uri, name, url, documents, subscribers, recommendations, recommendationCoverageComplete
  }

  func encode(to encoder: Encoder) throws {
    var container = encoder.container(keyedBy: CodingKeys.self)
    try container.encode(uri, forKey: .uri)
    try container.encode(name, forKey: .name)
    try container.encode(url, forKey: .url)
    try container.encode(documents, forKey: .documents)
    try container.encode(subscribers, forKey: .subscribers)
    try container.encode(recommendations, forKey: .recommendations)
    try container.encode(recommendationCoverageComplete, forKey: .recommendationCoverageComplete)
  }
}
