import Foundation

protocol AnalyticsStoring: Sendable {
  func save(author: String, items: [AnalyticsItem], complete: Bool, at: Date) async throws
  func record(viewer: String, author: String, event: AnalyticsEvent, at: Date) async throws
  func summary(author: String, days: Int, limit: Int, cursor: String, at: Date) async throws -> AnalyticsSummary
  func cleanup(at: Date) async throws
}

extension AnalyticsStore: AnalyticsStoring {}
