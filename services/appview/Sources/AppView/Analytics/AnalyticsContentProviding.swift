import Foundation

protocol AnalyticsContentProviding: Sendable {
  func validate(_ uri: String) async throws
  func inventory(author: String, at: Date) async throws -> (items: [AnalyticsItem], complete: Bool)
}
