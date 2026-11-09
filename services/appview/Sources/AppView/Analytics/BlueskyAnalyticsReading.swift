import Foundation

protocol BlueskyAnalyticsReading: Sendable {
  func profile(author: String) async throws -> BlueskyAnalyticsProfile
  func feed(author: String, cursor: String?) async throws -> BlueskyAnalyticsFeed
  func shares(url: String, since: Date, until: Date) async throws -> [BlueskyAnalyticsFeed.Post]
}
