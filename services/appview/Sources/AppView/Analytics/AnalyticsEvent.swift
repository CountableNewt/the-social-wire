import Foundation

struct AnalyticsEvent: Codable, Sendable, Equatable {
  var subjectUri: String
  var event: String
  var surface: String
}
