import Foundation
import GatewayCore

protocol ArticleEmbedReading: Sendable {
  func page(url: String) async throws -> (status: Int, body: Data)
  func record(uri: String) async throws -> AnalyticsPublicRecord?
}
