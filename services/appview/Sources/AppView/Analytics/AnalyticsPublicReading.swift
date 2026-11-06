protocol AnalyticsPublicReading: Sendable {
  func records(author: String, collection: String, cursor: String?) async throws -> (records: [AnalyticsPublicRecord], cursor: String?)
  func record(uri: String) async throws -> AnalyticsPublicRecord?
  func engagement(uris: [String]) async throws -> AnalyticsPosts
}
