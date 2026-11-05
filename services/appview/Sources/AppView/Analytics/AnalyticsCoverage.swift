import Foundation

struct AnalyticsCoverage: Codable, Sendable, Equatable {
  var trackingStart: String? = nil
  var sampledAt: Date? = nil
  var engagementKnownItems: Int = 0
  var totalItems: Int = 0
  var excludesSelf: Bool = true
  var trackingRetentionDays: Int = 90
  var snapshotStart: String? = nil
  var inventoryComplete: Bool = false
  var trackingScope: String = "social-wire"
  var platformImpressions: Int? = nil
  var snapshotSemantics: String = "cumulative-last-observation-per-day"

  enum CodingKeys: String, CodingKey { case trackingStart, sampledAt, engagementKnownItems, totalItems, excludesSelf, trackingRetentionDays, snapshotStart, inventoryComplete, trackingScope, platformImpressions, snapshotSemantics }
  func encode(to encoder: Encoder) throws {
    var container = encoder.container(keyedBy: CodingKeys.self)
    try container.encode(trackingStart, forKey: .trackingStart)
    try container.encode(sampledAt, forKey: .sampledAt)
    try container.encode(engagementKnownItems, forKey: .engagementKnownItems)
    try container.encode(totalItems, forKey: .totalItems)
    try container.encode(excludesSelf, forKey: .excludesSelf)
    try container.encode(trackingRetentionDays, forKey: .trackingRetentionDays)
    try container.encode(snapshotStart, forKey: .snapshotStart)
    try container.encode(inventoryComplete, forKey: .inventoryComplete)
    try container.encode(trackingScope, forKey: .trackingScope)
    try container.encode(platformImpressions, forKey: .platformImpressions)
    try container.encode(snapshotSemantics, forKey: .snapshotSemantics)
  }
}
