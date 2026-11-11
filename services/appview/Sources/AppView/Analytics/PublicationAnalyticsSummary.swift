import Foundation

struct PublicationAnalyticsSummary: Codable, Sendable {
  let sampledAt: Date
  let publications: [PublicationAnalyticsRow]
  let inventoryComplete: Bool
}
