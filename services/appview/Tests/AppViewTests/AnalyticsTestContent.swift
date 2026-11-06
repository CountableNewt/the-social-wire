import Foundation
@testable import AppView

struct AnalyticsTestContent: AnalyticsContentProviding {
  var items: [AnalyticsItem] = []
  var valid = true
  func validate(_ uri: String) async throws { if !valid { throw AnalyticsError.providerUnavailable } }
  func inventory(author: String, at: Date) async throws -> (items: [AnalyticsItem], complete: Bool) { (items, true) }
}
