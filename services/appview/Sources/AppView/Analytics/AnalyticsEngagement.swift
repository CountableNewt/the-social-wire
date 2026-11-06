import Foundation

struct AnalyticsEngagement: Codable, Sendable, Equatable {
  var likes: Int = 0
  var replies: Int = 0
  var reposts: Int = 0
  var quotes: Int = 0
}
