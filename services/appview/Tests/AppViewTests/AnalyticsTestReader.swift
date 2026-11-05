@testable import AppView

struct AnalyticsTestReader: AnalyticsPublicReading {
  var collections: [String: [AnalyticsPublicRecord]] = [:]
  var recordsByURI: [String: AnalyticsPublicRecord] = [:]
  var posts = AnalyticsPosts(posts: [])
  var repeatedCursor = false
  func records(author: String, collection: String, cursor: String?) async throws -> (records: [AnalyticsPublicRecord], cursor: String?) {
    (cursor == nil ? collections[collection] ?? [] : [], repeatedCursor ? "repeat" : nil)
  }
  func record(uri: String) async throws -> AnalyticsPublicRecord? { recordsByURI[uri] }
  func engagement(uris: [String]) async throws -> AnalyticsPosts { posts }
}
